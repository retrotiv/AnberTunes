package youtube

import (
	"cmp"
	"strconv"
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/screens"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/text"
	"github.com/ujjwal313/boombrick/internal/ui/views"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// The album page's pills: Play, and Download as an icon alone, as the
// Library's Shuffle is, so that both fit the column.
const (
	playPill = iota
	getPill
	pillCount
)

// noSongs is what an album without tracks says.
const noSongs = "This album has no songs to play."

// The album page's hints, kept so that no frame builds them: on a track, and
// on each pill. Without pills, the page shows pageHints[1:].
var (
	trackHints = []widgets.Hint{{Key: "A", Label: "Play"}, {Key: "X", Label: "Download"}, {Key: "Y", Label: "Add to queue"},
		{Key: "B", Label: "Back"}, tabsHint}
	pillHints = [pillCount][]widgets.Hint{
		playPill: {{Key: "A", Label: "Play"}, {Key: "B", Label: "Back"}, tabsHint},
		getPill:  {{Key: "A", Label: "Download album"}, {Key: "B", Label: "Back"}, tabsHint},
	}
)

// AlbumPage shows a YouTube Music album as the Library shows one: its art,
// details and pills on the left, its tracks on the right with their
// download state. With downloads, focus starts on the Play pill; without,
// there are no pills and focus is on the tracks.
type AlbumPage struct {
	*shared
	item   ytmusic.Item // the result it opened from, shown until the page comes
	page   ytmusic.AlbumPage
	have   bool
	songs  []ytmusic.Song // the tracks as downloads
	rows   []widgets.Row
	list   views.Grid
	onList bool
	pill   int
	pills  [pillCount]anim.Value // focus strength
	msg    widgets.Wrapped

	// The details on the left, made when they change.
	art   widgets.ArtSpec
	title string
	who   string // "Artist · 2023"
	count string // "12 songs · 46 min", once the page has come
}

// NewAlbumPage opens the album result it.
func NewAlbumPage(nav screens.Nav, src Source, dl Downloads, it ytmusic.Item) *AlbumPage {
	return newAlbumPage(&shared{nav: nav, src: src, dl: dl}, it)
}

// newAlbumPage opens the album result it, sharing sh with the screen that
// opened it.
func newAlbumPage(sh *shared, it ytmusic.Item) *AlbumPage {
	pg := &AlbumPage{shared: sh, item: it, onList: sh.dl == nil,
		list: views.Grid{Cols: 1, CellW: theme.ScreenW - screens.Side - screens.PageListX, CellH: widgets.RowHeight,
			Pad: screens.Pad, ViewH: screens.ContentBot - screens.ContentTop}}
	if !pg.onList {
		pg.pills[playPill].Set(1)
	}
	pg.details()
	return pg
}

// details makes the lines beside the art. What the result said comes first,
// being on screen already: its picture's address differs from the page's,
// and the page's would fetch the picture again. The page fills what the
// result lacks.
func (pg *AlbumPage) details() {
	pg.title = cmp.Or(pg.item.Title, pg.page.Title)
	pg.art = art(pg.title, cmp.Or(pg.item.Thumb, pg.page.Thumb))
	pg.who = cmp.Or(pg.item.Artist, pg.page.Artist)
	if year := cmp.Or(pg.item.Year, pg.page.Year); year > 0 {
		if pg.who != "" {
			pg.who += " · "
		}
		pg.who += strconv.Itoa(year)
	}
}

// load takes the page once it has come, and says how far it has got.
func (pg *AlbumPage) load() ytmusic.State {
	if pg.have {
		return ytmusic.Done
	}
	page, st := pg.src.Album(pg.item.ID)
	if st != ytmusic.Done {
		return st
	}
	pg.page, pg.have, pg.songs = page, true, pageSongs(page)
	pg.rows = make([]widgets.Row, len(page.Tracks))
	var length time.Duration
	for i, t := range page.Tracks {
		r := widgets.Row{Index: strconv.Itoa(t.Number), Title: t.Title, Subtitle: t.Artist, Trail: circle}
		if t.Number == 0 {
			r.Index = strconv.Itoa(i + 1)
		}
		if t.Duration > 0 {
			r.Duration = screens.Clock(t.Duration)
		}
		pg.rows[i] = r
		length += t.Duration
	}
	pg.list.SetCount(len(page.Tracks))
	if !pg.onList {
		pg.list.SetActive(false, time.Time{})
	}
	pg.details()
	pg.count = screens.SongsAndTime(len(page.Tracks), length)
	return st
}

// emptyText says why there are no tracks to show.
func (pg *AlbumPage) emptyText(st ytmusic.State) string {
	if st == ytmusic.Done {
		return noSongs
	}
	return stateText(st)
}

// pillsShown reports whether the page has its pills: with downloads, once
// there are tracks to play.
func (pg *AlbumPage) pillsShown() bool { return pg.dl != nil && len(pg.songs) > 0 }

// Handle moves between the pills and the tracks, plays with A, downloads
// with X and queues with Y.
func (pg *AlbumPage) Handle(ev input.Event, now time.Time) bool {
	if ev.Kind == input.Release || pg.load() != ytmusic.Done || len(pg.songs) == 0 {
		return false
	}
	if ev.Kind == input.Press {
		f := pg.list.Focus()
		one := pg.songs[f : f+1 : f+1] // capped, so that appending to it can't write over the next track
		switch ev.Button {
		case input.A:
			switch {
			case pg.onList:
				return play(pg.dl, pg.songs, f)
			case pg.pill == playPill:
				return play(pg.dl, pg.songs, 0)
			}
			return get(pg.nav, pg.dl, pg.songs)
		case input.X:
			return pg.onList && get(pg.nav, pg.dl, one)
		case input.Y:
			return pg.onList && enqueue(pg.nav, pg.dl, one)
		}
	}
	if pg.onList {
		if pg.list.Move(ev.Button, now) {
			return true
		}
		if ev.Button != input.Left || pg.dl == nil {
			return false
		}
		pg.onList = false
		pg.list.SetActive(false, now)
		pg.pills[pg.pill].To(1, now, anim.Focus, anim.EaseOutCubic)
		return true
	}
	next := pg.pill
	switch ev.Button {
	case input.Left:
		next--
	case input.Right:
		next++
	default:
		return false
	}
	switch {
	case next == pillCount:
		pg.pills[pg.pill].To(0, now, anim.Focus, anim.EaseOutCubic)
		pg.onList = true
		pg.list.SetActive(true, now)
	case next >= 0:
		pg.pills[pg.pill].To(0, now, anim.Focus, anim.EaseOutCubic)
		pg.pill = next
		pg.pills[next].To(1, now, anim.Focus, anim.EaseOutCubic)
	default:
		return false
	}
	return true
}

// Animating reports whether focus or the list is moving.
func (pg *AlbumPage) Animating(now time.Time) bool {
	return pg.list.Animating(now) || !pg.pills[playPill].Done(now) || !pg.pills[getPill].Done(now)
}

// ShowsDownloads reports whether the tracks' download circles show.
func (pg *AlbumPage) ShowsDownloads() bool { return pg.dl != nil }

// Draw renders the frame. It also gets albums asked for on the results under
// it, whose pages may come while it is open.
func (pg *AlbumPage) Draw(u *ui.Context) {
	pg.pollAlbums()
	st := pg.load()
	widgets.DrawBackground(u, widgets.TabGround)
	widgets.Art(u, pg.art, screens.PageArt, widgets.CardRadius,
		gfx.Rect{X: screens.Side, Y: screens.PageTop, W: screens.PageArt, H: screens.PageArt})
	y := float32(screens.PageTop + screens.PageArt + screens.Pad)
	u.Text(u.Fit(pg.title, text.Heading, screens.PageArt), text.Heading, screens.Side, y, theme.Paper)
	y += u.LineHeight(text.Heading)
	u.Text(u.Fit(pg.who, text.Meta, screens.PageArt), text.Meta, screens.Side, y, theme.Ink2)
	if pg.have {
		y += u.LineHeight(text.Meta)
		u.Text(u.Fit(pg.count, text.Meta, screens.PageArt), text.Meta, screens.Side, y, theme.Ink3)
	}
	if pg.pillsShown() {
		pg.drawPills(u)
	}
	if len(pg.rows) > 0 {
		pg.drawTracks(u)
	} else {
		message(u, &pg.msg, pg.emptyText(st), gfx.Rect{X: screens.PageListX, Y: screens.ContentTop,
			W: theme.ScreenW - screens.Side - screens.PageListX, H: screens.ContentBot - screens.ContentTop})
	}
	widgets.StatusBar(u)
	widgets.Tabs(u, tab)
	widgets.HintBar(u, pg.hints())
}

// drawPills draws Play and the round Download pill along the column's foot.
func (pg *AlbumPage) drawPills(u *ui.Context) {
	y := float32(screens.ContentBot - screens.Pad - widgets.PillHeight)
	x := float32(screens.Side)
	k0, k1 := float32(pg.pills[playPill].At(u.Now)), float32(pg.pills[getPill].At(u.Now))
	widgets.PillGlow(u, "play", "Play", x, y, k0)
	x += widgets.Pill(u, "play", "Play", x, y, k0) + screens.PillGap
	widgets.PillGlow(u, "download", "", x, y, k1)
	widgets.Pill(u, "download", "", x, y, k1)
}

func (pg *AlbumPage) hints() []widgets.Hint {
	switch {
	case !pg.pillsShown():
		return pageHints[1:]
	case pg.onList:
		return trackHints
	}
	return pillHints[pg.pill]
}

func (pg *AlbumPage) drawTracks(u *ui.Context) {
	u.PushClip(screens.GlowArea)
	pg.list.Draw(u.Now, screens.PageListX, screens.ContentTop, func(i int, x, y, k float32) {
		box := gfx.Rect{X: x, Y: y, W: pg.list.CellW, H: widgets.RowHeight}
		widgets.RowGlow(u, box, widgets.RowRadius, k)
		u.PushClip(screens.Below(screens.ContentTop))
		widgets.ListRow(u, pg.rows[i], box, k)
		drawCircle(u, pg.dl, pg.page.Tracks[i].VideoID, box, circle)
		u.PopClip()
	})
	u.PopClip()
}
