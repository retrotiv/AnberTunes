package audio

import (
	"io"
	"sync"
	"testing"
	"time"
)

// level is a Source of n frames at a steady level v, at the output rate, so
// what reaches the output is what it gave.
type level struct {
	n, pos int64
	v      float32
}

func (l *level) SampleRate() int { return OutputRate }
func (l *level) Frames() int64   { return l.n }

func (l *level) Read(buf []float32) (int, error) {
	k := min(int64(len(buf)/2), l.n-l.pos)
	for i := range 2 * k {
		buf[i] = l.v
	}
	l.pos += k
	if l.pos >= l.n {
		return int(k), io.EOF
	}
	return int(k), nil
}

func (l *level) SeekFrame(frame int64) error { l.pos = frame; return nil }
func (l *level) Close() error                { return nil }

// tape is an Output that keeps every sample written. Each write takes a
// millisecond, so a second of audio plays in about 50 ms.
type tape struct {
	mu  sync.Mutex
	pcm []int16
}

func (t *tape) Open(int) error { return nil }

func (t *tape) Write(buf []int16) error {
	time.Sleep(time.Millisecond)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pcm = append(t.pcm, buf...)
	return nil
}

func (t *tape) Drop() (int, error) { return 0, nil }
func (t *tape) Close() error       { return nil }

func (t *tape) samples() []int16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]int16(nil), t.pcm...)
}

// pcm16 is level v as the output writes it.
func pcm16(v float32) int16 { return int16(v * 32767) }

// full is a level of 0.5 as the output writes it.
var full = pcm16(0.5)

func TestFadeOutEndsInSilenceAndPauses(t *testing.T) {
	out := &tape{}
	e := NewEngine(out, nil)
	defer e.Close()
	e.Play(&level{n: 10 * OutputRate, v: 0.5})
	eventually(t, "playback", func() bool { return len(out.samples()) > 0 })
	e.FadeOut(100 * time.Millisecond)
	eventually(t, "the fade to pause playback", func() bool { return !e.Status().Playing })

	pcm := out.samples()
	if pcm[len(pcm)-2] != 0 || pcm[len(pcm)-1] != 0 {
		t.Fatalf("last frame written %d %d, want silence", pcm[len(pcm)-2], pcm[len(pcm)-1])
	}
	// From full level down to silence, never rising on the way.
	start := 0
	for start < len(pcm) && pcm[start] == full {
		start += 2
	}
	if start == 0 || start >= len(pcm) {
		t.Fatalf("no full level before the fade (first lower sample at %d of %d)", start, len(pcm))
	}
	for i := start + 2; i < len(pcm); i += 2 {
		if pcm[i] > pcm[i-2] {
			t.Fatalf("level rose during the fade: %d then %d at %d", pcm[i-2], pcm[i], i)
		}
	}
	if frames := (len(pcm) - start) / 2; frames < OutputRate/10 || frames > OutputRate/10+2*chunk {
		t.Errorf("the fade took %d frames, want about %d", frames, OutputRate/10)
	}

	// Resuming plays at full level again.
	n := len(pcm)
	e.Resume()
	eventually(t, "playback after the fade", func() bool { return len(out.samples()) > n+4*chunk })
	if got := out.samples(); got[len(got)-2] != full {
		t.Errorf("level after Resume %d, want %d", got[len(got)-2], full)
	}
}

func TestFadeEndSilencesTheEnd(t *testing.T) {
	out := &tape{}
	ch, onEnd := ends()
	e := NewEngine(out, onEnd)
	defer e.Close()
	src := &level{n: OutputRate / 2, v: 0.5}
	e.Play(src)
	e.FadeEnd(src, 100*time.Millisecond)
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("the source didn't end")
	}
	pcm := out.samples()
	over := int64(OutputRate / 10)
	at := func(frame int64) int16 { return pcm[2*frame] }
	if len(pcm) != 2*int(src.n) {
		t.Fatalf("wrote %d frames, want %d", len(pcm)/2, src.n)
	}
	if got := at(src.n - over - 1); got != full {
		t.Errorf("before the fade: %d, want %d", got, full)
	}
	if got, want := at(src.n-over/2), pcm16(0.5*0.25); got != want {
		t.Errorf("halfway through the fade: %d, want %d", got, want)
	}
	if got := at(src.n - 1); got != 0 {
		t.Errorf("last frame %d, want silence", got)
	}
}

func TestLoadingASourceEndsAFade(t *testing.T) {
	out := &tape{}
	e := NewEngine(out, nil)
	defer e.Close()
	e.Play(&level{n: 10 * OutputRate, v: 0.5})
	e.FadeOut(10 * time.Second)
	e.Play(&level{n: 10 * OutputRate, v: 0.25})
	eventually(t, "the second source", func() bool {
		pcm := out.samples()
		return len(pcm) > 0 && pcm[len(pcm)-2] == pcm16(0.25)
	})
	n := len(out.samples())
	eventually(t, "more playback", func() bool { return len(out.samples()) > n+8*chunk })
	if got := out.samples(); got[len(got)-2] != pcm16(0.25) || !e.Status().Playing {
		t.Errorf("after loading: level %d, playing %v; want full level, playing", got[len(got)-2], e.Status().Playing)
	}
}
