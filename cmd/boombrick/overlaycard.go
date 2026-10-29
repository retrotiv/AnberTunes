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

	// Transport buttons, like a real player: previous, play/pause, next, each
	// tagged with the gamepad button that works it (B, A, Y).
	const d, gap = 34, 26
	rowW := 3*d + 2*gap
	bx := textX + (textW-rowW)/2
	if bx < textX {
		bx = textX
	}
	by := cardPad + 80
	c.drawBtn(img, bx, by, d, "prev", "B", false)
	c.drawBtn(img, bx+d+gap, by, d, "play", "A", s.Playing)
	c.drawBtn(img, bx+2*(d+gap), by, d, "next", "Y", false)

	return img
}

// drawBtn draws a transport button: a round key with a play/pause/next/prev
// icon, and the gamepad letter under it. The play key is filled amber (the
// primary action); the others are raised tiles with an amber icon.
func (c *overlayCard) drawBtn(dst *image.NRGBA, x, y, d int, kind, letter string, playing bool) {
	cx, cy := x+d/2, y+d/2
	icon := theme.Gold
	if kind == "play" {
		fillRoundRect(dst, image.Rect(x, y, x+d, y+d), d/2, theme.Gold)
		icon = theme.Ground
	} else {
		fillRoundRect(dst, image.Rect(x, y, x+d, y+d), d/2, theme.LiftedRow)
	}
	t := d / 6
	bw := d/12 + 1
	switch kind {
	case "play":
		if playing { // two bars
			fillRoundRect(dst, image.Rect(cx-t, cy-t, cx-t+bw, cy+t), 1, icon)
			fillRoundRect(dst, image.Rect(cx+t-bw, cy-t, cx+t, cy+t), 1, icon)
		} else { // right triangle
			fillTri(dst, cx-t+1, cy-t, cx-t+1, cy+t, cx+t, cy, icon)
		}
	case "prev":
		fillRoundRect(dst, image.Rect(cx-t-bw, cy-t, cx-t, cy+t), 1, icon)
		fillTri(dst, cx+t, cy-t, cx+t, cy+t, cx-t+1, cy, icon)
	case "next":
		fillTri(dst, cx-t, cy-t, cx-t, cy+t, cx+t-1, cy, icon)
		fillRoundRect(dst, image.Rect(cx+t, cy-t, cx+t+bw, cy+t), 1, icon)
	}
	ls := text.Style{Face: text.Figtree, Size: 13}
	lw := c.textWidth(ls, letter)
	c.drawText(dst, ls, letter, cx-lw/2, y+d+2, theme.Ink4, cardW)
}

// fillTri fills the triangle (x0,y0)(x1,y1)(x2,y2) with col.
func fillTri(dst *image.NRGBA, x0, y0, x1, y1, x2, y2 int, col color.NRGBA) {
	minY, maxY := min(y0, y1, y2), max(y0, y1, y2)
	minX, maxX := min(x0, x1, x2), max(x0, x1, x2)
	area := (x1-x0)*(y2-y0) - (x2-x0)*(y1-y0)
	if area == 0 {
		return
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			w0 := (x1-x)*(y2-y) - (x2-x)*(y1-y)
			w1 := (x2-x)*(y0-y) - (x0-x)*(y2-y)
			w2 := (x0-x)*(y1-y) - (x1-x)*(y0-y)
			if (w0 >= 0 && w1 >= 0 && w2 >= 0) || (w0 <= 0 && w1 <= 0 && w2 <= 0) {
				if x >= 0 && y >= 0 && x < dst.Rect.Dx() && y < dst.Rect.Dy() {
					overPixel(dst, x, y, col)
				}
			}
		}
	}
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
