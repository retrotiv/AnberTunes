package widgets

import (
	"math"

	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
)

var downloadIcons = [...]string{media.Available: "download", media.Queued: "clock", media.Downloading: "down",
	media.Saved: "check", media.Failed: "refresh"}

// DownloadCircle draws a song's download state in a circle size design
// pixels across with its top-left at (x, y).
// While it downloads, progress, from 0 to 1, sweeps the accent round from
// 12 o'clock, in whole percents so that the masks it needs stay few.
func DownloadCircle(u *ui.Context, st media.DownloadState, progress float32, size int, x, y float32) {
	box := gfx.Rect{X: x, Y: y, W: float32(size), H: float32(size)}
	r := float64(size) / 2
	acc := u.Accent.Color
	ring, ink := theme.PaperA(0.40), theme.Paper
	switch st {
	case media.Queued:
		ring, ink = theme.PaperA(0.25), theme.Ink3
	case media.Downloading:
		ring, ink = acc, acc
		if !(progress > 0) { // NaN too, which min and max pass through
			progress = 0
		}
		// None at 0 %, where a sector would be a hairline to 12 o'clock.
		if sweep := math.Round(float64(min(progress, 1))*100) / 100; sweep > 0 {
			u.Shape(raster.Shape{W: size, H: size, Radius: r, Kind: raster.Sector, Sweep: sweep}, box, fade(acc, 0.38))
		}
	case media.Saved:
		ring, ink = acc, theme.Ground
		u.Shape(raster.Shape{W: size, H: size, Radius: r, Kind: raster.Fill}, box, acc)
	case media.Failed:
		ring, ink = theme.GoldSoft, theme.GoldSoft
	}
	u.Shape(raster.Shape{W: size, H: size, Radius: r, Kind: raster.Ring, RingOffset: -2, RingWidth: 2}, box, ring)
	icon := int(math.Round(float64(size) * 0.46))
	off := float32(size-icon) / 2
	u.Icon(downloadIcons[st], icon, 2, false, x+off, y+off, ink)
}
