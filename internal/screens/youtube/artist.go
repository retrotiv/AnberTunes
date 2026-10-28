package youtube

import (
	"cmp"
	"strconv"
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/screens"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/text"
	"github.com/ujjwal313/boombrick/internal/ui/views"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// pageHints are the pages' hints, kept so that no frame builds them. An
// album's page, or an artist's without releases, has nothing to open and
// shows them from [1:].
var pageHints = []widgets.Hint{{Key: "A", Label: "Open"}, {Key: "B", Label: "Back"}, {Key: "L1/R1", Label: "Tabs"}}

// ArtistPage shows a YouTube Music artist's albums, singles and EPs in the
// grid the Library's artist page uses. A opens one.
type ArtistPage struct {
	*shared
	item ytmusic.Item // the result it opened from
	page ytmusic.ArtistPage
	have bool
	name string   // the result's, or else the page's
	meta string   // "4 releases", once the page has come
	subs []string // the cards' second lines
	arts []widgets.ArtSpec
	grid views.Grid
	top  float32 // the grid's top, once the heading is measured
	msg  widgets.Wrapped
}

// NewArtistPage opens the artist result it.
func NewArtistPage(nav screens.Nav, src Source, dl Downloads, it ytmusic.Item) *ArtistPage {
	return newArtistPage(&shared{nav: nav, src: src, dl: dl}, it)
}

// newArtistPage opens the artist result it, sharing sh with the screen that
// opened it.
func newArtistPage(sh *shared, it ytmusic.Item) *ArtistPage {
	return &ArtistPage{shared: sh, item: it, name: it.Title, grid: screens.AlbumGrid()}
}

func (pg *ArtistPage) load() ytmusic.State {
	if pg.have {
		return ytmusic.Done
	}
	page, st := pg.src.Artist(pg.item.ID)
	if st != ytmusic.Done {
		return st
	}
	pg.page, pg.have = page, true
	pg.name, pg.meta = cmp.Or(pg.item.Title, page.Name), releases(len(page.Albums))
	pg.subs = make([]string, len(page.Albums))
	pg.arts = make([]widgets.ArtSpec, len(page.Albums))
	for i, a := range page.Albums {
		pg.subs[i] = a.Type
		if a.Year > 0 {
			pg.subs[i] += " · " + strconv.Itoa(a.Year)
		}
		pg.arts[i] = art(a.Title, a.Thumb)
	}
	pg.grid.SetCount(len(page.Albums))
	return st
}

// emptyText says why there are no releases to show.
func (pg *ArtistPage) emptyText(st ytmusic.State) string {
	if st == ytmusic.Done {
		return "No releases to show."
	}
	return stateText(st)
}

// Handle moves through the releases and opens one with A.
func (pg *ArtistPage) Handle(ev input.Event, now time.Time) bool {
	if ev.Kind == input.Release || pg.load() != ytmusic.Done {
		return false
	}
	if ev.Kind == input.Press && ev.Button == input.A && pg.grid.Count() > 0 {
		pg.nav.Push(newAlbumPage(pg.shared, pg.page.Albums[pg.grid.Focus()]))
		return true
	}
	return pg.grid.Move(ev.Button, now)
}

// Animating reports whether focus or the grid is moving.
func (pg *ArtistPage) Animating(now time.Time) bool { return pg.grid.Animating(now) }

// Draw renders the frame.
func (pg *ArtistPage) Draw(u *ui.Context) {
	pg.pollAlbums()
	st := pg.load()
	if pg.top == 0 {
		pg.top = screens.PageTop + u.LineHeight(text.Heading)
		screens.LayoutGrid(u, &pg.grid, screens.ContentBot-pg.top)
	}
	widgets.DrawBackground(u, widgets.TabGround)
	screens.Heading(u, pg.name, pg.meta, screens.PageTop)
	if pg.grid.Count() > 0 {
		u.PushClip(screens.GlowArea)
		pg.grid.Draw(u.Now, screens.GridX, pg.top, func(i int, x, y, k float32) {
			widgets.CardGlow(u, x, y, screens.GridArt, k)
			u.PushClip(screens.Below(pg.top))
			widgets.Card(u, widgets.CardItem{Title: pg.page.Albums[i].Title, Subtitle: pg.subs[i], Art: pg.arts[i]},
				x, y, screens.GridArt, k, true)
			u.PopClip()
		})
		u.PopClip()
	} else {
		message(u, &pg.msg, pg.emptyText(st), gfx.Rect{X: screens.Side, Y: pg.top, W: theme.ScreenW - 2*screens.Side,
			H: screens.ContentBot - pg.top})
	}
	widgets.StatusBar(u)
	widgets.Tabs(u, tab)
	if pg.grid.Count() > 0 {
		widgets.HintBar(u, pageHints)
	} else {
		widgets.HintBar(u, pageHints[1:])
	}
}

// releases counts an artist's releases: "1 release", "12 releases".
func releases(n int) string {
	if n == 1 {
		return "1 release"
	}
	return screens.Count(n) + " releases"
}
