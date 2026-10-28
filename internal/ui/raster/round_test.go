package raster

import (
	"image"
	"testing"
)

func opaqueSquare(n int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	return img
}

func TestRoundCorners(t *testing.T) {
	img := opaqueSquare(40)
	RoundCorners(img, 10)
	alpha := func(x, y int) uint8 { return img.NRGBAAt(x, y).A }
	if alpha(0, 0) != 0 || alpha(39, 39) != 0 || alpha(39, 0) != 0 {
		t.Fatalf("corner alphas %d %d %d, want 0", alpha(0, 0), alpha(39, 39), alpha(39, 0))
	}
	if alpha(20, 0) != 255 || alpha(0, 20) != 255 || alpha(20, 20) != 255 {
		t.Fatal("the edges' middles or the centre lost opacity")
	}
	if a := alpha(2, 3); a == 0 || a == 255 {
		t.Fatalf("the curve at (2, 3) has alpha %d; want it antialiased", a)
	}
}

func TestRoundCornersMakesACircle(t *testing.T) {
	img := opaqueSquare(30)
	RoundCorners(img, 15)
	if img.NRGBAAt(3, 3).A != 0 || img.NRGBAAt(15, 15).A != 255 || img.NRGBAAt(15, 1).A != 255 {
		t.Fatal("not a circle")
	}
}
