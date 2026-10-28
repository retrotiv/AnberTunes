package raster

import (
	"image"
	"image/color"
	"math"

	"github.com/ujjwal313/boombrick/internal/theme"
)

// Placeholder draws generated cover art for items without artwork. glyph is an optional white mask of the item's first
// letter, drawn at (gx, gy) in variant 2.
func Placeholder(size int, radius, hue float64, variant int, glyph *image.NRGBA, gx, gy int) *image.NRGBA {
	const c = 0.12
	k := func(l, ch, h float64) color.NRGBA { return theme.OKLCH{L: l, C: ch, H: h}.NRGBA(1) }
	s := float64(size)
	variant = ((variant % 4) + 4) % 4

	var paint func(x, y float64) color.NRGBA
	switch variant {
	case 0:
		base, band, sun := k(0.46, c, hue), k(0.27, 0.8*c, hue), k(0.84, 0.8*c, hue+25)
		// A CSS circle gradient sizes its stops against the farthest corner.
		sunR := 0.21 * math.Hypot(0.68, 0.64) * s
		paint = func(x, y float64) color.NRGBA {
			col := base
			if y >= 0.64*s {
				col = band
			}
			return over(col, sun, clamp01(0.5-(math.Hypot(x-0.68*s, y-0.36*s)-sunR)))
		}
	case 1:
		dark, light := k(0.32, c, hue), k(0.62, c, hue+18)
		paint = func(x, y float64) color.NRGBA {
			if math.Mod((s-y)/s, 0.15) < 0.11 {
				return dark
			}
			return light
		}
	case 2:
		a, b := k(0.74, c, hue), k(0.62, c, hue-15)
		paint = func(x, y float64) color.NRGBA {
			if (x+y)/(2*s) < 0.6 {
				return a
			}
			return b
		}
	case 3:
		cols := [4]color.NRGBA{k(0.30, c, hue), k(0.56, c, hue+40), k(0.76, 0.9*c, hue), k(0.44, c, hue)}
		paint = func(x, y float64) color.NRGBA {
			deg := math.Atan2(x-0.42*s, -(y-0.58*s)) * 180 / math.Pi // clockwise from north
			f := math.Mod(deg-90+720, 360) / 360                     // "from 90deg"
			return cols[int(f*4)%4]
		}
	}

	ink := k(0.26, c, hue)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	r := math.Min(radius, s/2)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			col := paint(fx, fy)
			if glyph != nil && variant == 2 {
				if p := image.Pt(x-gx, y-gy); p.In(glyph.Rect) {
					col = over(col, ink, float64(glyph.NRGBAAt(p.X, p.Y).A)/255)
				}
			}
			d := sdRoundRect(fx, fy, s/2, s/2, s/2, s/2, r)
			col = over(col, white, 0.08*clamp01(0.5-(math.Abs(d+0.5)-0.5))) // inner 1 px hairline
			col.A = uint8(math.Round(255 * clamp01(0.5-d)))
			img.SetNRGBA(x, y, col)
		}
	}
	return img
}

// over composites src at coverage a onto an opaque dst.
func over(dst, src color.NRGBA, a float64) color.NRGBA {
	if a <= 0 {
		return dst
	}
	mix := func(d, s uint8) uint8 { return uint8(math.Round(float64(d) + (float64(s)-float64(d))*a)) }
	return color.NRGBA{R: mix(dst.R, src.R), G: mix(dst.G, src.G), B: mix(dst.B, src.B), A: 255}
}
