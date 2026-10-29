package main

import (
	"database/sql"
	"fmt"
	"image"
	"sync"
	"time"

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

// frameSig is a cheap fingerprint of what the card would draw, so an unchanged
// frame isn't drawn again every tick.
func frameSig(now overlaycard.Now, bm overlaycard.BrowseModel, browse bool, mode overlaycard.Mode) string {
	if browse {
		return fmt.Sprintf("b|%d|%d|%s|%d", bm.Tab, bm.Selected, bm.Heading, len(bm.Rows))
	}
	return fmt.Sprintf("n|%d|%s|%t|%d|%d|%t|%d", mode, now.Title, now.Playing, int(now.Elapsed/time.Second), now.Focus, now.Shuffle, now.Repeat)
}

// overlayNav drives the in-game player: the library, which view shows and the
// selection, turning the overlay's button commands into moves and playback.
type overlayNav struct {
	mu       sync.Mutex
	p        *player.Player
	ix       *library.Index
	browsing bool
	tab      int    // 0 Albums, 1 Artists, 2 Songs, 3 Genres
	sel      [4]int // selection per tab
	npFocus  int    // focused now-playing control: 0 shuffle..2 play..4 repeat

	// A drilled-in album, artist or genre: its songs, ready to play.
	inDrill     bool
	drillTitle  string
	drillTracks []player.Track
	drillPaths  []string
	drillSel    int

	covers map[string]*image.NRGBA // squared to overlaycard.BrowseThumb, by path
}

const browseTabs = 4

var tabNames = []string{"Álbuns", "Artistas", "Músicas", "Gêneros"}

func newOverlayNav(p *player.Player, ix *library.Index) *overlayNav {
	return &overlayNav{p: p, ix: ix, npFocus: 2, covers: map[string]*image.NRGBA{}}
}

// Handle turns one command from the overlay into a move or playback.
func (n *overlayNav) Handle(cmd string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.browsing { // now-playing: focus moves across the 5 controls, A activates
		switch cmd {
		case "y":
			if n.ix != nil {
				n.browsing = true
			}
		case "left":
			n.npFocus = clamp(n.npFocus-1, 0, 4)
		case "right":
			n.npFocus = clamp(n.npFocus+1, 0, 4)
		case "a":
			n.activateNP()
		case "toggle": // from the reduced view / the minarch music menu
			n.p.Toggle()
		case "prev":
			n.p.Prev()
		case "next":
			n.p.Next()
		}
		return
	}
	if n.inDrill { // inside an album/artist/genre: its songs
		switch cmd {
		case "up":
			n.drillSel = clamp(n.drillSel-1, 0, len(n.drillTracks)-1)
		case "down":
			n.drillSel = clamp(n.drillSel+1, 0, len(n.drillTracks)-1)
		case "a":
			n.p.Play(n.drillTracks, n.drillSel)
			n.browsing, n.inDrill = false, false
		case "b":
			n.inDrill = false
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
	switch n.tab {
	case 0:
		return len(n.ix.Albums)
	case 1:
		return len(n.ix.Artists)
	case 2:
		return len(n.ix.Songs)
	default:
		return len(n.ix.Genres)
	}
}

// open enters the selected album/artist/genre, or plays the selected song.
func (n *overlayNav) open() {
	i := n.sel[n.tab]
	switch n.tab {
	case 0:
		if i >= 0 && i < len(n.ix.Albums) {
			a := n.ix.Albums[i]
			n.enter(a.Title, screens.Tracks(a.Tracks), libPaths(a.Tracks))
		}
	case 1:
		if i >= 0 && i < len(n.ix.Artists) {
			a := n.ix.Artists[i]
			tracks, paths := artistSongs(a)
			n.enter(a.Name, tracks, paths)
		}
	case 2:
		if i >= 0 && i < len(n.ix.Songs) {
			n.p.Play(ptrSongs(n.ix.Songs), i)
			n.browsing = false
		}
	case 3:
		if i >= 0 && i < len(n.ix.Genres) {
			g := n.ix.Genres[i]
			n.enter(g.Name, ptrSongs(g.Songs), ptrPaths(g.Songs))
		}
	}
}

func (n *overlayNav) enter(title string, tracks []player.Track, paths []string) {
	n.inDrill, n.drillTitle, n.drillTracks, n.drillPaths, n.drillSel = true, title, tracks, paths, 0
}

// activateNP works the focused now-playing control.
func (n *overlayNav) activateNP() {
	switch n.npFocus {
	case 0:
		n.p.SetShuffle(!n.p.State().Shuffle)
	case 1:
		n.p.Prev()
	case 2:
		n.p.Toggle()
	case 3:
		n.p.Next()
	case 4:
		n.p.CycleRepeat()
	}
}

// Frame builds what the card draws now: the browser when browsing in the
// expanded view, otherwise now-playing.
func (n *overlayNav) Frame() (overlaycard.Now, overlaycard.BrowseModel, bool, overlaycard.Mode) {
	n.mu.Lock()
	defer n.mu.Unlock()
	mode := overlaycard.Reduced
	if overlayViewMode.Load() == 2 { // 0 hidden, 1 reduced, 2 expanded
		mode = overlaycard.Expanded
	}
	s := n.p.State()
	now := overlaycard.Now{
		Title: s.Track.Title, Artist: s.Track.Artist, Album: s.Track.Album,
		Format: s.Format, Kbps: s.Kbps, Rate: s.Rate,
		Elapsed: s.Elapsed, Duration: s.Duration, Playing: s.Playing, Queued: s.Queued,
		CoverPath: s.Track.Path,
		Focus:     n.npFocus, Shuffle: s.Shuffle, Repeat: int(s.Repeat),
	}
	if !n.browsing || mode != overlaycard.Expanded || n.ix == nil {
		return now, overlaycard.BrowseModel{}, false, mode
	}
	return now, n.browseModel(), true, mode
}

func (n *overlayNav) browseModel() overlaycard.BrowseModel {
	if n.inDrill {
		rows := make([]overlaycard.BrowseRow, len(n.drillTracks))
		for i, t := range n.drillTracks {
			rows[i] = overlaycard.BrowseRow{Title: t.Title, Subtitle: t.Artist}
		}
		n.loadCovers(rows, n.drillSel, func(i int) string { return n.drillPaths[i] })
		return overlaycard.BrowseModel{Heading: n.drillTitle, Rows: rows, Selected: n.drillSel}
	}
	var rows []overlaycard.BrowseRow
	var path func(i int) string
	switch n.tab {
	case 0:
		rows = make([]overlaycard.BrowseRow, len(n.ix.Albums))
		for i, a := range n.ix.Albums {
			rows[i] = overlaycard.BrowseRow{Title: a.Title, Subtitle: a.Artist}
		}
		path = func(i int) string { return firstPath(n.ix.Albums[i].Tracks) }
	case 1:
		rows = make([]overlaycard.BrowseRow, len(n.ix.Artists))
		for i, a := range n.ix.Artists {
			rows[i] = overlaycard.BrowseRow{Title: a.Name, Subtitle: songsLabel(a.Songs)}
		}
		path = func(i int) string { return artistFirstPath(n.ix.Artists[i]) }
	case 2:
		rows = make([]overlaycard.BrowseRow, len(n.ix.Songs))
		for i, s := range n.ix.Songs {
			rows[i] = overlaycard.BrowseRow{Title: s.Title, Subtitle: s.Artist}
		}
		path = func(i int) string { return n.ix.Songs[i].Path }
	default:
		rows = make([]overlaycard.BrowseRow, len(n.ix.Genres))
		for i, g := range n.ix.Genres {
			rows[i] = overlaycard.BrowseRow{Title: g.Name, Subtitle: songsLabel(len(g.Songs))}
		}
		path = func(i int) string {
			if len(n.ix.Genres[i].Songs) > 0 {
				return n.ix.Genres[i].Songs[0].Path
			}
			return ""
		}
	}
	n.loadCovers(rows, n.sel[n.tab], path)
	return overlaycard.BrowseModel{Heading: "Biblioteca", Tabs: tabNames, Tab: n.tab, Rows: rows, Selected: n.sel[n.tab]}
}

// loadCovers reads covers only for the rows around sel (the ones on screen),
// squared and cached by path, so a long list stays cheap.
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
					img = art.Square(im, overlaycard.BrowseThumb)
				}
			}
			n.covers[p] = img // nil cached too, so it is tried once
		}
		rows[i].Cover = img
	}
}

