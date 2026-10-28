package screens

import (
	"fmt"
	"image/color"
	"strconv"

	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// StorageCardHeight is the storage card's height.
const StorageCardHeight = 112

var (
	cardTitle  = text.Style{Face: text.Figtree, Size: 26}
	cardLegend = text.Style{Face: text.Figtree, Size: 20}
)

// StorageCard is the SD card's space as the Downloads tab and the Storage
// page show it: what is used and free, and a bar split into music, the rest
// and free space. Its lines are made when the numbers change, not every
// frame.
type StorageCard struct {
	total, free, music int64
	summary            string
	legend             [3]string // music, other, free
}

// Set gives the card its numbers, in bytes; music is the library's files.
func (c *StorageCard) Set(total, free, music int64) {
	if c.summary != "" && total == c.total && free == c.free && music == c.music {
		return
	}
	c.total, c.free, c.music = total, free, music
	used := total - free
	c.summary = Size(used) + " used · " + Size(free) + " free of " + Size(total)
	c.legend = [3]string{"Music " + Size(music), "Other " + Size(max(used-music, 0)), "Free " + Size(free)}
}

// Summary is the card's "used · free of" line.
func (c *StorageCard) Summary() string { return c.summary }

// Draw draws the card in box, StorageCardHeight tall. Until the card has its
// numbers it shows only its title.
func (c *StorageCard) Draw(u *ui.Context, box gfx.Rect) {
	u.Shape(raster.Shape{W: int(box.W), H: StorageCardHeight, Radius: 14, Kind: raster.Ring, RingOffset: -1, RingWidth: 1}, box,
		theme.PaperA(0.14))
	x, y := box.X+20, box.Y+14
	u.Icon("sd", 26, 2, false, x, y+4, theme.Paper)
	u.Text("SD card", cardTitle, x+36, u.CenterY(cardTitle, y, 34), theme.Paper)
	if c.total <= 0 {
		return
	}
	u.Text(c.summary, text.Meta, box.X+box.W-20-u.TextWidth(c.summary, text.Meta), u.CenterY(text.Meta, y, 34), theme.Ink2)
	bar := gfx.Rect{X: x, Y: y + 34 + 8, W: box.W - 40, H: 10}
	shape := raster.Shape{W: int(bar.W), H: 10, Radius: 5, Kind: raster.Fill}
	u.Shape(shape, bar, theme.PaperA(0.12))
	music := bar.W * float32(c.music) / float32(c.total)
	other := bar.W * float32(max(c.total-c.free-c.music, 0)) / float32(c.total)
	u.PushClip(gfx.Rect{X: bar.X, Y: bar.Y, W: music, H: 10})
	u.Shape(shape, bar, u.Accent.Color)
	u.PopClip()
	u.PushClip(gfx.Rect{X: bar.X + music, Y: bar.Y, W: other, H: 10})
	u.Shape(shape, bar, theme.Ink3)
	u.PopClip()
	ly := bar.Y + 10 + 8
	lx := x
	for i, col := range [3]color.NRGBA{u.Accent.Color, theme.Ink3, theme.PaperA(0.12)} {
		u.Shape(raster.Shape{W: 12, H: 12, Radius: 3, Kind: raster.Fill}, gfx.Rect{X: lx, Y: ly + 7, W: 12, H: 12}, col)
		lx += 20 + u.Text(c.legend[i], cardLegend, lx+20, ly, theme.Ink2) + 24
	}
}

// Size is a byte count as the screens write it: "9.4 MB", "59.5 GB".
func Size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return strconv.FormatInt((n+1023)/1024, 10) + " KB"
}
