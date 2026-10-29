package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"

	"github.com/ujjwal313/boombrick/assets"
	"github.com/ujjwal313/boombrick/internal/art"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// The in-game overlay for the heavy emulators can't draw AnberTunes' UI itself,
// so AnberTunes draws the now-playing player here, in its own look (cover,
// title, artist, progress and the button hints), to a small image the overlay
// textures on screen. Pure Go, no screen needed.

const (
	cardW   = 560
	cardH   = 150
	cardPad = 18
	cardR   = 20
	coverPx = cardH - 2*cardPad // square cover on the left
)

type overlayCard struct {
	eng       *text.Engine
	path      string
	ok        bool
	coverKey  string       // the track whose cover is cached
	coverImg  *image.NRGBA // squared cover, or nil
}

func newOverlayCard(path string) *overlayCard {
	c := &overlayCard{path: path}
	if eng, err := text.NewEngine(assets.Fonts()); err == nil {
		c.eng, c.ok = eng, true
	}
	return c
}

func (c *overlayCard) update(s player.State) {
	if !c.ok {
		return
	}
	if !s.Queued {
		os.Remove(c.path)
		return
	}
	c.write(c.render(s))
}

// cover returns the squared cover for the track, cached by its path. It reads
// the picture embedded in the file, or a folder image beside it; no network.
func (c *overlayCard) cover(t player.Track, px int) *image.NRGBA {
	if t.Path == c.coverKey {
		return c.coverImg
	}
	c.coverKey, c.coverImg = t.Path, nil
	if t.Path == "" {
		return nil
	}
	if data, ok := art.Embedded(t.Path); ok {
		if im, err := art.Decode(data); err == nil {
			c.coverImg = art.Square(im, px)
		}
	}
	if c.coverImg == nil {
		if p, ok := art.FolderImage(filepath.Dir(t.Path)); ok {
			if data, err := os.ReadFile(p); err == nil {
				if im, err := art.Decode(data); err == nil {
					c.coverImg = art.Square(im, px)
				}
			}
		}
	}
	return c.coverImg
}

func (c *overlayCard) render(s player.State) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, cardW, cardH))
	panel := theme.Raised
	panel.A = 240
	fillRoundRect(img, image.Rect(0, 0, cardW, cardH), cardR, panel)

	// Cover on the left (rounded); a plain tile when there is none.
	cov := image.Rect(cardPad, cardPad, cardPad+coverPx, cardPad+coverPx)
	if cv := c.cover(s.Track, coverPx); cv != nil {
		compositeImage(img, cv, cov.Min, 10)
	} else {
		fillRoundRect(img, cov, 10, theme.LiftedRow)
		fillRoundRect(img, image.Rect(cov.Min.X+coverPx/2-3, cardPad+coverPx/2-10, cov.Min.X+coverPx/2+3, cardPad+coverPx/2+10), 2, theme.Gold)
	}

	textX := cardPad + coverPx + 18
	textW := cardW - textX - cardPad

	title := s.Track.Title
	if title == "" {
		title = "—"
	}
	c.drawText(img, text.Style{Face: text.FigtreeSemiBold, Size: 26}, title, textX, cardPad+2, theme.Paper, textW)
	if s.Track.Artist != "" {
		c.drawText(img, text.Style{Face: text.Figtree, Size: 18}, s.Track.Artist, textX, cardPad+40, theme.Ink3, textW)
	}

	// Progress track and amber fill.
	py1 := cardPad + 74
	py0 := py1 - 5
	fillRoundRect(img, image.Rect(textX, py0, cardW-cardPad, py1), 2, theme.PaperA(0.18))
	frac := 0.0
	if d := s.Duration.Seconds(); d > 0 {
		frac = s.Elapsed.Seconds() / d
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
	}
	if fw := int(float64(cardW-cardPad-textX) * frac); fw > 0 {
		fillRoundRect(img, image.Rect(textX, py0, textX+fw, py1), 2, theme.Gold)
	}

	// Button hints: A plays or pauses, Y is next, B is previous.
	hy := cardPad + 90
	x := textX
	playLabel := "Tocar"
	if s.Playing {
		playLabel = "Pausar"
	}
	x = c.drawHint(img, x, hy, "A", playLabel)
	x = c.drawHint(img, x+18, hy, "Y", "Próxima")
	c.drawHint(img, x+18, hy, "B", "Anterior")

	return img
}

