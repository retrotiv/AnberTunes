package widgets

import (
	"image/color"

	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// Dialog is what a dialog shows.
type Dialog struct {
	Icon      string
	IconColor color.NRGBA // a plain icon's colour
	Circle    bool        // the icon in a 52 px accent circle instead
	Title     string
	// An item the dialog is about, as a song: its art, and two lines beside
	// it. Art nil leaves it out.
	Art                   *ArtSpec
	ItemTitle, ItemDetail string
	Body                  string
	Pills                 []DialogPill

	body Wrapped // Body's lines, wrapped on the first draw
}

// DialogPill is one of a dialog's buttons.
type DialogPill struct {
	Icon, Label string
}

// Dialog geometry: 660 wide, padding 36 40, radius 20.
const (
	dialogW   = 660
	dialogPad = 40
	dialogTop = 36
	dialogArt = 64
)

var (
	dialogTitle = text.Style{Face: text.OutfitSemiBold, Size: 42}
	dialogBody  = text.Style{Face: text.Figtree, Size: 26}
)

// DrawDialog draws d over a scrim. shown, from 0 to 1, fades it in and grows
// the panel from 96 %; focus is each pill's focus strength. d keeps its body
// wrapped from one frame to the next.
func DrawDialog(u *ui.Context, d *Dialog, shown float32, focus []float32) {
	if shown <= 0 {
		return
	}
	lines := d.body.Lines(u, d.Body, dialogBody, dialogW-2*dialogPad)
	iconH := float32(36)
	if d.Circle {
		iconH = 52
	}
	headH := max(iconH, u.LineHeight(dialogTitle))
	h := dialogTop + headH + 20 + float32(len(lines))*u.LineHeight(dialogBody) + 28 + PillHeight + dialogTop
	if d.Art != nil {
		h += dialogArt + 20
	}
	p := BeginPanel(u, h, shown)
	defer p.End(u)
	box := p.Box

	x, y := box.X+dialogPad, box.Y+dialogTop
	if d.Circle {
		u.Shape(raster.Shape{W: 52, H: 52, Radius: 26, Kind: raster.Fill}, gfx.Rect{X: x, Y: y + (headH-52)/2, W: 52, H: 52}, u.Accent.Color)
		u.Icon(d.Icon, 28, 2, false, x+12, y+(headH-28)/2, theme.Ground)
	} else {
		u.Icon(d.Icon, 36, 2, false, x, y+(headH-36)/2, d.IconColor)
	}
	tx := x + iconH + 16
	u.Text(u.Fit(d.Title, dialogTitle, box.X+box.W-dialogPad-tx), dialogTitle, tx, u.CenterY(dialogTitle, y, headH), theme.Paper)
	y += headH + 20

	if d.Art != nil {
		Art(u, *d.Art, dialogArt, 8, gfx.Rect{X: x, Y: y, W: dialogArt, H: dialogArt})
		ix, iw := x+dialogArt+16, dialogW-2*dialogPad-dialogArt-16
		ty := y + (dialogArt-u.LineHeight(text.Body)-u.LineHeight(text.Meta))/2
		u.Text(u.Fit(d.ItemTitle, text.Body, float32(iw)), text.Body, ix, ty, theme.Paper)
		u.Text(u.Fit(d.ItemDetail, text.Meta, float32(iw)), text.Meta, ix, ty+u.LineHeight(text.Body), theme.Ink3)
		y += dialogArt + 20
	}
	for _, l := range lines {
		u.Text(l, dialogBody, x, y, theme.Ink2)
		y += u.LineHeight(dialogBody)
	}
	y += 28
	// A dialog holds up to three pills; they fit the panel.
	for i, p := range d.Pills {
		k := float32(0)
		if i < len(focus) {
			k = focus[i]
		}
		PillGlow(u, p.Icon, p.Label, x, y, k)
		x += Pill(u, p.Icon, p.Label, x, y, k) + 12
	}
}
