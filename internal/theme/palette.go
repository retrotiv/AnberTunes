package theme

import (
	"image"
	"image/color"
	"math"
)

// Dominant returns the dominant hue and chroma of img, the album accent's
// first step. The image is sampled on a 48x48 grid. Pixels with chroma
// above 0.04 are bucketed by hue into 10° bins, and the bin with the most
// pixels wins, counting its two neighbours so that a hue on a bin edge isn't
// split. ok is false when fewer than 3 % of samples carry colour, which is
// treated as greyscale art.
func Dominant(img image.Image) (hue, chroma float64, ok bool) {
	const grid, bins = 48, 36
	b := img.Bounds()
	if b.Empty() {
		return 0, 0, false
	}
	var (
		count      [bins]int
		sumC       [bins]float64
		sumX, sumY [bins]float64
		total      int
	)
	for gy := 0; gy < grid; gy++ {
		for gx := 0; gx < grid; gx++ {
			x := b.Min.X + (2*gx+1)*b.Dx()/(2*grid)
			y := b.Min.Y + (2*gy+1)*b.Dy()/(2*grid)
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A < 128 {
				continue
			}
			total++
			p := OklabOf(c).OKLCH()
			if p.C <= 0.04 {
				continue
			}
			i := int(p.H/10) % bins
			rad := p.H * math.Pi / 180
			count[i]++
			sumC[i] += p.C
			sumX[i] += math.Cos(rad)
			sumY[i] += math.Sin(rad)
		}
	}

	best, bestN := -1, 0
	for i := 0; i < bins; i++ {
		n := count[(i+bins-1)%bins] + count[i] + count[(i+1)%bins]
		if n > bestN {
			best, bestN = i, n
		}
	}
	if best < 0 || bestN*100 < total*3 {
		return 0, 0, false
	}

	var c, sx, sy float64
	for _, j := range [3]int{(best + bins - 1) % bins, best, (best + 1) % bins} {
		c += sumC[j]
		sx += sumX[j]
		sy += sumY[j]
	}
	hue = math.Atan2(sy, sx) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	return hue, c / float64(bestN), true
}
