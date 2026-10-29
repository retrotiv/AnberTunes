package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
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

// watchCommands lets the in-game music menu drive the background player: it
// reads lines from a FIFO and runs toggle, next or prev. The FIFO is opened
// read-write so it never blocks on open and never sees end-of-file when a
// writer closes, so one reader serves every press. If the FIFO can't be made,
// the menu just won't control the music; the button hotkeys still work.
func watchCommands(ctx context.Context, nav *overlayNav, path string) {
	os.Remove(path)
	if err := syscall.Mkfifo(path, 0o666); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		os.Remove(path)
		return
	}
	go func() {
		<-ctx.Done()
		f.Close()
		os.Remove(path)
	}()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		switch line := strings.TrimSpace(sc.Text()); line {
		case "view=hidden":
			overlayViewMode.Store(0)
		case "view=reduced":
			overlayViewMode.Store(1)
		case "view=expanded":
			overlayViewMode.Store(2)
		default:
			nav.Handle(line) // toggle/next/prev and the player's buttons
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
