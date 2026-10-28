package widgets

import (
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// FiltersHeight is the filter row's height.
const FiltersHeight = 56

// The filter row: Heading at 28 with an optional
// tabular count in ink3; the chosen filter sits on a tinted pill.
var (
	filterText = text.Style{Face: text.OutfitSemiBold, Size: 28}
	countText  = text.Style{Face: text.Figtree, Size: 20, Tabular: true}
)

const (
	filterPill = 44
	filterPad  = 16
	filterGap  = 8 // name to count
)

// count is counts[i], or "" without counts.
func count(counts []string, i int) string {
	if counts == nil {
		return ""
	}
	return counts[i]
}

func filterWidth(u *ui.Context, name, count string) float32 {
	w := filterPad + u.TextWidth(name, filterText)
	if count != "" {
		w += filterGap + u.TextWidth(count, countText)
	}
	return w + filterPad
}

// FiltersWidth measures the row Filters draws.
func FiltersWidth(u *ui.Context, names, counts []string) float32 {
	w := glyphWidth(u, "L2", TabPill) + 18
	for i, name := range names {
		w += filterWidth(u, name, count(counts, i)) + 8
	}
	return w + 10 + glyphWidth(u, "R2", TabPill)
}

// Filters draws L2, the filters named, each with its count when counts is
// given, and R2, from (x, y), with filter active on its tinted pill. It
// returns the row's width.
func Filters(u *ui.Context, names, counts []string, active int, x, y float32) float32 {
	x0 := x
	gy := y + (FiltersHeight-TabPill.Height)/2
	x += KeyGlyph(u, "L2", x, gy, TabPill, theme.PaperA(0.45)) + 18
	for i, name := range names {
		c := count(counts, i)
		w := filterWidth(u, name, c)
		box := gfx.Rect{X: x, Y: y + (FiltersHeight-filterPill)/2, W: w, H: filterPill}
		col := theme.Ink2
		if i == active {
			u.Shape(raster.Shape{W: int(w), H: filterPill, Radius: filterPill / 2, Kind: raster.Fill}, box, u.Accent.Tint)
			col = theme.Paper
		}
		tx := x + filterPad
		ty := u.CenterY(filterText, box.Y, filterPill)
		tx += u.Text(name, filterText, tx, ty, col)
		if c != "" {
			u.Text(c, countText, tx+filterGap, u.Baseline(countText, ty+u.Ascent(filterText)), theme.Ink3)
		}
		x += w + 8
	}
	x += 10
	x += KeyGlyph(u, "R2", x, gy, TabPill, theme.PaperA(0.45))
	return x - x0
}
