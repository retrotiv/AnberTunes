package widgets

import (
	"math"

	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// PillHeight is a pill button's height.
const PillHeight = 58

var pillText = text.Style{Face: text.Figtree, Size: 24}

// PillWidth measures a pill button: 22 padding each side, and a 24 px icon 10
// before the label. An icon alone makes a round button.
func PillWidth(u *ui.Context, icon, label string) float32 {
	if label == "" {
		return PillHeight
	}
	w := 44 + u.TextWidth(label, pillText)
	if icon != "" {
		w += 24 + 10
	}
	return float32(math.Ceil(float64(w)))
}

// Pill draws a pill button with its top-left at (x, y) and returns its width.
// At rest it has a 1.5 px border of paper at 30 %; focus k brings in a 2 px
// accent border with the ring, and scales it to 105 %. PillGlow draws its
// glow.
func Pill(u *ui.Context, icon, label string, x, y, k float32) float32 {
	w := PillWidth(u, icon, label)
	box := gfx.Rect{X: x, Y: y, W: w, H: PillHeight}
	u.PushScale(x+w/2, y, 1+0.05*k)
	defer u.Pop()
	FocusRing(u, box, PillHeight/2, k, theme.Ground)
	edge := raster.Shape{W: int(w), H: PillHeight, Radius: PillHeight / 2, Kind: raster.Ring, RingOffset: -1.5, RingWidth: 1.5}
	u.Shape(edge, box, fade(theme.PaperA(0.30), 1-k))
	edge.RingOffset, edge.RingWidth = -2, 2
	u.Shape(edge, box, fade(u.Accent.Color, k))
	cx := x + 22
	if label == "" {
		cx = x + (w-24)/2
	}
	if icon != "" {
		u.Icon(icon, 24, 2, icon == "play", cx, y+(PillHeight-24)/2, theme.Paper)
		cx += 24 + 10
	}
	u.Text(label, pillText, cx, u.CenterY(pillText, y, PillHeight), theme.Paper)
	return w
}

// PillGlow draws the focus glow of the pill Pill draws with the same
// arguments, scaled as the pill is. Draw it first.
func PillGlow(u *ui.Context, icon, label string, x, y, k float32) {
	if k <= 0 {
		return
	}
	w := PillWidth(u, icon, label)
	u.PushScale(x+w/2, y, 1+0.05*k)
	FocusGlow(u, gfx.Rect{X: x, Y: y, W: w, H: PillHeight}, PillHeight/2, k)
	u.Pop()
}
