package resample

import (
	"math"
	"testing"
)

// sine returns n stereo frames of a sine at freq Hz.
func sine(n, rate int, freq float64) []float32 {
	pcm := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		v := float32(0.5 * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
		pcm[2*i], pcm[2*i+1] = v, v
	}
	return pcm
}

func crossings(pcm []float32) int {
	n := 0
	for i := 2; i < len(pcm); i += 2 {
		if (pcm[i] < 0) != (pcm[i-2] < 0) {
			n++
		}
	}
	return n
}

func TestConvertsRateAndKeepsPitch(t *testing.T) {
	r := New(2, 4)
	defer r.Close()
	in := sine(44100, 44100, 1000)
	var out []float32
	buf := make([]float32, 2*1024*6+64)
	for i := 0; i < len(in); i += 2 * 1024 {
		end := min(i+2*1024, len(in))
		n := r.Process(44100, 48000, in[i:end], buf)
		out = append(out, buf[:2*n]...)
	}
	if frames := len(out) / 2; frames < 47800 || frames > 48000 {
		t.Fatalf("one second at 44.1 kHz became %d frames at 48 kHz", frames)
	}
	// 1 kHz crosses zero 2000 times a second, whatever the rate.
	if c := crossings(out); c < 1980 || c > 2020 {
		t.Fatalf("%d zero crossings, want about 2000", c)
	}
}

func TestEqualRatesCopy(t *testing.T) {
	r := New(2, 4)
	defer r.Close()
	in := sine(1000, 48000, 440)
	out := make([]float32, len(in))
	if n := r.Process(48000, 48000, in, out); n != 1000 {
		t.Fatalf("copied %d frames, want 1000", n)
	}
	for i := range in {
		if in[i] != out[i] {
			t.Fatalf("sample %d changed", i)
		}
	}
}

func TestProcessDoesNotAllocate(t *testing.T) {
	r := New(2, 4)
	defer r.Close()
	in := sine(1024, 44100, 1000)
	out := make([]float32, 2*1024*2)
	if n := testing.AllocsPerRun(100, func() { r.Process(44100, 48000, in, out) }); n != 0 {
		t.Fatalf("Process allocates %v times per call", n)
	}
}

func peak(pcm []float32) float64 {
	var m float64
	for _, v := range pcm {
		m = max(m, math.Abs(float64(v)))
	}
	return m
}

func TestRateSwitchKeepsRunning(t *testing.T) {
	r := New(2, 4)
	defer r.Close()
	buf := make([]float32, 2*4096)
	var out []float32
	for _, c := range []struct{ rate, frames int }{{44100, 4410}, {96000, 9600}} {
		in := sine(c.frames, c.rate, 1000)
		for i := 0; i < len(in); i += 2 * 1024 {
			end := min(i+2*1024, len(in))
			n := r.Process(c.rate, 48000, in[i:end], buf)
			out = append(out, buf[:2*n]...)
		}
	}
	if frames := len(out) / 2; frames < 9400 || frames > 9600 {
		t.Fatalf("0.2 s across a rate switch became %d frames, want about 9600", frames)
	}
	for i, v := range out {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("sample %d is %v", i, v)
		}
	}
	if p := peak(out); p > 0.5+0.1 {
		t.Fatalf("peak %v, input peak is 0.5", p)
	}
}

func TestBypassLeavesNoHistory(t *testing.T) {
	r := New(2, 4)
	defer r.Close()
	loud := sine(1024, 48000, 1000)
	out := make([]float32, 2*2048)
	r.Process(44100, 48000, sine(1024, 44100, 1000), out)
	r.Process(48000, 48000, loud, make([]float32, len(loud)))
	n := r.Process(44100, 48000, make([]float32, 2*1024), out)
	if p := peak(out[:2*n]); p >= 1e-6 {
		t.Fatalf("silence after a bypass came out at %v", p)
	}
}

func TestResetLeavesNoHistory(t *testing.T) {
	r := New(2, 4)
	defer r.Close()
	out := make([]float32, 2*2048)
	r.Process(44100, 48000, sine(1024, 44100, 1000), out)
	r.Reset()
	n := r.Process(44100, 48000, make([]float32, 2*1024), out)
	if p := peak(out[:2*n]); p >= 1e-6 {
		t.Fatalf("silence after Reset came out at %v", p)
	}
}

func TestShortOutPanics(t *testing.T) {
	for _, rates := range [][2]int{{44100, 48000}, {48000, 48000}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%d to %d with a short out did not panic", rates[0], rates[1])
				}
			}()
			r := New(2, 4)
			defer r.Close()
			r.Process(rates[0], rates[1], sine(1024, rates[0], 1000), make([]float32, 2*512))
		}()
	}
}
