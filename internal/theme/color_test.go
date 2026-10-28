package theme

import (
	"image/color"
	"math"
	"testing"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.5f, want %.5f ± %g", name, got, want, tol)
	}
}

func diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func closeRGB(a, b color.NRGBA, tol int) bool {
	return diff(a.R, b.R) <= tol && diff(a.G, b.G) <= tol && diff(a.B, b.B) <= tol && a.A == b.A
}

func TestSRGBToOKLCH(t *testing.T) {
	cases := []struct {
		hex     string
		l, c, h float64
	}{
		{"#ff0000", 0.62796, 0.25768, 29.2339},
		{"#0000ff", 0.45201, 0.31321, 264.0520},
		{"#e1ad66", 0.78076, 0.10760, 73.7245},
	}
	for _, tc := range cases {
		got := OklabOf(Hex(tc.hex)).OKLCH()
		near(t, tc.hex+" L", got.L, tc.l, 1e-4)
		near(t, tc.hex+" C", got.C, tc.c, 1e-4)
		near(t, tc.hex+" H", got.H, tc.h, 1e-2)
	}
}

func TestGreyHasNoChroma(t *testing.T) {
	got := OklabOf(Hex("#808080")).OKLCH()
	near(t, "L", got.L, 0.59987, 1e-4)
	if got.C > 1e-4 {
		t.Errorf("C = %g, want ~0", got.C)
	}
}

func TestOKLCHToSRGB(t *testing.T) {
	cases := []struct {
		in   OKLCH
		want color.NRGBA
	}{
		{OKLCH{0.80, 0.12, 35}, color.NRGBA{255, 161, 137, 255}}, // out of gamut, red clipped
		{OKLCH{0.34, 0.09, 35}, color.NRGBA{94, 35, 20, 255}},
		{OKLCH{0.80, 0.12, 265}, color.NRGBA{152, 188, 255, 255}},
		{OKLCH{0.30, 0.04, 75}, color.NRGBA{58, 43, 22, 255}},
	}
	for _, tc := range cases {
		if got := tc.in.NRGBA(1); !closeRGB(got, tc.want, 1) {
			t.Errorf("%v -> %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAlphaIsApplied(t *testing.T) {
	if got := (OKLCH{0.8, 0.12, 35}).NRGBA(0.14).A; got != 36 {
		t.Errorf("alpha = %d, want 36", got)
	}
}

func TestMix(t *testing.T) {
	a, b := OKLCH{0.2, 0, 0}.Oklab(), OKLCH{0.8, 0, 0}.Oklab()
	near(t, "start", Mix(a, b, 0).L, 0.2, 1e-9)
	near(t, "middle", Mix(a, b, 0.5).L, 0.5, 1e-9)
	near(t, "end", Mix(a, b, 1).L, 0.8, 1e-9)
}

func TestPaperA(t *testing.T) {
	if got := PaperA(0.14); got != (color.NRGBA{243, 242, 242, 36}) {
		t.Errorf("PaperA(0.14) = %v", got)
	}
}
