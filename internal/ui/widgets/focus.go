package widgets

import (
	"image/color"

	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
)

// FocusGlow draws the card and button focus glow around box: 44 px blur, 8 px
// spread, accent at 38 %. strength fades it while focus moves. It is apart
// from the ring so a list or grid can clip its items at its top and still let
// their glows fade out past it. Draw it before FocusRing.
func FocusGlow(u *ui.Context, box gfx.Rect, radius float64, strength float32) {
	if strength <= 0 {
		return
	}
	u.Shape(raster.Shape{W: int(box.W), H: int(box.H), Radius: radius, Kind: raster.Glow, GlowSpread: 8, GlowBlur: 44},
		box, fade(u.Accent.Color, 0.38*strength))
}

// FocusRing draws the card and button focus ring around box: a 4 px accent
// ring and a 3 px gap in gap colour between the ring and the element.
// strength fades it while focus moves. Draw it before the element itself.
func FocusRing(u *ui.Context, box gfx.Rect, radius float64, strength float32, gap color.NRGBA) {
	if strength <= 0 {
		return
	}
	w, h := int(box.W), int(box.H)
	u.Shape(raster.Shape{W: w, H: h, Radius: radius, Kind: raster.Ring, RingWidth: 7}, box, fade(u.Accent.Color, strength))
	u.Shape(raster.Shape{W: w, H: h, Radius: radius, Kind: raster.Ring, RingWidth: 3}, box, fade(gap, strength))
}

// RowGlow draws the list-row focus glow: 30 px, accent at 30 %. Draw it
// before RowFocus.
func RowGlow(u *ui.Context, box gfx.Rect, radius float64, strength float32) {
	if strength <= 0 {
		return
	}
	u.Shape(raster.Shape{W: int(box.W), H: int(box.H), Radius: radius, Kind: raster.Glow, GlowSpread: 2, GlowBlur: 30},
		box, fade(u.Accent.Color, 0.30*strength))
}

// RowFocus draws the list-row focus treatment: accent tint and a 4 px ring.
// Draw it before the row's content.
func RowFocus(u *ui.Context, box gfx.Rect, radius float64, strength float32) {
	if strength <= 0 {
		return
	}
	w, h := int(box.W), int(box.H)
	acc := u.Accent.Color
	u.Shape(raster.Shape{W: w, H: h, Radius: radius, Kind: raster.Fill}, box, fade(acc, 0.14*strength))
	u.Shape(raster.Shape{W: w, H: h, Radius: radius, Kind: raster.Ring, RingWidth: 4}, box, fade(acc, strength))
}
