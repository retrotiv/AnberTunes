package raster

import (
	"image"
	"math"

	"github.com/ujjwal313/boombrick/internal/theme"
)

// Stop is a colour stop at Pos along the gradient ray: 0 at the centre, 1 at
// the edge of the ellipse.
type Stop struct {
	Pos   float64
	Color theme.Oklab
}

// RadialGradient renders an elliptical gradient like CSS
// radial-gradient(rx ry at cx cy, …), interpolating in Oklab. Positions past
// the last stop take the last colour.
func RadialGradient(w, h int, cx, cy, rx, ry float64, stops []Stop) *image.NRGBA {
	// Colours are precomputed along the ray, and each column's distance from
	// the centre, so each pixel costs an add, a sqrt and a multiply.
	const lutSize = 1024
	maxT := 0.0
	for _, p := range [4][2]float64{{0, 0}, {float64(w), 0}, {0, float64(h)}, {float64(w), float64(h)}} {
		maxT = math.Max(maxT, math.Hypot((p[0]-cx)/rx, (p[1]-cy)/ry))
	}
	var lut [lutSize + 1][4]uint8
	for i := range lut {
		c := sample(stops, maxT*float64(i)/lutSize).NRGBA(1)
		lut[i] = [4]uint8{c.R, c.G, c.B, 255}
	}
	dx2 := make([]float64, w)
	for x := range dx2 {
		dx := (float64(x) + 0.5 - cx) / rx
		dx2[x] = dx * dx
	}
	k := lutSize / maxT
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		dy := (float64(y) + 0.5 - cy) / ry
		dy2 := dy * dy
		row := img.Pix[y*img.Stride : y*img.Stride+4*w]
		for x, d := range dx2 {
			*(*[4]uint8)(row[4*x : 4*x+4]) = lut[int(math.Sqrt(d+dy2)*k)]
		}
	}
	return img
}

func sample(stops []Stop, t float64) theme.Oklab {
	if t <= stops[0].Pos {
		return stops[0].Color
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].Pos {
			a, b := stops[i-1], stops[i]
			return theme.Mix(a.Color, b.Color, (t-a.Pos)/(b.Pos-a.Pos))
		}
	}
	return stops[len(stops)-1].Color
}
