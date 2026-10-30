package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ujjwal313/boombrick/internal/player"
)

// publishNowPlaying writes what the background player is playing to a small
// file once a second, so a menu with no link to the player (the in-game music
// menu in NextUI's minarch) can show the track and whether it is playing. The
// file is written whole and renamed over the old one, so a reader never catches
// it half-written. It is removed when the player stops.
func publishNowPlaying(ctx context.Context, p *player.Player, nav *overlayNav, card *overlayCard, path string) {
	// A short tick keeps the overlay snappy; the frame signature means an
	// unchanged view costs only the check, not a redraw.
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	lastSig := ""
	write := func() {
		s := p.State()
		play := 0
		if s.Playing {
			play = 1
		}
		queued := 0
		if s.Queued {
			queued = 1
		}
		body := fmt.Sprintf("playing=%d\nqueued=%d\ntitle=%s\nartist=%s\nelapsed=%d\nduration=%d\n",
			play, queued, oneLine(s.Track.Title), oneLine(s.Track.Artist),
			int(s.Elapsed/time.Second), int(s.Duration/time.Second))
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(body), 0o644); err == nil {
			os.Rename(tmp, path)
		}
		// The overlay card is drawn only while the overlay is on screen, and
		// only when something it shows has changed, to spare the device.
		if overlayViewMode.Load() == 0 { // hidden
			card.clear()
			lastSig = ""
			return
		}
		now, bm, browse, mode := nav.Frame()
		sig := frameSig(now, bm, browse, mode)
		if sig == lastSig {
			return
		}
		lastSig = sig
		card.render(now, bm, browse, mode)
	}
	write()
	for {
		select {
		case <-ctx.Done():
			os.Remove(path)
			os.Remove(card.path)
			return
		case <-t.C:
			write()
		}
	}
}

// watchCommands lets the in-game overlay drive the background player: the
// overlay appends lines to a plain file (view= and the player's buttons) and
// this polls it for new ones. It is a plain file, not a FIFO, because it lives
// on the card's shared storage (the only place the sandboxed heavy emulators
// and the player can both reach), and a FIFO can't be made there. Reading
// starts at the end, so stale commands from a past session are skipped, and the
// file is trimmed when it grows, so it can't fill the card.
func watchCommands(ctx context.Context, nav *overlayNav, path string) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	var off int64
	if fi, err := os.Stat(path); err == nil {
		off = fi.Size() // skip whatever a past session left
	}
	var pending string // a half-written last line, finished on the next read
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		size := fi.Size()
		if size < off { // trimmed or replaced: start over
			off, pending = 0, ""
		}
		if size > off {
			if _, err := f.Seek(off, io.SeekStart); err == nil {
				b, _ := io.ReadAll(f)
				off = size
				pending += string(b)
				for {
					i := strings.IndexByte(pending, '\n')
					if i < 0 {
						break
					}
					line := strings.TrimSpace(pending[:i])
					pending = pending[i+1:]
					switch line {
					case "":
					case "view=hidden":
						overlayViewMode.Store(0)
					case "view=reduced":
						overlayViewMode.Store(1)
					case "view=expanded":
						overlayViewMode.Store(2)
					default:
						nav.Handle(line) // the player's buttons
					}
				}
			}
		}
		f.Close()
		if off > 64<<10 && pending == "" { // keep it from filling the card
			if os.Truncate(path, 0) == nil {
				off = 0
			}
		}
	}
}

// oneLine keeps a title or artist to a single line, so it can't break the
// key=value file the menu parses.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
}
