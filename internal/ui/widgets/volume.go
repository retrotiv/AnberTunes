package widgets

import (
	"strconv"

	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

var volumeText = text.Style{Face: text.Figtree, Size: 24, Tabular: true}

const (
	volumeBar  = 220 // the bar's width
	volumeMax  = 20  // NextUI's top level
	volumeGap  = 14
	volumeIcon = 24
)

// VolumePill draws a volume level, 0 to 20, as a toast does: the speaker, a bar
// filled to the level and the level in figures. alpha fades it.
func VolumePill(u *ui.Context, level int, alpha float32) {
	level = min(max(level, 0), volumeMax)
	num := strconv.Itoa(level)
	// The figures are tabular, so the pill keeps its width from 0 to 20 but for
	// the second digit; measure the widest so it doesn't jump.
	numW := u.TextWidth("20", volumeText)
	h := u.LineHeight(volumeText)
	box := toastBox(u, volumeIcon+volumeGap+volumeBar+volumeGap+numW, h, alpha)
	x, y := box.X+22, box.Y+14
	u.Icon("volume", volumeIcon, 2, false, x, y+(h-volumeIcon)/2, fade(theme.Paper, alpha))
	x += volumeIcon + volumeGap
	by := y + (h-6)/2
	u.Shape(raster.Shape{W: volumeBar, H: 6, Radius: 3, Kind: raster.Fill}, gfx.Rect{X: x, Y: by, W: volumeBar, H: 6}, fade(theme.Paper, 0.18*alpha))
	if fw := volumeBar * level / volumeMax; fw > 0 {
		u.Shape(raster.Shape{W: fw, H: 6, Radius: float64(min(3, fw/2)), Kind: raster.Fill}, gfx.Rect{X: x, Y: by, W: float32(fw), H: 6}, fade(theme.Paper, alpha))
	}
	x += volumeBar + volumeGap
	u.Text(num, volumeText, x+numW-u.TextWidth(num, volumeText), y, fade(theme.Paper, alpha))
}
