package audio

import (
	"math"
	"testing"
)

func TestGainScalesAndProtectsPeaks(t *testing.T) {
	read := func(src Source) float32 {
		buf := make([]float32, 2)
		src.Read(buf)
		return buf[0]
	}
	quiet := WithGain(&ramp{rate: OutputRate, n: 2, pos: 1}, -6, 0)
	if got := read(quiet); math.Abs(float64(got)-0.5*0.501) > 0.001 {
		t.Fatalf("-6 dB of 0.5 = %v, want 0.2505", got)
	}
	// +6 dB would push a 0.9 peak to 1.8: it is held at full scale instead.
	loud := WithGain(&ramp{rate: OutputRate, n: 2, pos: 1}, 6, 0.9)
	if got := read(loud); math.Abs(float64(got)-0.5/0.9) > 0.001 {
		t.Fatalf("+6 dB with peak 0.9 gave %v, want %v", got, 0.5/0.9)
	}
}

func TestGainWithoutAKnownPeakNeverBoosts(t *testing.T) {
	buf := make([]float32, 2)
	WithGain(&ramp{rate: OutputRate, n: 2, pos: 1}, 6, 0).Read(buf)
	if buf[0] != 0.5 {
		t.Fatalf("+6 dB with no peak gave %v, want 0.5", buf[0])
	}
}
