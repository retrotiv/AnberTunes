package main

import (
	"database/sql"
	"image"
	"sync"

	"github.com/ujjwal313/boombrick/internal/art"
	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/overlaycard"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/screens"
)

// loadOverlayLibrary loads the library index for the in-game browser, or nil if
// it can't (then browsing just does nothing).
func loadOverlayLibrary(db *sql.DB) *library.Index {
	ix, err := library.Load(db)
	if err != nil {
		return nil
	}
	return ix
}

// overlayNav drives the in-game player: it holds the library, which view is
// showing (now-playing or the library browser) and the selection, and turns the
// overlay's button commands into moves and playback. It builds the frame the
// card is drawn from.
type overlayNav struct {
	mu       sync.Mutex
	p        *player.Player
	ix       *library.Index
	browsing bool
	tab      int // 0 Albums, 1 Songs
	sel      [2]int
	inAlbum  *library.Album
	albumSel int
	covers   map[string]image.Image
}

func newOverlayNav(p *player.Player, ix *library.Index) *overlayNav {
	return &overlayNav{p: p, ix: ix, covers: map[string]image.Image{}}
}

const browseTabs = 2 // Albums, Songs

// Handle turns one command from the overlay into a move or playback.
func (n *overlayNav) Handle(cmd string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.browsing {
		switch cmd {
		case "y":
			if n.ix != nil {
				n.browsing = true
			}
		case "a", "toggle":
			n.p.Toggle()
		case "left", "prev":
			n.p.Prev()
		case "right", "next":
			n.p.Next()
		}
		return
	}
	if n.inAlbum != nil { // inside an album: its tracks
		switch cmd {
		case "up":
			n.albumSel = clamp(n.albumSel-1, 0, len(n.inAlbum.Tracks)-1)
		case "down":
			n.albumSel = clamp(n.albumSel+1, 0, len(n.inAlbum.Tracks)-1)
		case "a":
			n.p.Play(screens.Tracks(n.inAlbum.Tracks), n.albumSel)
			n.browsing, n.inAlbum = false, nil
		case "b":
			n.inAlbum = nil
		}
		return
	}
	switch cmd { // the library list
	case "up":
		n.sel[n.tab] = clamp(n.sel[n.tab]-1, 0, n.listLen()-1)
	case "down":
		n.sel[n.tab] = clamp(n.sel[n.tab]+1, 0, n.listLen()-1)
	case "left", "l2":
		n.tab = clamp(n.tab-1, 0, browseTabs-1)
	case "right", "r2":
		n.tab = clamp(n.tab+1, 0, browseTabs-1)
	case "a":
		n.open()
	case "b", "y":
		n.browsing = false
	}
}

func (n *overlayNav) listLen() int {
	if n.tab == 0 {
		return len(n.ix.Albums)
	}
	return len(n.ix.Songs)
}

// open enters the selected album, or plays the selected song.
func (n *overlayNav) open() {
	i := n.sel[n.tab]
	if n.tab == 0 {
		if i >= 0 && i < len(n.ix.Albums) {
			n.inAlbum, n.albumSel = n.ix.Albums[i], 0
		}
		return
	}
	if i >= 0 && i < len(n.ix.Songs) {
		tracks := make([]player.Track, len(n.ix.Songs))
		for j, s := range n.ix.Songs {
			tracks[j] = screens.TrackOf(s)
		}
		n.p.Play(tracks, i)
		n.browsing = false
	}
}

// Frame builds what the card draws now: the browser when browsing in the
// expanded view, otherwise now-playing.
func (n *overlayNav) Frame() (overlaycard.Now, overlaycard.BrowseModel, bool, overlaycard.Mode) {
	n.mu.Lock()
	defer n.mu.Unlock()
	mode := overlaycard.Reduced
	if overlayViewMode.Load() == 1 {
		mode = overlaycard.Expanded
	}
	s := n.p.State()
	now := overlaycard.Now{
		Title: s.Track.Title, Artist: s.Track.Artist, Album: s.Track.Album,
		Format: s.Format, Kbps: s.Kbps, Rate: s.Rate,
		Elapsed: s.Elapsed, Duration: s.Duration, Playing: s.Playing, Queued: s.Queued,
		CoverPath: s.Track.Path,
	}
	if !n.browsing || mode != overlaycard.Expanded || n.ix == nil {
		return now, overlaycard.BrowseModel{}, false, mode
	}
	return now, n.browseModel(), true, mode
}

// browseModel is the list to draw: the album's tracks, or the albums or songs,
// with covers loaded for the rows on screen.
func (n *overlayNav) browseModel() overlaycard.BrowseModel {
	if n.inAlbum != nil {
		rows := make([]overlaycard.BrowseRow, len(n.inAlbum.Tracks))
		for i, t := range n.inAlbum.Tracks {
			rows[i] = overlaycard.BrowseRow{Title: t.Title, Subtitle: t.Artist}
		}
		n.loadCovers(rows, n.albumSel, func(i int) string { return n.inAlbum.Tracks[i].Path })
		return overlaycard.BrowseModel{Heading: n.inAlbum.Title, Tabs: nil, Rows: rows, Selected: n.albumSel}
	}
	var rows []overlaycard.BrowseRow
	var path func(i int) string
	if n.tab == 0 {
		rows = make([]overlaycard.BrowseRow, len(n.ix.Albums))
		for i, a := range n.ix.Albums {
			rows[i] = overlaycard.BrowseRow{Title: a.Title, Subtitle: a.Artist}
		}
		path = func(i int) string {
			if len(n.ix.Albums[i].Tracks) > 0 {
				return n.ix.Albums[i].Tracks[0].Path
			}
			return ""
		}
	} else {
		rows = make([]overlaycard.BrowseRow, len(n.ix.Songs))
		for i, s := range n.ix.Songs {
			rows[i] = overlaycard.BrowseRow{Title: s.Title, Subtitle: s.Artist}
		}
		path = func(i int) string { return n.ix.Songs[i].Path }
	}
	n.loadCovers(rows, n.sel[n.tab], path)
	return overlaycard.BrowseModel{Heading: "Biblioteca", Tabs: []string{"Álbuns", "Músicas"}, Tab: n.tab, Rows: rows, Selected: n.sel[n.tab]}
}

// loadCovers reads covers only for the rows around sel (the ones on screen),
// cached by path, so a long list stays cheap.
func (n *overlayNav) loadCovers(rows []overlaycard.BrowseRow, sel int, path func(i int) string) {
	for i := sel - 3; i <= sel+3; i++ {
		if i < 0 || i >= len(rows) {
			continue
		}
		p := path(i)
		if p == "" {
			continue
		}
		img, ok := n.covers[p]
		if !ok {
			if data, has := art.Embedded(p); has {
				if im, err := art.Decode(data); err == nil {
					img = im
				}
			}
			n.covers[p] = img // nil cached too, so it is tried once
		}
		rows[i].Cover = img
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
