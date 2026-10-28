// Package theme holds Boombrick's design tokens and the colour maths behind
// them.
package theme

import (
	"fmt"
	"image/color"
	"math"
)

// Oklab is a colour in Björn Ottosson's Oklab space.
type Oklab struct{ L, A, B float64 }

// OKLCH is Oklab in polar form: lightness, chroma and hue in degrees.
type OKLCH struct{ L, C, H float64 }

// Oklab converts to rectangular form.
func (c OKLCH) Oklab() Oklab {
	h := c.H * math.Pi / 180
	return Oklab{L: c.L, A: c.C * math.Cos(h), B: c.C * math.Sin(h)}
}

// OKLCH converts to polar form with the hue in [0, 360).
func (c Oklab) OKLCH() OKLCH {
	h := math.Atan2(c.B, c.A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return OKLCH{L: c.L, C: math.Hypot(c.A, c.B), H: h}
}

// NRGBA converts to 8-bit sRGB with opacity alpha (0-1). Out-of-gamut
// channels are clipped, as browsers clip them.
func (c Oklab) NRGBA(alpha float64) color.NRGBA {
	l := cube(c.L + 0.3963377774*c.A + 0.2158037573*c.B)
	m := cube(c.L - 0.1055613458*c.A - 0.0638541728*c.B)
	s := cube(c.L - 0.0894841775*c.A - 1.2914855480*c.B)
	r := 4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return color.NRGBA{R: encode(r), G: encode(g), B: encode(b), A: to8(alpha)}
}

// NRGBA converts to 8-bit sRGB with opacity alpha (0-1).
func (c OKLCH) NRGBA(alpha float64) color.NRGBA { return c.Oklab().NRGBA(alpha) }

// OklabOf converts an sRGB colour, ignoring its alpha.
func OklabOf(c color.NRGBA) Oklab {
	r, g, b := linear[c.R], linear[c.G], linear[c.B]
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return Oklab{
		L: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		A: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		B: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// Mix interpolates from a to b in Oklab, the space CSS uses for gradients
// between oklch() colours.
func Mix(a, b Oklab, t float64) Oklab {
	return Oklab{L: a.L + (b.L-a.L)*t, A: a.A + (b.A-a.A)*t, B: a.B + (b.B-a.B)*t}
}

// Hex parses "#rrggbb". It panics on malformed input because it is only used
// for constants.
func Hex(s string) color.NRGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		panic(fmt.Sprintf("theme.Hex(%q): %v", s, err))
	}
	return color.NRGBA{R: r, G: g, B: b, A: 255}
}

// linear maps 8-bit sRGB values to linear light.
var linear [256]float64

func init() {
	for i := range linear {
		x := float64(i) / 255
		if x <= 0.04045 {
			linear[i] = x / 12.92
		} else {
			linear[i] = math.Pow((x+0.055)/1.055, 2.4)
		}
	}
}

func encode(x float64) uint8 {
	x = clamp01(x)
	if x <= 0.0031308 {
		x *= 12.92
	} else {
		x = 1.055*math.Pow(x, 1/2.4) - 0.055
	}
	return to8(x)
}

func to8(x float64) uint8       { return uint8(math.Round(clamp01(x) * 255)) }
func cube(x float64) float64    { return x * x * x }
func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }
