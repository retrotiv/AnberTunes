package raster

import "image"

// Blur blurs img in place with three passes of a box of radius r across and
// then down, which is close to a Gaussian with a standard deviation of about
// r. Each channel is blurred as stored, so a white mask's alpha spreads and
// its colour stays white. Edge pixels repeat outwards.
func Blur(img *image.NRGBA, r int) { blur(img, r, 0) }

// BlurAlpha is Blur for the alpha channel alone, for a mask whose colour
// doesn't matter. It does a quarter of the work.
func BlurAlpha(img *image.NRGBA, r int) { blur(img, r, 3) }

// blur blurs the channels from first to the last (alpha).
func blur(img *image.NRGBA, r, first int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if r <= 0 || w == 0 || h == 0 {
		return
	}
	n := max(w, h)
	col, tmp := make([]uint8, 4*n), make([]uint8, 4*n)
	for range 3 {
		for y := range h {
			o := y * img.Stride
			box(img.Pix[o:o+4*w], tmp, w, r, first)
		}
		for x := range w {
			for y := range h {
				copy(col[4*y:4*y+4], img.Pix[y*img.Stride+4*x:])
			}
			box(col[:4*h], tmp, h, r, first)
			for y := range h {
				copy(img.Pix[y*img.Stride+4*x:y*img.Stride+4*x+4], col[4*y:])
			}
		}
	}
}

// box replaces channels first to 3 of each of the n four-channel pixels in
// px with the mean of the 2r+1 around it, using tmp as scratch.
func box(px, tmp []uint8, n, r, first int) {
	div := 2*r + 1
	for c := first; c < 4; c++ {
		at := func(i int) int { return int(px[4*min(max(i, 0), n-1)+c]) }
		sum := 0
		for i := -r; i <= r; i++ {
			sum += at(i)
		}
		for i := range n {
			tmp[4*i+c] = uint8((sum + div/2) / div)
			sum += at(i+r+1) - at(i-r)
		}
		for i := range n {
			px[4*i+c] = tmp[4*i+c]
		}
	}
}
