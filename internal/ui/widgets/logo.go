package widgets

import (
	"fmt"
	"image"
	"image/color"

	"github.com/ujjwal313/boombrick/assets"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
)

// logoKey is the image key of the mark last drawn, and its size in device
// pixels, so that a frame drawing it again formats nothing. Only the UI
// thread draws.
var logoKey struct {
	w, h int
	key  string
}

// LogoMark draws the boombox mark at em px per em: 1.5 em wide and 1.27 em
// tall including the handle.
func LogoMark(u *ui.Context, em, x, y float32, col color.NRGBA) {
	w, h := 1.5*em, 1.27*em
	pw, ph := u.Px(w), u.Px(h)
	if logoKey.key == "" || logoKey.w != pw || logoKey.h != ph {
		logoKey.w, logoKey.h, logoKey.key = pw, ph, fmt.Sprintf("logo:%dx%d", pw, ph)
	}
	u.Image(logoKey.key, func() *image.NRGBA {
		img, err := raster.SVG(assets.Logo(), pw, ph)
		if err != nil {
			return nil
		}
		return img
	}, gfx.Rect{X: x, Y: y, W: float32(pw) / u.Scale, H: float32(ph) / u.Scale}, col)
}
