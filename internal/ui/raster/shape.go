package raster

import (
	"image"
	"math"
)

// Kind selects how a Shape is drawn.
type Kind uint8

const (
	// Fill covers the rounded rectangle.
	Fill Kind = iota
	// Ring is a band from RingOffset to RingOffset+RingWidth px outside the
	// edge. A negative offset moves it inside, as a CSS border would be.
	Ring
	// Glow is a CSS box-shadow with GlowSpread and GlowBlur. Like a
	// box-shadow it is transparent inside the rectangle.
	Glow
	// Shadow is a filled rectangle grown by GlowSpread and blurred by
	// GlowBlur, for drop shadows under art and dialogs.
	Shadow
	// Sector is a pie slice of the circle inside the rectangle: Sweep of a
	// turn, clockwise from 12 o'clock.
	Sector
)

// Shape is a W x H rounded rectangle with corner radius Radius. Its mask
// extends Outset px beyond the rectangle on every side so rings and glows fit.
type Shape struct {
	W, H                  int
	Radius                float64
	Kind                  Kind
	RingOffset, RingWidth float64
	GlowSpread, GlowBlur  float64
	Sweep                 float64 // Sector: how much of a turn, 0 to 1
}

// Outset is how far the mask extends beyond the rectangle on each side.
func (s Shape) Outset() int {
	switch s.Kind {
	case Ring:
		return int(math.Ceil(math.Max(0, s.RingOffset+s.RingWidth))) + 1
	case Glow, Shadow:
		return int(math.Ceil(s.GlowSpread+s.GlowBlur)) + 1
	}
	return 0
}

// Mask renders the shape as white pixels with coverage in alpha.
func (s Shape) Mask() *image.NRGBA {
	o := s.Outset()
	w, h := s.W+2*o, s.H+2*o
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	hw, hh := float64(s.W)/2, float64(s.H)/2
	r := math.Min(s.Radius, math.Min(hw, hh))
	sigma := s.GlowBlur / 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := sdRoundRect(float64(x)+0.5, float64(y)+0.5, cx, cy, hw, hh, r)
			var a float64
			switch s.Kind {
			case Fill:
				a = clamp01(0.5 - d)
			case Ring:
				mid := s.RingOffset + s.RingWidth/2
				a = clamp01(0.5 - (math.Abs(d-mid) - s.RingWidth/2))
			case Sector:
				a = clamp01(0.5-d) * s.sweep(float64(x)+0.5-cx, float64(y)+0.5-cy)
			case Glow, Shadow:
				if sigma <= 0 {
					a = clamp01(0.5 - (d - s.GlowSpread))
				} else {
					a = 0.5 * math.Erfc((d-s.GlowSpread)/(sigma*math.Sqrt2))
				}
			}
			if s.Kind == Glow {
				// Clip to outside the rectangle, as a box-shadow is.
				a *= clamp01(0.5 + d)
			}
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2] = 255, 255, 255
			img.Pix[i+3] = uint8(math.Round(a * 255))
		}
	}
	return img
}

// sweep is how much of a pixel at (dx, dy) from the centre lies within the
// slice, its edges eased over a pixel.
func (s Shape) sweep(dx, dy float64) float64 {
	if s.Sweep >= 1 {
		return 1
	}
	th := math.Atan2(dx, -dy) // from 12 o'clock, clockwise down the screen
	if th < 0 {
		th += 2 * math.Pi
	}
	end := math.Max(s.Sweep, 0) * 2 * math.Pi
	e := -math.Min(th-end, 2*math.Pi-th) // radians inside the slice; negative outside
	if th <= end {
		e = math.Min(th, end-th)
	}
	return clamp01(0.5 + e*math.Hypot(dx, dy))
}

// sdRoundRect is the signed distance from (px, py) to a rounded rectangle
// centred at (cx, cy) with half-size (hw, hh) and corner radius r. It is
// negative inside.
func sdRoundRect(px, py, cx, cy, hw, hh, r float64) float64 {
	qx := math.Abs(px-cx) - (hw - r)
	qy := math.Abs(py-cy) - (hh - r)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - r
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }
