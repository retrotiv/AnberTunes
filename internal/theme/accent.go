package theme

import (
	"image/color"
	"math"
)

// Accent is the set of colours a screen takes from the playing track's art.
type Accent struct {
	Hue, Chroma float64
	Glow        color.NRGBA // background gradient
	Color       color.NRGBA // focus rings, progress, active tab, lyric highlight
	Tint        color.NRGBA // Color at 14 %, behind focused rows
}

// Derive builds the accent for a dominant hue and chroma.
func Derive(hue, chroma float64) Accent {
	c := OKLCH{L: 0.80, C: clamp(chroma, 0.06, 0.14), H: hue}.NRGBA(1)
	return Accent{
		Hue:    hue,
		Chroma: chroma,
		Glow:   OKLCH{L: 0.34, C: math.Min(chroma, 0.09), H: hue}.NRGBA(1),
		Color:  c,
		Tint:   withAlpha(c, 0.14),
	}
}

// Fallback is the accent for greyscale or missing art: brand gold over a warm
// neutral glow.
func Fallback() Accent {
	return Accent{Hue: 75, Glow: FallbackGlow, Color: Gold, Tint: withAlpha(Gold, 0.14)}
}

// paletteHue maps a fixed interface-colour name to its hue in OKLCH.
var paletteHue = map[string]float64{
	"amber": 75, "rose": 22, "magenta": 330, "purple": 295,
	"blue": 255, "teal": 195, "green": 150,
}

// Named is the fixed accent for a palette colour name; ok is false for "album"
// or any unknown name, which means the colour follows the album art.
func Named(name string) (Accent, bool) {
	h, ok := paletteHue[name]
	if !ok {
		return Accent{}, false
	}
	return Derive(h, 0.12), true
}

// Lerp blends towards b in Oklab, used for the 600 ms colour change between
// tracks.
func (a Accent) Lerp(b Accent, t float64) Accent {
	c := lerpColor(a.Color, b.Color, t)
	return Accent{
		Hue:    b.Hue,
		Chroma: b.Chroma,
		Glow:   lerpColor(a.Glow, b.Glow, t),
		Color:  c,
		Tint:   withAlpha(c, 0.14),
	}
}

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	return Mix(OklabOf(a), OklabOf(b), t).NRGBA(1)
}

func withAlpha(c color.NRGBA, a float64) color.NRGBA {
	c.A = to8(a)
	return c
}

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }
