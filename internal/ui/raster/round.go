package raster

import (
	"image"
	"math"
)

// RoundCorners makes img transparent outside a rounded rectangle of its size
// with corner radius r pixels, antialiased as a Fill shape is. Only the
// corner squares are touched.
func RoundCorners(img *image.NRGBA, r float64) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	r = math.Min(r, math.Min(float64(w), float64(h))/2)
	if r <= 0 {
		return
	}
	n := int(math.Ceil(r))
	hw, hh := float64(w)/2, float64(h)/2
	for y := range h {
		if y >= n && y < h-n {
			continue
		}
		for x := range w {
			if x >= n && x < w-n {
				continue
			}
			a := clamp01(0.5 - sdRoundRect(float64(x)+0.5, float64(y)+0.5, hw, hh, hw, hh, r))
			if a < 1 {
				i := img.PixOffset(x+img.Rect.Min.X, y+img.Rect.Min.Y)
				img.Pix[i+3] = uint8(float64(img.Pix[i+3])*a + 0.5)
			}
		}
	}
}
