package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"os"

	"github.com/ujjwal313/boombrick/assets"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// The in-game overlay for the heavy emulators can't draw AnberTunes' UI itself,
// so AnberTunes draws the now-playing card here, in its own look, to a small
// image the overlay just textures on screen. Pure Go, no screen needed.

const (
	cardW   = 480
	cardH   = 96
	cardPad = 16
	cardR   = 18 // corner radius
)

type overlayCard struct {
	eng  *text.Engine
	path string
	ok   bool
}

func newOverlayCard(path string) *overlayCard {
	c := &overlayCard{path: path}
	if eng, err := text.NewEngine(assets.Fonts()); err == nil {
		c.eng, c.ok = eng, true
	}
	return c
}

// update redraws the card for the current track and writes it, or removes the
// file when nothing is queued. Called once a second, so the progress moves.
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

func (c *overlayCard) render(s player.State) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, cardW, cardH))
	// Panel: AnberTunes' raised surface, a touch see-through, rounded.
	panel := theme.Raised
	panel.A = 236
	fillRoundRect(img, image.Rect(0, 0, cardW, cardH), cardR, panel)
	// Amber accent bar down the left edge.
	fillRoundRect(img, image.Rect(cardPad, cardPad, cardPad+4, cardH-cardPad), 2, theme.Gold)

	textX := cardPad + 4 + 12
	textW := cardW - textX - cardPad

	title := s.Track.Title
	if title == "" {
		title = "—"
	}
	c.drawText(img, text.Style{Face: text.FigtreeSemiBold, Size: 24}, title, textX, cardPad+2, theme.Paper, textW)
	if s.Track.Artist != "" {
		c.drawText(img, text.Style{Face: text.Figtree, Size: 17}, s.Track.Artist, textX, cardPad+36, theme.Ink3, textW)
	}

	// Progress: a thin track with an amber fill along the bottom of the card.
	py1 := cardH - cardPad
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
	fillW := int(float64(cardW-cardPad-textX) * frac)
	if fillW > 0 {
		fillRoundRect(img, image.Rect(textX, py0, textX+fillW, py1), 2, theme.Gold)
	}
	return img
}

// drawText shapes s in st (ellipsised to fit maxW), rasterises it to a white
// mask, and composites it tinted col with its top-left at (x, y).
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

// fillRoundRect fills r with col, rounding the corners by radius.
func fillRoundRect(dst *image.NRGBA, r image.Rectangle, radius int, col color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !insideRound(x, y, r, radius) {
				continue
			}
			overPixel(dst, x, y, col)
		}
	}
}

// insideRound reports whether (x,y) is inside r once the corners are rounded.
func insideRound(x, y int, r image.Rectangle, radius int) bool {
	if radius <= 0 {
		return true
	}
	cx, cy := x, y
	left, right := r.Min.X+radius, r.Max.X-1-radius
	top, bottom := r.Min.Y+radius, r.Max.Y-1-radius
	var dx, dy int
	switch {
	case cx < left:
		dx = left - cx
	case cx > right:
		dx = cx - right
	}
	switch {
	case cy < top:
		dy = top - cy
	case cy > bottom:
		dy = cy - bottom
	}
	if dx == 0 || dy == 0 {
		return true
	}
	return dx*dx+dy*dy <= radius*radius
}

// overPixel composites src over dst's pixel at (x,y) in straight-alpha NRGBA.
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
	blend := func(s, d uint8) uint8 {
		return uint8((int(s)*sa + int(d)*da*(255-sa)/255) / oa)
	}
	dst.Pix[o] = blend(src.R, dst.Pix[o])
	dst.Pix[o+1] = blend(src.G, dst.Pix[o+1])
	dst.Pix[o+2] = blend(src.B, dst.Pix[o+2])
	dst.Pix[o+3] = uint8(oa)
}

// write saves the card as the overlay's format: "ATN1", width and height as
// little-endian int32, then tight straight-alpha RGBA rows. Written whole and
// renamed over the old one.
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
