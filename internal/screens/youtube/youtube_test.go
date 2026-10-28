package youtube

import (
	"image"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/fixture"
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/screens"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/uitest"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

func TestMain(m *testing.M) { uitest.Main(m) }

var t0 = time.Unix(1000, 0)

func press(b input.Button) input.Event { return input.Event{Button: b, Kind: input.Press} }

// fakeSource answers from its maps at once, and with state, Waiting unless
// set, for anything else. It records the searches asked.
type fakeSource struct {
	results  map[string][]ytmusic.Item // by "query Filter"
	albums   map[string]ytmusic.AlbumPage
	artists  map[string]ytmusic.ArtistPage
	state    ytmusic.State
	searched []string
}

func (s *fakeSource) Search(q string, f ytmusic.Filter) ([]ytmusic.Item, ytmusic.State) {
	key := q + " " + ytmusic.FilterNames[f]
	s.searched = append(s.searched, key)
	if items, ok := s.results[key]; ok {
		return items, ytmusic.Done
	}
	return nil, s.state
}

func (s *fakeSource) Album(id string) (ytmusic.AlbumPage, ytmusic.State) {
	if a, ok := s.albums[id]; ok {
		return a, ytmusic.Done
	}
	return ytmusic.AlbumPage{}, s.state
}

func (s *fakeSource) Artist(id string) (ytmusic.ArtistPage, ytmusic.State) {
	if a, ok := s.artists[id]; ok {
		return a, ytmusic.Done
	}
	return ytmusic.ArtistPage{}, s.state
}

type download struct {
	st media.DownloadState
	p  float32
}

// fakeDownloads gives the states it holds, and Available otherwise. It does
// nothing asked of it.
type fakeDownloads map[string]download

func (d fakeDownloads) State(id string) (media.DownloadState, float32) { return d[id].st, d[id].p }
func (fakeDownloads) Get([]ytmusic.Song)                               {}
func (fakeDownloads) Play([]ytmusic.Song, int)                         {}
func (fakeDownloads) Enqueue([]ytmusic.Song)                           {}

// recorder is Downloads that records what the screens ask of it.
type recorder struct {
	fakeDownloads
	got, queued [][]string
	played      []string       // "ids@start"
	lastGot     []ytmusic.Song // the slice Get was last given
}

func ids(songs []ytmusic.Song) []string {
	var out []string
	for _, s := range songs {
		out = append(out, s.VideoID)
	}
	return out
}

func (r *recorder) Get(s []ytmusic.Song)     { r.got, r.lastGot = append(r.got, ids(s)), s }
func (r *recorder) Enqueue(s []ytmusic.Song) { r.queued = append(r.queued, ids(s)) }
func (r *recorder) Play(s []ytmusic.Song, start int) {
	r.played = append(r.played, strings.Join(ids(s), ",")+"@"+strconv.Itoa(start))
}

// fakeNav records pushes, pops and toasts.
type fakeNav struct {
	pushed []screens.Screen
	toasts []string
	tabs   []int
}

func (n *fakeNav) Push(s screens.Screen) { n.pushed = append(n.pushed, s) }
func (n *fakeNav) Pop()                  { n.pushed = n.pushed[:len(n.pushed)-1] }
func (n *fakeNav) Toast(msg string)      { n.toasts = append(n.toasts, msg) }
func (n *fakeNav) ShowTab(t int)         { n.tabs = append(n.tabs, t) }

// render draws s twice, as the app would: the first frame asks for its
// pictures and the second shows them, faded in. set adjusts the context.
func render(t *testing.T, s screens.Screen, now time.Time, set ...func(*ui.Context)) *image.NRGBA {
	t.Helper()
	u, off := uitest.NewContext(t, theme.DeviceW, theme.DeviceH)
	uitest.WithCovers(u)
	u.Accent = theme.Derive(265, 0.12) // "Night Drive Theory", as the design's YouTube frames play
	u.Status = fixture.Status()
	for _, f := range set {
		f(u)
	}
	u.BeginFrame(now)
	s.Draw(u)
	u.BeginFrame(now.Add(anim.ArtFade))
	s.Draw(u)
	return off.Image()
}

func thumb(name string) string {
	return "https://yt3.googleusercontent.com/" + name + "=w544-h544-l90-rj"
}

var nightDrive = ytmusic.AlbumPage{ID: "MPREb_nd", Title: "Night Drive Theory", Artist: "Lumen Coast", ArtistID: "UClc",
	Type: "Album", Year: 2023, Thumb: thumb("nd"), Tracks: []ytmusic.Track{
		{VideoID: "v1", Number: 1, Title: "Night Drive Theory", Duration: 231 * time.Second},
		{VideoID: "v2", Number: 2, Title: "Sodium Lights", Duration: 204 * time.Second},
		{VideoID: "v3", Number: 3, Title: "Overpass", Artist: "Lumen Coast & Vela", Duration: 257 * time.Second},
		{VideoID: "v4", Number: 4, Title: "Halogen Hymn", Duration: 189 * time.Second},
		{VideoID: "v5", Number: 5, Title: "Exit 41", Duration: 312 * time.Second},
	}}

var lumenCoast = ytmusic.ArtistPage{ID: "UClc", Name: "Lumen Coast", Thumb: thumb("lc"), Albums: []ytmusic.Item{
	{Kind: ytmusic.AlbumItem, ID: "MPREb_nd", Title: "Night Drive Theory", Artist: "Lumen Coast", ArtistID: "UClc", Year: 2023,
		Type: "Album", Thumb: thumb("nd")},
	{Kind: ytmusic.AlbumItem, ID: "MPREb_tw", Title: "Tidewater", Artist: "Lumen Coast", ArtistID: "UClc", Year: 2020,
		Type: "Album", Thumb: thumb("tw")},
	{Kind: ytmusic.AlbumItem, ID: "MPREb_hs", Title: "Harbour Static", Artist: "Lumen Coast", ArtistID: "UClc", Year: 2024,
		Type: "Single", Thumb: thumb("hs")},
	{Kind: ytmusic.AlbumItem, ID: "MPREb_ss", Title: "Slow Signal", Artist: "Lumen Coast", ArtistID: "UClc", Year: 2018,
		Type: "EP", Thumb: thumb("ss")},
}}

// nightResults are what searching "night drive" finds, the top result first.
var nightResults = []ytmusic.Item{
	{Kind: ytmusic.AlbumItem, ID: "MPREb_nd", Title: "Night Drive Theory", Artist: "Lumen Coast", ArtistID: "UClc", Year: 2023,
		Type: "Album", Thumb: thumb("nd")},
	{Kind: ytmusic.SongItem, ID: "v1", Title: "Night Drive Theory", Artist: "Lumen Coast", Album: "Night Drive Theory",
		AlbumID: "MPREb_nd", Duration: 231 * time.Second, Thumb: thumb("nd")},
	{Kind: ytmusic.ArtistItem, ID: "UClc", Title: "Lumen Coast", Audience: "210K monthly audience", Thumb: thumb("lc")},
	{Kind: ytmusic.SongItem, ID: "s2", Title: "Night Drive", Artist: "Kenji Arata", Duration: 198 * time.Second, Thumb: thumb("k")},
	{Kind: ytmusic.SongItem, ID: "s3", Title: "Drive All Night", Artist: "Odalys", Duration: 245 * time.Second, Thumb: thumb("o")},
	{Kind: ytmusic.SongItem, ID: "s4", Title: "Nightdriver", Artist: "The Hollow Pines", Duration: 221 * time.Second, Thumb: thumb("h")},
	{Kind: ytmusic.AlbumItem, ID: "MPREb_nc", Title: "Night Classics", Artist: "Various Artists", Year: 2019, Type: "Album",
		Thumb: thumb("nc")},
}

func TestAlbumPage(t *testing.T) {
	src := &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": nightDrive}}
	dl := &recorder{fakeDownloads: fakeDownloads{"v1": {media.Saved, 0}, "v2": {media.Downloading, 0.4}, "v4": {media.Queued, 0}}}
	nav := &fakeNav{}
	pg := NewAlbumPage(nav, src, dl, nightResults[0])
	uitest.Golden(t, "yt-album", render(t, pg, t0))
	for _, b := range []input.Button{input.A, input.Right, input.A, input.Right, input.Down, input.A, input.X, input.Y} {
		pg.Handle(press(b), t0)
	}
	all := "v1,v2,v3,v4,v5"
	if !slices.Equal(dl.played, []string{all + "@0", all + "@1"}) || len(dl.got) != 2 || len(dl.got[0]) != 5 ||
		!slices.Equal(dl.got[1], []string{"v2"}) || len(dl.queued) != 1 || !slices.Equal(dl.queued[0], []string{"v2"}) {
		t.Fatalf("played %q, got %q, queued %q", dl.played, dl.got, dl.queued)
	}
	// The album's toast counts the four songs not saved yet.
	if want := []string{"Downloading 4 songs", "Downloading “Sodium Lights”", screens.AddedToQueue}; !slices.Equal(nav.toasts, want) {
		t.Fatalf("toasts %q, want %q", nav.toasts, want)
	}
	pg.Handle(press(input.Up), t0)
	pg.Handle(press(input.X), t0) // a saved track
	if len(dl.got) != 2 || nav.toasts[len(nav.toasts)-1] != "Already downloaded" {
		t.Fatalf("X on a saved track: got %q, toasts %q", dl.got, nav.toasts)
	}
}

// A track is given alone, so that appending to it can't write over the
// page's next track.
func TestAlbumPageGivesATrackAlone(t *testing.T) {
	src := &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": nightDrive}}
	dl := &recorder{}
	pg := NewAlbumPage(&fakeNav{}, src, dl, nightResults[0])
	render(t, pg, t0)
	for _, b := range []input.Button{input.Right, input.Right, input.X} {
		pg.Handle(press(b), t0)
	}
	_ = append(dl.lastGot, ytmusic.Song{VideoID: "zz"})
	if len(dl.lastGot) != 1 || pg.songs[1].VideoID != "v2" {
		t.Fatalf("given %q; the page's second track is now %q", ids(dl.lastGot), pg.songs[1].VideoID)
	}
}

// Left from the tracks goes back to the pill last focused.
func TestAlbumPageBackToThePills(t *testing.T) {
	src := &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": nightDrive}}
	dl := &recorder{}
	pg := NewAlbumPage(&fakeNav{}, src, dl, nightResults[0])
	render(t, pg, t0)
	for _, b := range []input.Button{input.Right, input.Right, input.Down} { // Download, the tracks, track 2
		pg.Handle(press(b), t0)
	}
	if !pg.onList || pg.list.Focus() != 1 {
		t.Fatalf("on the tracks %v, at %d", pg.onList, pg.list.Focus())
	}
	if !pg.Handle(press(input.Left), t0) || pg.onList || pg.pill != getPill {
		t.Fatalf("Left from the tracks: on the tracks %v, pill %d", pg.onList, pg.pill)
	}
	pg.Handle(press(input.A), t0)
	if len(dl.got) != 1 || len(dl.got[0]) != 5 || len(dl.played) != 0 {
		t.Fatalf("A on the Download pill: got %q, played %q", dl.got, dl.played)
	}
}

// Without downloads the page only browses: no pills, and focus on the tracks.
func TestAlbumPageWithoutDownloads(t *testing.T) {
	src := &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": nightDrive}}
	pg := NewAlbumPage(&fakeNav{}, src, nil, nightResults[0])
	render(t, pg, t0)
	if !pg.Handle(press(input.Down), t0) || pg.list.Focus() != 1 {
		t.Fatal("Down doesn't move through the tracks")
	}
	for _, b := range []input.Button{input.A, input.X, input.Y, input.Left} {
		if pg.Handle(press(b), t0) {
			t.Errorf("%v did something without downloads", b)
		}
	}
}

func TestAlbumPageHintsFit(t *testing.T) {
	u, _ := uitest.NewContext(t, 64, 64)
	src := &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": nightDrive}}
	pg := NewAlbumPage(&fakeNav{}, src, &recorder{}, nightResults[0])
	render(t, pg, t0)
	for _, b := range []input.Button{input.Right, input.Right} { // the Play pill, Download, the tracks
		if w := widgets.HintsWidth(u, pg.hints()); w > widgets.MaxHintsWidth {
			t.Errorf("hints %v are %v wide", pg.hints(), w)
		}
		pg.Handle(press(b), t0)
	}
	if h := pg.hints(); h[0].Label != "Play" || h[1].Label != "Download" || h[2].Label != "Add to queue" {
		t.Fatalf("a track's hints are %v", h)
	}
	if w := widgets.HintsWidth(u, pg.hints()); w > widgets.MaxHintsWidth {
		t.Errorf("a track's hints are %v wide", w)
	}
}

// An album with nothing to play has no pills to focus.
func TestEmptyAlbumPageWithDownloads(t *testing.T) {
	album := nightDrive
	album.Tracks = nil
	dl := &recorder{}
	pg := NewAlbumPage(&fakeNav{}, &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": album}}, dl, nightResults[0])
	render(t, pg, t0)
	for _, b := range []input.Button{input.A, input.Right, input.A} {
		if pg.Handle(press(b), t0) {
			t.Errorf("%v did something on an empty album", b)
		}
	}
	if len(dl.played) != 0 || len(dl.got) != 0 || pg.hints()[0].Key != "B" {
		t.Fatalf("played %q, got %q, hints %v", dl.played, dl.got, pg.hints())
	}
}

func TestAlbumPageWhileItLoads(t *testing.T) {
	pg := NewAlbumPage(&fakeNav{}, &fakeSource{}, nil, nightResults[0])
	uitest.Golden(t, "yt-album-loading", render(t, pg, t0))
	if pg.Handle(press(input.Down), t0) {
		t.Fatal("moved through tracks that aren't there yet")
	}
}

func TestArtistPage(t *testing.T) {
	nav := &fakeNav{}
	src := &fakeSource{artists: map[string]ytmusic.ArtistPage{"UClc": lumenCoast}}
	pg := NewArtistPage(nav, src, nil, nightResults[2])
	uitest.Golden(t, "yt-artist", render(t, pg, t0))
	pg.Handle(press(input.Right), t0)
	pg.Handle(press(input.A), t0)
	if len(nav.pushed) != 1 {
		t.Fatalf("%d pages opened", len(nav.pushed))
	}
	if ap, ok := nav.pushed[0].(*AlbumPage); !ok || ap.item.ID != "MPREb_tw" {
		t.Fatalf("opened %#v", nav.pushed[0])
	}
}

// When the page comes, the details stay as the result showed them, the
// picture above all: another address would fetch it again. What the result
// lacks comes from the page.
func TestAlbumPageKeepsWhatTheResultShowed(t *testing.T) {
	page := nightDrive
	page.Thumb, page.Artist, page.Year = thumb("nd-header"), "", 0
	pg := NewAlbumPage(&fakeNav{}, &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": page}}, nil, nightResults[0])
	render(t, pg, t0)
	if !pg.have || pg.art.Cover.URL != nightResults[0].Thumb || pg.who != "Lumen Coast · 2023" {
		t.Fatalf("loaded %v: picture %q, %q", pg.have, pg.art.Cover.URL, pg.who)
	}

	bare := ytmusic.Item{Kind: ytmusic.AlbumItem, ID: "MPREb_nd"}
	pg = NewAlbumPage(&fakeNav{}, &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": nightDrive}}, nil, bare)
	render(t, pg, t0)
	if pg.title != nightDrive.Title || pg.art.Cover.URL != nightDrive.Thumb || pg.who != "Lumen Coast · 2023" {
		t.Fatalf("from the page: %q, picture %q, %q", pg.title, pg.art.Cover.URL, pg.who)
	}
}

func TestArtistPageKeepsTheResultsName(t *testing.T) {
	page := lumenCoast
	page.Name = "LUMEN COAST"
	pg := NewArtistPage(&fakeNav{}, &fakeSource{artists: map[string]ytmusic.ArtistPage{"UClc": page}}, nil, nightResults[2])
	render(t, pg, t0)
	if pg.name != "Lumen Coast" || pg.meta != "4 releases" {
		t.Fatalf("heading %q, %q", pg.name, pg.meta)
	}
}

func TestEmptyAlbumPage(t *testing.T) {
	album := nightDrive
	album.Tracks = nil
	pg := NewAlbumPage(&fakeNav{}, &fakeSource{albums: map[string]ytmusic.AlbumPage{"MPREb_nd": album}}, nil, nightResults[0])
	uitest.Golden(t, "yt-album-empty", render(t, pg, t0))
	if got := pg.emptyText(pg.load()); got != "This album has no songs to play." {
		t.Fatalf("an empty album says %q", got)
	}
}

func TestEmptyArtistPage(t *testing.T) {
	artist := lumenCoast
	artist.Albums = nil
	pg := NewArtistPage(&fakeNav{}, &fakeSource{artists: map[string]ytmusic.ArtistPage{"UClc": artist}}, nil, nightResults[2])
	uitest.Golden(t, "yt-artist-empty", render(t, pg, t0))
	if got := pg.emptyText(pg.load()); got != "No releases to show." {
		t.Fatalf("an artist without releases says %q", got)
	}
}

func TestStateTextsDiffer(t *testing.T) {
	seen := map[string]bool{}
	for _, st := range []ytmusic.State{ytmusic.Waiting, ytmusic.Offline, ytmusic.Failed, ytmusic.Changed} {
		s := stateText(st)
		if s == "" || seen[s] {
			t.Fatalf("state %d: %q", st, s)
		}
		seen[s] = true
	}
}

// The screens are redrawn for the download queue when they can download, the
// typing screen not while it says it is offline.
func TestScreensThatShowDownloads(t *testing.T) {
	src := &fakeSource{results: map[string][]ytmusic.Item{"night drive All": nightResults}}
	if NewResults(&fakeNav{}, src, nil, "night drive").ShowsDownloads() {
		t.Fatal("results that can't download show downloads")
	}
	if !NewResults(&fakeNav{}, src, &recorder{}, "night drive").ShowsDownloads() {
		t.Fatal("results that can download don't")
	}
	if !NewAlbumPage(&fakeNav{}, src, &recorder{}, nightResults[0]).ShowsDownloads() {
		t.Fatal("an album page that can download doesn't")
	}
	s := NewSearch(&fakeNav{}, src, &recorder{})
	if !s.ShowsDownloads() {
		t.Fatal("the typing screen doesn't")
	}
	render(t, s, t0, func(u *ui.Context) { u.Status.Online = false })
	if s.ShowsDownloads() {
		t.Fatal("the typing screen does while offline")
	}
}