// drawHint draws a gold button badge with the letter, then its label, and
// returns the x just past the label.
func (c *overlayCard) drawHint(dst *image.NRGBA, x, y int, letter, label string) int {
	const d = 24 // badge diameter
	fillRoundRect(dst, image.Rect(x, y, x+d, y+d), d/2, theme.Gold)
	ls := text.Style{Face: text.FigtreeSemiBold, Size: 16}
	lw := c.textWidth(ls, letter)
	c.drawText(dst, ls, letter, x+(d-lw)/2, y+3, theme.Ground, d)
	labelX := x + d + 7
	st := text.Style{Face: text.Figtree, Size: 16}
	c.drawText(dst, st, label, labelX, y+4, theme.Ink2, cardW)
	return labelX + c.textWidth(st, label)
}

func (c *overlayCard) textWidth(st text.Style, s string) int { return c.eng.Shape(s, st).Width }

// drawText shapes s (ellipsised to maxW), rasterises it to a white mask, and
// composites it tinted col with its top-left at (x, y).
func (c *overlayCard) drawText(dst *image.NRGBA, st text.Style, s string, x, y int, col color.NRGBA, maxW int) {
	s = c.eng.Fit(s, st, maxW)
	line := c.eng.Shape(s, st)
	mask, _ := c.eng.Rasterize(line, st)
	b := mask.Bounds()
	for my := b.Min.Y; my < b.Max.Y; my++ {
		for mx := b.Min.X; mx < b.Max.X; mx++ {
			a := mask.Pix[mask.PixOffset(mx, my)+3]
			if a == 0 {
				continue
			}
			px, py := x+(mx-b.Min.X), y+(my-b.Min.Y)
			if px < 0 || py < 0 || px >= dst.Rect.Dx() || py >= dst.Rect.Dy() {
				continue
			}
			src := col
			src.A = uint8(uint16(col.A) * uint16(a) / 255)
			overPixel(dst, px, py, src)
		}
	}
}

// compositeImage draws src onto dst with its top-left at p, rounding corners.
func compositeImage(dst, src *image.NRGBA, p image.Point, radius int) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	r := image.Rect(p.X, p.Y, p.X+w, p.Y+h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !insideRound(p.X+x, p.Y+y, r, radius) {
				continue
			}
			o := src.PixOffset(b.Min.X+x, b.Min.Y+y)
			overPixel(dst, p.X+x, p.Y+y, color.NRGBA{src.Pix[o], src.Pix[o+1], src.Pix[o+2], src.Pix[o+3]})
		}
	}
}

func fillRoundRect(dst *image.NRGBA, r image.Rectangle, radius int, col color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if insideRound(x, y, r, radius) {
				overPixel(dst, x, y, col)
			}
		}
	}
}

func insideRound(x, y int, r image.Rectangle, radius int) bool {
	if radius <= 0 {
		return true
	}
	left, right := r.Min.X+radius, r.Max.X-1-radius
	top, bottom := r.Min.Y+radius, r.Max.Y-1-radius
	var dx, dy int
	switch {
	case x < left:
		dx = left - x
	case x > right:
		dx = x - right
	}
	switch {
	case y < top:
		dy = top - y
	case y > bottom:
		dy = y - bottom
	}
	if dx == 0 || dy == 0 {
		return true
	}
	return dx*dx+dy*dy <= radius*radius
}

func overPixel(dst *image.NRGBA, x, y int, src color.NRGBA) {
	o := dst.PixOffset(x, y)
	sa := int(src.A)
	if sa == 0 {
		return
	}
	if sa == 255 {
		dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = src.R, src.G, src.B, 255
		return
	}
	da := int(dst.Pix[o+3])
	oa := sa + da*(255-sa)/255
	if oa == 0 {
		dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = 0, 0, 0, 0
		return
	}
	blend := func(s, d uint8) uint8 { return uint8((int(s)*sa + int(d)*da*(255-sa)/255) / oa) }
	dst.Pix[o] = blend(src.R, dst.Pix[o])
	dst.Pix[o+1] = blend(src.G, dst.Pix[o+1])
	dst.Pix[o+2] = blend(src.B, dst.Pix[o+2])
	dst.Pix[o+3] = uint8(oa)
}

// write saves the card as the overlay's format: "ATN1", width and height as
// little-endian int32, then tight straight-alpha RGBA rows, whole then renamed.
func (c *overlayCard) write(img *image.NRGBA) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	buf := make([]byte, 12, 12+w*h*4)
	copy(buf, "ATN1")
	binary.LittleEndian.PutUint32(buf[4:], uint32(w))
	binary.LittleEndian.PutUint32(buf[8:], uint32(h))
	for y := 0; y < h; y++ {
		o := img.PixOffset(b.Min.X, b.Min.Y+y)
		buf = append(buf, img.Pix[o:o+4*w]...)
	}
	tmp := c.path + ".tmp"
	if os.WriteFile(tmp, buf, 0o644) == nil {
		os.Rename(tmp, c.path)
	}
}
