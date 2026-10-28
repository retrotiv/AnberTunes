package raster

import (
	"image"
	"image/color"
	"testing"
)

func TestBlurKeepsAnEvenImageEven(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 9, 7))
	for i := range img.Pix {
		img.Pix[i] = 90
	}
	Blur(img, 3)
	for i, v := range img.Pix {
		if v != 90 {
			t.Fatalf("byte %d is %d after blurring an even image", i, v)
		}
	}
}

func TestBlurSpreadsAPointEvenly(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 31, 31))
	img.SetNRGBA(15, 15, color.NRGBA{255, 255, 255, 255})
	Blur(img, 2)
	at := func(x, y int) uint8 { return img.NRGBAAt(x, y).R }
	if at(15, 15) == 255 || at(15, 15) == 0 {
		t.Fatalf("centre %d after blurring", at(15, 15))
	}
	for _, d := range []int{1, 3, 5} {
		// Rounding to 8 bits after the across passes and again after the down
		// passes can leave the two directions one step apart.
		a, b, c, e := at(15-d, 15), at(15+d, 15), at(15, 15-d), at(15, 15+d)
		if a != b || c != e || max(a, c)-min(a, c) > 1 {
			t.Fatalf("at distance %d: %d %d %d %d; want the same all round", d, a, b, c, e)
		}
	}
	if at(15, 15) <= at(16, 15) || at(16, 15) < at(18, 15) {
		t.Fatal("the blur doesn't fall off from the centre")
	}
}

func TestBlurOfAWhiteMaskStaysWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for y := range 10 {
		for x := range 20 {
			a := uint8(0)
			if x == 10 {
				a = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, a})
		}
	}
	Blur(img, 3)
	for y := range 10 {
		for x := range 20 {
			if c := img.NRGBAAt(x, y); c.R != 255 || c.G != 255 || c.B != 255 {
				t.Fatalf("(%d, %d) is %v; the colour of a white mask must stay white", x, y, c)
			}
		}
	}
	if img.NRGBAAt(12, 5).A == 0 {
		t.Fatal("the alpha didn't spread")
	}
}

func TestBlurAlphaLeavesTheColour(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 21, 21))
	for y := range 21 {
		for x := range 21 {
			img.SetNRGBA(x, y, color.NRGBA{10, 20, 30, 0})
		}
	}
	img.SetNRGBA(10, 10, color.NRGBA{200, 20, 30, 255})
	BlurAlpha(img, 2)
	if c := img.NRGBAAt(10, 10); c.R != 200 || c.A == 255 || c.A == 0 {
		t.Fatalf("centre %v: the colour must stay and the alpha spread", c)
	}
	if c := img.NRGBAAt(12, 10); c.R != 10 || c.A == 0 {
		t.Fatalf("(12, 10) %v", c)
	}
}
