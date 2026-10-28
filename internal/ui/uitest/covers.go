package uitest

import (
	"hash/fnv"
	"image"

	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
)

// Covers is album art for screenshot tests: every album gets a two-tone
// picture coloured from its name, ready at once.
type Covers struct{}

// Cover returns c's picture px pixels square.
func (Covers) Cover(c media.Cover, px int) (*image.NRGBA, bool) { return picture(c.Key(), px), false }

// Backdrop returns c's picture at the backdrop's size.
func (Covers) Backdrop(c media.Cover) (*image.NRGBA, bool) { return picture(c.Key(), 64), false }

func picture(key string, px int) *image.NRGBA {
	h := fnv.New32a()
	h.Write([]byte(key))
	hue := float64(h.Sum32() % 360)
	light := theme.OKLCH{L: 0.62, C: 0.13, H: hue}.NRGBA(1)
	dark := theme.OKLCH{L: 0.35, C: 0.08, H: hue + 40}.NRGBA(1)
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	for y := range px {
		for x := range px {
			c := light
			if x+y > px {
				c = dark
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// WithCovers draws u's art from Covers, uploading as many as a frame needs,
// so a screen shows all its covers on the frame after the one that asks.
func WithCovers(u *ui.Context) {
	u.Covers = Covers{}
	u.MaxUploads = 1000
}
