package spotify

import (
	"slices"
	"strconv"
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/screens"
	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
	"github.com/ujjwal313/boombrick/internal/ui/views"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

// The import pages' layout: full screen, a title and the steps under the
// status bar.
const (
	importTop = widgets.StatusHeight + 12
	checkBox  = 32
	checkX    = 18 // in a row's number column
	// songLength is how long a song is taken to be when its playlist's songs
	// aren't known, for the size an import comes to.
	songLength = 3*time.Minute + 30*time.Second
)

var (
	stepsText = text.Style{Face: text.OutfitSemiBold, Size: 26}
	footText  = text.Style{Face: text.Figtree, Size: 24, Tabular: true}
	freeText  = text.Style{Face: text.Figtree, Size: 22, Tabular: true}
)

// importSteps are the import's steps, as its pages show them.
var importSteps = [...]string{"1 Choose", "2 Review", "3 Download"}

// importHeader draws a page's title on the left and the steps on the right,
// the current one in the accent and underlined, and returns where the
// content starts.
func importHeader(u *ui.Context, title string, step int) float32 {
	u.Text(u.Fit(title, text.Title, theme.ScreenW/2), text.Title, screens.Side, importTop, theme.Paper)
	x := float32(theme.ScreenW - screens.Side)
	y := importTop + (u.LineHeight(text.Title)-u.LineHeight(stepsText))/2
	for i := len(importSteps) - 1; i >= 0; i-- {
		w := u.TextWidth(importSteps[i], stepsText)
		x -= w
		col := theme.Ink3
		if i == step {
			col = u.Accent.Color
			u.Fill(gfx.Rect{X: x, Y: y + u.LineHeight(stepsText), W: w, H: 2}, col)
		}
		u.Text(importSteps[i], stepsText, x, y, col)
		if i > 0 {
			x -= 14 + u.TextWidth("·", stepsText) + 14
			u.Text("·", stepsText, x+14, y, theme.Ink4)
		}
	}
	return importTop + u.LineHeight(text.Title) + 16
}

var (
	chooseHints = []widgets.Hint{{Key: "A", Label: "Choose"}, {Key: "Y", Label: "All"}, {Key: "Start", Label: "Continue"},
		{Key: "B", Label: "Cancel"}}
	chooseWait = []widgets.Hint{{Key: "B", Label: "Cancel"}}
)

// Choose picks the playlists to import, Liked Songs first, and says what
// they come to. It is the first page of an import from the Spotify tab.
type Choose struct {
	e      *Env
	pls    []spotify.Playlist
	chosen map[string]bool // by URI
	list   views.Grid
	rows   []widgets.Row
	arts   []widgets.ArtSpec
	k      []anim.Value       // the checkboxes' fill
	counts map[string]counted // the chosen playlists' songs as last counted, by URI
	foot   [2]string          // the footer's lines, written when the choice's tally changes
	footOf tally              // the tally they were written for
}

// tally is what playlists come to: how many, their songs, and about how long
// those play.
type tally struct {
	lists, songs int
	length       time.Duration
}

// counted is a playlist's tally, made from its songs once they are known and
// estimated from its count until then; known is how many songs it was made
// from, -1 for none.
type counted struct {
	tally
	known int
}

// NewChoose opens the page with nothing chosen.
func NewChoose(e *Env) *Choose {
	return &Choose{e: e, chosen: map[string]bool{}, counts: map[string]counted{},
		list: views.Grid{Cols: 1, CellW: theme.ScreenW - 2*screens.Side, CellH: widgets.RowHeight, Pad: screens.Pad}}
}

// Claims keeps Start, which continues.
func (c *Choose) Claims(b input.Button) bool { return b == input.Start }

// refresh takes the playlists as they now stand.
func (c *Choose) refresh() {
	pls, _, _ := c.e.Src.Playlists()
	first := liked()
	if ts, ok, _ := c.e.Src.Tracks(spotify.LikedURI); ok {
		first.Count = len(ts)
	}
	if len(c.pls) == len(pls)+1 && c.pls[0] == first && slices.Equal(c.pls[1:], pls) {
		return
	}
	all := append([]spotify.Playlist{first}, pls...)
	c.pls = all
	clear(c.counts)
	c.rows = make([]widgets.Row, len(all))
	c.arts = make([]widgets.ArtSpec, len(all))
	c.k = make([]anim.Value, len(all))
	for i, p := range all {
		c.arts[i] = art(p.Name, p.Cover)
		if p.URI == spotify.LikedURI {
			c.arts[i] = screens.LikedArt
		}
		// The row keeps its number column, blank, for the checkbox.
		c.rows[i] = widgets.Row{Index: " ", Art: &c.arts[i], Title: p.Name, Subtitle: songs(p.Count)}
		if o := owner(p); o != "" {
			c.rows[i].Subtitle += " · " + o
		}
		if c.chosen[p.URI] {
			c.k[i].Set(1)
		}
	}
	c.list.SetCountFocus(len(all), min(c.list.Focus(), len(all)-1))
}

// Handle chooses with A and all with Y, and continues with Start.
func (c *Choose) Handle(ev input.Event, now time.Time) bool {
	if ev.Kind == input.Release || c.e.leaving {
		return false
	}
	c.refresh()
	if ev.Kind == input.Press {
		switch ev.Button {
		case input.A:
			c.set(c.list.Focus(), !c.chosen[c.pls[c.list.Focus()].URI], now)
			return true
		case input.Y:
			all := !slices.ContainsFunc(c.pls, func(p spotify.Playlist) bool { return !c.chosen[p.URI] })
			for i := range c.pls {
				c.set(i, !all, now)
			}
			return true
		case input.Start:
			var pls []spotify.Playlist
			for _, p := range c.pls {
				if c.chosen[p.URI] {
					pls = append(pls, p)
				}
			}
			if len(pls) == 0 {
				c.e.Nav.Toast("Choose a playlist to import")
				return true
			}
			c.e.Nav.Push(NewReview(c.e, pls, false))
			return true
		}
	}
	return c.list.Move(ev.Button, now)
}

func (c *Choose) set(i int, on bool, now time.Time) {
	c.chosen[c.pls[i].URI] = on
	k := 0.0
	if on {
		k = 1
	}
	c.k[i].To(k, now, anim.Focus, anim.EaseOutCubic)
}

// Animating reports whether the list or a checkbox is moving.
func (c *Choose) Animating(now time.Time) bool {
	if c.list.Animating(now) {
		return true
	}
	for i := range c.k {
		if !c.k[i].Done(now) {
			return true
		}
	}
	return false
}

// Draw renders the frame; once the import's pages are closing, it closes,
// being their first.
func (c *Choose) Draw(u *ui.Context) {
	if c.e.leaving {
		c.e.leaving = false
		c.e.Nav.Pop()
	}
	c.refresh()
	widgets.DrawBackground(u, widgets.FlatGround)
	widgets.StatusBar(u)
	top := importHeader(u, "Import from Spotify", 0)
	footH := 16 + u.LineHeight(footText) + u.LineHeight(freeText) + 12
	bottom := screens.ContentBot - footH
	c.list.ViewH = bottom - top
	u.PushClip(gfx.Rect{X: 0, Y: top - screens.Pad, W: theme.ScreenW, H: bottom - top + screens.Pad})
	c.list.Draw(u.Now, screens.Side, top, func(i int, x, y, k float32) {
		box := gfx.Rect{X: x, Y: y, W: c.list.CellW, H: widgets.RowHeight}
		widgets.RowGlow(u, box, widgets.RowRadius, k)
		widgets.ListRow(u, c.rows[i], box, k)
		drawCheck(u, x+checkX, y+(widgets.RowHeight-checkBox)/2, float32(c.k[i].At(u.Now)))
	})
	u.PopClip()
	c.drawFoot(u, bottom)
	hints := chooseHints
	if len(c.pls) == 0 {
		hints = chooseWait
	}
	widgets.HintBar(u, hints)
}

// drawCheck draws a checkbox with its top-left at (x, y), filled to k.
func drawCheck(u *ui.Context, x, y, k float32) {
	box := gfx.Rect{X: x, Y: y, W: checkBox, H: checkBox}
	shape := raster.Shape{W: checkBox, H: checkBox, Radius: 8, Kind: raster.Ring, RingOffset: -2, RingWidth: 2}
	u.Shape(shape, box, theme.PaperA(0.45))
	if k > 0 {
		shape.Kind = raster.Fill
		a := u.Alpha
		u.Alpha *= k
		u.Shape(shape, box, u.Accent.Color)
		u.Icon("check", 22, 3, false, x+5, y+5, theme.Ground)
		u.Alpha = a
	}
}

// drawFoot draws, above a hairline, how many playlists and songs are chosen,
// and about how much room they take of what is free.
func (c *Choose) drawFoot(u *ui.Context, y float32) {
	var sum tally
	for _, p := range c.pls {
		if c.chosen[p.URI] {
			t := c.count(p)
			sum.lists += t.lists
			sum.songs += t.songs
			sum.length += t.length
		}
	}
	if sum != c.footOf || c.foot[0] == "" {
		c.footOf = sum
		pl := strconv.Itoa(sum.lists) + " playlists"
		if sum.lists == 1 {
			pl = "1 playlist"
		}
		c.foot[0] = pl + " · " + songs(sum.songs)
		c.foot[1] = ""
		if c.e.Estimate != nil {
			perHour, free := c.e.Estimate()
			c.foot[1] = "About " + screens.Size(int64(sum.length.Hours()*perHour*(1<<20))) + " · " + screens.Size(free) + " free"
		}
	}
	u.Fill(gfx.Rect{X: screens.Side, Y: y, W: theme.ScreenW - 2*screens.Side, H: 1}, theme.PaperA(0.12))
	y += 16
	u.Text(c.foot[0], footText, screens.Side, y, theme.Paper)
	u.Text(c.foot[1], freeText, screens.Side, y+u.LineHeight(footText), theme.Ink2)
}

// count is what p comes to, counted again only when how many of its songs
// are known changes.
func (c *Choose) count(p spotify.Playlist) tally {
	ts, ok, _ := c.e.Src.Tracks(p.URI)
	known := -1
	if ok {
		known = len(ts)
	}
	if n, had := c.counts[p.URI]; had && n.known == known {
		return n.tally
	}
	n := counted{tally: tally{lists: 1, songs: p.Count, length: time.Duration(p.Count) * songLength}, known: known}
	if ok {
		n.songs, n.length = len(ts), 0
		for _, t := range ts {
			n.length += time.Duration(t.Duration) * time.Millisecond
		}
	}
	c.counts[p.URI] = n
	return n.tally
}