// ---- helpers ----------------------------------------------------------------

func artistSongs(a *library.Artist) ([]player.Track, []string) {
	var tracks []player.Track
	var paths []string
	for _, al := range a.Albums {
		for i := range al.Tracks {
			tracks = append(tracks, screens.TrackOf(&al.Tracks[i]))
			paths = append(paths, al.Tracks[i].Path)
		}
	}
	return tracks, paths
}

func ptrSongs(songs []*library.Track) []player.Track {
	out := make([]player.Track, len(songs))
	for i, s := range songs {
		out[i] = screens.TrackOf(s)
	}
	return out
}

func libPaths(ts []library.Track) []string {
	out := make([]string, len(ts))
	for i := range ts {
		out[i] = ts[i].Path
	}
	return out
}

func ptrPaths(songs []*library.Track) []string {
	out := make([]string, len(songs))
	for i, s := range songs {
		out[i] = s.Path
	}
	return out
}

func firstPath(ts []library.Track) string {
	if len(ts) > 0 {
		return ts[0].Path
	}
	return ""
}

func artistFirstPath(a *library.Artist) string {
	if len(a.Albums) > 0 {
		return firstPath(a.Albums[0].Tracks)
	}
	return ""
}

func songsLabel(n int) string {
	if n == 1 {
		return "1 música"
	}
	return fmt.Sprintf("%d músicas", n)
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
