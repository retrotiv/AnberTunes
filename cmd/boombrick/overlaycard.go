package main

import (
	"encoding/binary"
	"image"
	"os"
	"sync/atomic"

	"github.com/ujjwal313/boombrick/internal/overlaycard"
	"github.com/ujjwal313/boombrick/internal/player"
)

// overlayViewMode is the view the in-game overlay asks for: 0 reduced,
// 1 expanded. The overlay sets it over the command pipe ("view=..."); the card
// is drawn in that mode.
var overlayViewMode atomic.Int32

// overlayCard renders AnberTunes' now-playing player to a file the in-game
// overlay (on the heavy emulators) textures on screen. The drawing lives in the
// overlaycard package so a desktop preview can show it too.
type overlayCard struct {
	r    *overlaycard.Renderer
	path string
	ok   bool
}

func newOverlayCard(path string) *overlayCard {
	r, err := overlaycard.New()
	if err != nil {
		return &overlayCard{path: path}
	}
	return &overlayCard{r: r, path: path, ok: true}
}

func (c *overlayCard) update(s player.State) {
	if !c.ok {
		return
	}
	if !s.Queued {
		os.Remove(c.path)
		return
	}
	m := overlaycard.Reduced
	if overlayViewMode.Load() == 1 {
		m = overlaycard.Expanded
	}
	n := overlaycard.Now{
		Title:     s.Track.Title,
		Artist:    s.Track.Artist,
		Album:     s.Track.Album,
		Format:    s.Format,
		Kbps:      s.Kbps,
		Rate:      s.Rate,
		Elapsed:   s.Elapsed,
		Duration:  s.Duration,
		Playing:   s.Playing,
		Queued:    s.Queued,
		CoverPath: s.Track.Path,
	}
	c.write(c.r.Render(n, m), m)
}

// write saves the card as the overlay's format: "ATN2", width, height and mode
// as little-endian int32, then tight straight-alpha RGBA rows; whole then
// renamed. The mode lets the overlay place a reduced card low and an expanded
// one over the whole screen.
func (c *overlayCard) write(img *image.NRGBA, m overlaycard.Mode) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	buf := make([]byte, 16, 16+w*h*4)
	copy(buf, "ATN2")
	binary.LittleEndian.PutUint32(buf[4:], uint32(w))
	binary.LittleEndian.PutUint32(buf[8:], uint32(h))
	binary.LittleEndian.PutUint32(buf[12:], uint32(m))
	for y := 0; y < h; y++ {
		o := img.PixOffset(b.Min.X, b.Min.Y+y)
		buf = append(buf, img.Pix[o:o+4*w]...)
	}
	tmp := c.path + ".tmp"
	if os.WriteFile(tmp, buf, 0o644) == nil {
		os.Rename(tmp, c.path)
	}
}
