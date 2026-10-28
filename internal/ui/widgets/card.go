package widgets

import (
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// Card geometry.
const (
	CardRadius = 12
	CaptionGap = 12 // art to caption
)

// CardItem is what a card or tile shows.
type CardItem struct {
	Title, Subtitle string
	Art             ArtSpec
	Source          media.Source
	Badge           bool    // a source badge and progress strip, as on Continue listening
	Progress        float64 // 0-1
}

// CardHeight is the height of a captioned card with art size px square.
func CardHeight(u *ui.Context, size float32) float32 {
	return size + CaptionGap + 2 + u.LineHeight(text.Body) + u.LineHeight(text.Meta)
}

// Card draws a card, or a tile when caption is false, with its art's top-left
// at (x, y). As focus k goes from 0 to 1 it scales to 106 % from its top
// centre and gains the focus ring; CardGlow draws its glow.
func Card(u *ui.Context, it CardItem, x, y, size, k float32, caption bool) {
	u.PushScale(x+size/2, y, 1+0.06*k)
	defer u.Pop()

	box := gfx.Rect{X: x, Y: y, W: size, H: size}
	FocusRing(u, box, CardRadius, k, theme.Ground)
	Art(u, it.Art, int(size), CardRadius, box)
	if it.Badge {
		Badge(u, it.Source, x+10, y+10, false)
		progressStrip(u, box, it.Progress)
	}
	if !caption {
		return
	}
	ty := y + size + CaptionGap
	u.Text(u.Fit(it.Title, text.Body, size), text.Body, x, ty, theme.Paper)
	u.Text(u.Fit(it.Subtitle, text.Meta, size), text.Meta, x, ty+u.LineHeight(text.Body)+2, theme.Ink3)
}

// CardGlow draws the focus glow of the card Card draws at (x, y) with size
// and k, scaled as the card is. Draw it first.
func CardGlow(u *ui.Context, x, y, size, k float32) {
	if k <= 0 {
		return
	}
	u.PushScale(x+size/2, y, 1+0.06*k)
	FocusGlow(u, gfx.Rect{X: x, Y: y, W: size, H: size}, CardRadius, k)
	u.Pop()
}

// progressStrip draws the 6 px progress bar along the bottom of the art,
// clipped to its rounded corners.
func progressStrip(u *ui.Context, box gfx.Rect, progress float64) {
	mask := raster.Shape{W: int(box.W), H: int(box.H), Radius: CardRadius, Kind: raster.Fill}
	strip := gfx.Rect{X: box.X, Y: box.Y + box.H - 6, W: box.W, H: 6}
	u.PushClip(strip)
	u.Shape(mask, box, theme.ArtShade)
	u.PopClip()
	strip.W = box.W * float32(progress)
	u.PushClip(strip)
	u.Shape(mask, box, u.Accent.Color)
	u.PopClip()
}
