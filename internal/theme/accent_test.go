package theme

import (
	"image"
	"image/color"
	"testing"
)

func solid(c color.NRGBA, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestDeriveFollowsTheRecipe(t *testing.T) {
	a := Derive(35, 0.12)
	if !closeRGB(a.Color, color.NRGBA{255, 161, 137, 255}, 1) {
		t.Errorf("Color = %v", a.Color)
	}
	if !closeRGB(a.Glow, color.NRGBA{94, 35, 20, 255}, 1) {
		t.Errorf("Glow = %v", a.Glow)
	}
	if a.Tint.A != 36 || a.Tint.R != a.Color.R {
		t.Errorf("Tint = %v, want Color at 14 %%", a.Tint)
	}
}

func TestDeriveClampsChroma(t *testing.T) {
	low := Derive(35, 0.03)
	if !closeRGB(low.Color, color.NRGBA{225, 177, 164, 255}, 1) {
		t.Errorf("low chroma accent = %v, want chroma raised to 0.06", low.Color)
	}
	if want := (OKLCH{0.34, 0.03, 35}).NRGBA(1); low.Glow != want {
		t.Errorf("low chroma glow = %v, want %v", low.Glow, want)
	}

	high := Derive(264, 0.31)
	if want := (OKLCH{0.80, 0.14, 264}).NRGBA(1); high.Color != want {
		t.Errorf("high chroma accent = %v, want %v", high.Color, want)
	}
	if want := (OKLCH{0.34, 0.09, 264}).NRGBA(1); high.Glow != want {
		t.Errorf("high chroma glow = %v, want %v", high.Glow, want)
	}
}

func TestFallbackIsGold(t *testing.T) {
	f := Fallback()
	if f.Color != Gold {
		t.Errorf("Color = %v, want gold", f.Color)
	}
	if !closeRGB(f.Glow, color.NRGBA{58, 43, 22, 255}, 1) {
		t.Errorf("Glow = %v", f.Glow)
	}
}

func TestLerpEndpoints(t *testing.T) {
	a, b := Derive(35, 0.12), Derive(265, 0.12)
	if got := a.Lerp(b, 0); !closeRGB(got.Color, a.Color, 1) {
		t.Errorf("t=0 gives %v, want %v", got.Color, a.Color)
	}
	if got := a.Lerp(b, 1); !closeRGB(got.Color, b.Color, 1) {
		t.Errorf("t=1 gives %v, want %v", got.Color, b.Color)
	}
}

func TestDominantSolidBlue(t *testing.T) {
	h, c, ok := Dominant(solid(Hex("#0000ff"), 100, 100))
	if !ok {
		t.Fatal("solid blue should have a dominant hue")
	}
	near(t, "hue", h, 264.05, 0.1)
	near(t, "chroma", c, 0.31321, 1e-3)
}

func TestDominantGreyIsNotOK(t *testing.T) {
	if _, _, ok := Dominant(solid(Hex("#808080"), 100, 100)); ok {
		t.Error("grey art must fall back to gold")
	}
}

func TestDominantPicksLargestCluster(t *testing.T) {
	img := solid(Hex("#ff0000"), 100, 100)
	for y := 0; y < 100; y++ {
		for x := 0; x < 70; x++ {
			img.SetNRGBA(x, y, Hex("#0000ff"))
		}
	}
	h, _, ok := Dominant(img)
	if !ok {
		t.Fatal("expected a dominant hue")
	}
	near(t, "hue", h, 264.05, 0.1)
}

func TestDominantIgnoresSpecksOfColour(t *testing.T) {
	img := solid(Hex("#808080"), 100, 100)
	for y := 0; y < 100; y++ {
		img.SetNRGBA(0, y, Hex("#ff0000"))
		img.SetNRGBA(1, y, Hex("#ff0000"))
	}
	if _, _, ok := Dominant(img); ok {
		t.Error("2 % colour should count as greyscale")
	}
}
