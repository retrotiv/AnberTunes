package widgets

import (
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// RowHeight is a list row's height.
const RowHeight = 80

// RowRadius is a list row's corner radius, for its focus treatment.
const RowRadius = 14

// Row is what a list row shows.
type Row struct {
	Index    string   // a leading track or queue number, or ""
	Grip     bool     // a grip icon in place of the index, for moving rows
	Art      *ArtSpec // nil for none
	Round    bool     // round art, for artists
	Title    string
	Subtitle string  // "" centres the title
	Duration string  // right-aligned, or ""
	Playing  bool    // the playing track: the title in the accent, with a note
	Trail    float32 // room kept at the right for what the caller draws there (see TrailBox)
}

// Row layout: padding 16, gaps 18, the index in 36, art 58, the duration in 60.
const (
	rowPad   = 16
	rowGap   = 18
	indexW   = 36
	rowArt   = 58
	durW     = 60
	noteSize = 20
)

var rowNumbers = text.Style{Face: text.Figtree, Size: 22, Tabular: true}

// ListRow draws r across box with focus strength k: rows keep their size and
// gain the accent tint and ring. RowGlow, with RowRadius, draws the glow.
func ListRow(u *ui.Context, r Row, box gfx.Rect, k float32) {
	RowFocus(u, box, RowRadius, k)
	x, right := box.X+rowPad, box.X+box.W-rowPad
	if r.Trail > 0 {
		right -= r.Trail + rowGap
	}
	switch {
	case r.Grip:
		u.Icon("grip", 24, 2, false, x+indexW-24, box.Y+(box.H-24)/2, theme.Ink3)
		x += indexW + rowGap
	case r.Index != "":
		w := u.TextWidth(r.Index, rowNumbers)
		u.Text(r.Index, rowNumbers, x+indexW-w, u.CenterY(rowNumbers, box.Y, box.H), theme.Ink3)
		x += indexW + rowGap
	}
	if r.Art != nil {
		radius := 8.0
		if r.Round {
			radius = rowArt / 2
		}
		Art(u, *r.Art, rowArt, radius, gfx.Rect{X: x, Y: box.Y + (box.H-rowArt)/2, W: rowArt, H: rowArt})
		x += rowArt + rowGap
	}
	if r.Duration != "" {
		w := u.TextWidth(r.Duration, rowNumbers)
		u.Text(r.Duration, rowNumbers, right-w, u.CenterY(rowNumbers, box.Y, box.H), theme.Ink2)
		right -= durW + rowGap
	}
	titleH := u.LineHeight(text.Body)
	ty := u.CenterY(text.Body, box.Y, box.H)
	if r.Subtitle != "" {
		ty = box.Y + (box.H-titleH-u.LineHeight(text.Meta))/2
	}
	col, w := theme.Paper, right-x
	if r.Playing {
		col, w = u.Accent.Color, w-noteSize-8
	}
	tw := u.Text(u.Fit(r.Title, text.Body, w), text.Body, x, ty, col)
	if r.Playing { // the fonts have no ♪, so the music icon stands in
		u.Icon("music", noteSize, 2, false, x+tw+8, ty+(titleH-noteSize)/2, col)
	}
	if r.Subtitle != "" {
		u.Text(u.Fit(r.Subtitle, text.Meta, right-x), text.Meta, x, ty+titleH, theme.Ink3)
	}
}

// TrailBox is the room, w wide, that a row drawn into box keeps at its right.
func TrailBox(box gfx.Rect, w float32) gfx.Rect {
	return gfx.Rect{X: box.X + box.W - rowPad - w, Y: box.Y, W: w, H: box.H}
}
