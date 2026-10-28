package spotify

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/importer"
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/screens"
	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/store"
	"github.com/ujjwal313/boombrick/internal/ui/uitest"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// fakeImporter holds jobs the test sets, and records what it is asked.
type fakeImporter struct {
	jobs     map[string]importer.Job
	imported []string
	used     []string // "id:candidate"
	skipped  []string
	cancels  []string
	version  uint64
}

func (f *fakeImporter) Import(uri, name string, ts []spotify.Track) {
	f.imported = append(f.imported, uri)
	if _, ok := f.jobs[uri]; !ok {
		j := importer.Job{URI: uri, Name: name}
		for _, t := range ts {
			j.Items = append(j.Items, importer.Item{Track: t})
		}
		f.jobs[uri] = j
	}
	f.version++
}

// Changes gives all of a job's songs each time; the screens' taking only the
// songs that changed is tested with the importer itself.
func (f *fakeImporter) Changes(uri string, since uint64) (importer.Changes, bool) {
	j, ok := f.jobs[uri]
	return importer.Changes{Version: f.version, All: true, Items: j.Items}, ok
}
func (f *fakeImporter) Err() error { return nil }
func (f *fakeImporter) Use(uri, id string, c int) {
	f.used = append(f.used, id+":"+strconv.Itoa(c))
	f.version++
}
func (f *fakeImporter) Skip(uri, id string) { f.skipped = append(f.skipped, id); f.version++ }
func (f *fakeImporter) Cancel(uri string)   { f.cancels = append(f.cancels, uri); f.version++ }
func (f *fakeImporter) Version() uint64     { return f.version }

// fakeQueue gives the download states the test sets, and counts how often
// it is asked one.
type fakeQueue struct {
	states  map[string]media.DownloadState
	paused  bool
	version uint64
	asked   int
}

func (q *fakeQueue) State(id string) (media.DownloadState, float32) {
	q.asked++
	if q.states[id] == media.Downloading {
		return media.Downloading, 0.42
	}
	return q.states[id], 0
}
func (q *fakeQueue) Paused() bool      { return q.paused }
func (q *fakeQueue) SetPaused(on bool) { q.paused = on }
func (q *fakeQueue) Note() string      { return "" }
func (q *fakeQueue) Version() uint64   { return q.version }

func cand(id, artist, title, album string, d time.Duration, score float64) importer.Candidate {
	return importer.Candidate{Song: ytmusic.Song{VideoID: id, Artist: artist, Title: title, Album: album, Duration: d}, Score: score}
}

// roadJob is Road trip's import: Sapphire matched, Going Bad unsure, and a
// song not found.
func roadJob() importer.Job {
	missing := spotify.Track{ID: "0nowhere0000000000000", Title: "Nowhere", Artists: []string{"Nobody"}, Duration: 200000}
	return importer.Job{URI: lists[1].URI, Name: "Road trip", Items: []importer.Item{
		{Track: road[0], State: importer.Matched, Candidates: []importer.Candidate{
			cand("a", "Ed Sheeran", "Sapphire", "Sapphire", 180*time.Second, 1)}},
		{Track: road[1], State: importer.Unsure, Candidates: []importer.Candidate{
			cand("g1", "Meek Mill", "Going Bad (feat. Drake)", "Championships", 192*time.Second, 0.75),
			cand("g2", "Meek Mill", "Going Bad (Live)", "", 181*time.Second, 0.45)}},
		{Track: missing, State: importer.NotFound},
	}}
}

func importEnv(nav *fakeNav, src *fakeSource) (*Env, *fakeImporter, *fakeQueue) {
	im := &fakeImporter{jobs: map[string]importer.Job{}}
	q := &fakeQueue{states: map[string]media.DownloadState{}}
	have := map[string]bool{road[0].ID: true}
	e := &Env{Nav: nav, Src: src, Player: &fakePlayer{}, Import: im, Queue: q,
		Copies:   func(id string) bool { return have[id] },
		Online:   func() bool { return true },
		Estimate: func() (float64, int64) { return 72, 386 << 27 }}
	return e, im, q
}

func TestChoose(t *testing.T) {
	nav := &fakeNav{}
	e, _, _ := importEnv(nav, signedIn())
	c := NewChoose(e)
	render(t, c)
	c.Handle(press(input.Start), t0)
	if len(nav.pushed) != 0 || len(nav.toasts) != 1 {
		t.Fatalf("Start with nothing chosen: pushed %d, toasts %v", len(nav.pushed), nav.toasts)
	}
	c.Handle(press(input.A), t0)    // Liked Songs
	c.Handle(press(input.Down), t0) // This Is Eminem
	c.Handle(press(input.Down), t0) // Road trip
	c.Handle(press(input.A), t0)
	uitest.Golden(t, "spotify-choose", render(t, c))
	if c.foot[0] != "2 playlists · 3 songs" || c.foot[1] == "" {
		t.Fatalf("footer %q", c.foot)
	}
	c.Handle(press(input.Start), t0)
	r, ok := nav.pushed[0].(*Review)
	if !ok || r.first || len(r.pls) != 2 || r.pls[0].URI != spotify.LikedURI || r.pls[1].URI != lists[1].URI {
		t.Fatalf("Start opened %+v", nav.pushed[0])
	}
	c.Handle(press(input.Y), t0) // all
	if len(c.chosen) != 3 || !c.chosen[lists[0].URI] {
		t.Fatalf("Y chose %v", c.chosen)
	}
}

// The footer counts a chosen playlist's songs once they come, in place of
// the estimate from its count.
func TestChooseCountsSongsAsTheyCome(t *testing.T) {
	src := signedIn()
	e, _, _ := importEnv(&fakeNav{}, src)
	c := NewChoose(e)
	render(t, c)
	c.Handle(press(input.Down), t0) // This Is Eminem, its songs not yet known
	c.Handle(press(input.A), t0)
	render(t, c)
	if c.foot[0] != "1 playlist · 50 songs" {
		t.Fatalf("estimated, the footer says %q", c.foot[0])
	}
	src.tracks[lists[0].URI] = road
	render(t, c)
	if c.foot[0] != "1 playlist · 2 songs" {
		t.Fatalf("with the songs known, the footer says %q", c.foot[0])
	}
}

func TestReview(t *testing.T) {
	nav := &fakeNav{}
	e, im, _ := importEnv(nav, signedIn())
	im.jobs[lists[1].URI] = roadJob()
	r := NewReview(e, []spotify.Playlist{lists[1], liked()}, true)
	render(t, r)
	if !slices.Equal(im.imported, []string{lists[1].URI, spotify.LikedURI}) {
		t.Fatalf("imported %v", im.imported)
	}
	if r.counts != "1 matched · 1 unsure · 1 not found" {
		t.Fatalf("counts %q", r.counts)
	}
	r.Handle(press(input.Down), t0)
	uitest.Golden(t, "spotify-review", render(t, r))
	r.Handle(press(input.A), t0) // Use this
	r.Handle(press(input.X), t0) // Other matches
	if r.other == nil {
		t.Fatal("X didn't open the other matches")
	}
	uitest.Golden(t, "spotify-other-matches", render(t, r))
	r.Handle(press(input.Down), t0)
	r.Handle(press(input.A), t0)
	if !slices.Equal(im.used, []string{road[1].ID + ":0", road[1].ID + ":1"}) || r.other != nil {
		t.Fatalf("used %v, open %v", im.used, r.other != nil)
	}
	r.Handle(press(input.Up), t0)
	r.Handle(press(input.Y), t0) // skip Sapphire
	if !slices.Equal(im.skipped, []string{road[0].ID}) {
		t.Fatalf("skipped %v", im.skipped)
	}
	r.Handle(press(input.R2), t0)
	render(t, r)
	if r.cur != 1 || r.job.URI != spotify.LikedURI {
		t.Fatalf("R2 shows %d, %q", r.cur, r.job.URI)
	}
	r.Handle(press(input.Start), t0)
	if _, ok := nav.pushed[0].(*Progress); !ok {
		t.Fatalf("Start opened %T", nav.pushed[0])
	}
}

// Leaving the downloads leaves the import's pages; X cancels it first.
func TestProgress(t *testing.T) {
	nav := &fakeNav{}
	e, im, q := importEnv(nav, signedIn())
	j := roadJob()
	j.Items[1].State = importer.Accepted
	im.jobs[lists[1].URI] = j
	q.states["a"] = media.Saved
	q.states["g1"] = media.Downloading
	review := NewReview(e, []spotify.Playlist{lists[1]}, true)
	p := NewProgress(e, []spotify.Playlist{lists[1]})
	uitest.Golden(t, "spotify-progress", render(t, p))
	if p.counter != "1" || p.of != "of 2 songs" {
		t.Fatalf("counter %q %q", p.counter, p.of)
	}
	p.Handle(press(input.Y), t0)
	if !q.paused {
		t.Fatal("Y didn't pause")
	}
	// B pops the page itself (the shell does), and the review, the import's
	// first page, pops itself when next drawn.
	if p.Back() || !e.leaving {
		t.Fatal("B doesn't leave the import")
	}
	nav.pushed = []screens.Screen{review}
	render(t, review)
	if len(nav.pushed) != 0 || e.leaving {
		t.Fatalf("the first page didn't close: %d pages, leaving %v", len(nav.pushed), e.leaving)
	}
	nav.pushed = []screens.Screen{p}
	p.Handle(press(input.X), t0)
	if !slices.Equal(im.cancels, []string{lists[1].URI}) || !e.leaving || len(nav.pushed) != 0 {
		t.Fatalf("cancels %v, leaving %v, %d pages", im.cancels, e.leaving, len(nav.pushed))
	}
}

// The downloading page asks the queue for the rows it draws, and counts the
// songs again only when the queue has changed.
func TestProgressCountsWhenTheQueueChanges(t *testing.T) {
	e, im, q := importEnv(&fakeNav{}, signedIn())
	j := roadJob()
	j.Items[1].State = importer.Accepted
	im.jobs[lists[1].URI] = j
	p := NewProgress(e, []spotify.Playlist{lists[1]})
	render(t, p)
	q.asked = 0
	render(t, p)
	if q.asked != 4 {
		t.Fatalf("asked %d times for two rows drawn twice", q.asked)
	}
	q.states["a"], q.version = media.Saved, q.version+1
	q.asked = 0
	render(t, p)
	if q.asked != 6 || p.counter != "1" {
		t.Fatalf("with a song saved, asked %d times, counter %q", q.asked, p.counter)
	}
}

// realImport is an importer on a database of its own, asking search; with no
// search it matches nothing.
func realImport(tb testing.TB, q *fakeQueue, search func(context.Context, string, ytmusic.Filter) ([]ytmusic.Item, error)) *importer.Importer {
	tb.Helper()
	s, err := store.Open(filepath.Join(tb.TempDir(), "boombrick.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { s.Close() })
	im, err := importer.New(importer.Options{DB: s.DB, Search: search, Get: func([]ytmusic.Song) {}, State: q.State,
		Remove: func(string) {}, Online: func() bool { return search != nil }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Pause: time.Millisecond})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(im.Close)
	return im
}

// The import's pages follow its songs as they change, making again only
// what changed, and show what pages made afresh would.
func TestPagesFollowAnImport(t *testing.T) {
	answers := map[string][]ytmusic.Item{
		"Ed Sheeran Sapphire": {{Kind: ytmusic.SongItem, ID: "a", Title: "Sapphire", Artist: "Ed Sheeran", Album: "Sapphire",
			Duration: 179 * time.Second}},
		"Meek Mill Going Bad (feat. Drake)": {{Kind: ytmusic.SongItem, ID: "g1", Title: "Going Bad", Artist: "Meek Mill",
			Duration: 188 * time.Second}, {Kind: ytmusic.SongItem, ID: "g2", Title: "Going Bad (Live)", Artist: "Meek Mill"}},
	}
	q := &fakeQueue{states: map[string]media.DownloadState{}}
	im := realImport(t, q, func(_ context.Context, query string, _ ytmusic.Filter) ([]ytmusic.Item, error) {
		return answers[query], nil
	})
	e := &Env{Nav: &fakeNav{}, Src: signedIn(), Player: &fakePlayer{}, Import: im, Queue: q}
	pls := []spotify.Playlist{lists[1]}
	r, p := NewReview(e, pls, true), NewProgress(e, pls)
	same := func(when string) {
		t.Helper()
		r.refresh()
		p.refresh()
		fresh, freshP := NewReview(e, pls, true), NewProgress(e, pls)
		fresh.started = r.started
		fresh.refresh()
		freshP.refresh()
		if !slices.Equal(r.rows, fresh.rows) || r.counts != fresh.counts || !reflect.DeepEqual(r.job, fresh.job) {
			t.Fatalf("%s, the review shows %q %+v, afresh %q %+v", when, r.counts, r.rows, fresh.counts, fresh.rows)
		}
		if !reflect.DeepEqual(shown(p), shown(freshP)) {
			t.Fatalf("%s, the downloads show %q, afresh %q", when, shown(p), shown(freshP))
		}
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(time.Millisecond) {
		same("matching")
		if j, _ := im.Job(lists[1].URI); j.Count(importer.Waiting) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still matching")
		}
	}
	if len(p.rows) == 0 || len(shown(p)) == len(r.rows) {
		t.Fatalf("matched, the downloads show %q of %d songs", shown(p), len(r.rows))
	}
	im.Use(lists[1].URI, road[1].ID, 1)
	same("with another match used")
	im.Skip(lists[1].URI, road[0].ID)
	same("with a song skipped")
}

// shown is the downloading page's rows' labels and songs.
func shown(p *Progress) []string {
	var out []string
	for _, r := range p.rows {
		f := p.jobs[r.job]
		out = append(out, f.labels[r.song]+" "+f.songs[r.song].VideoID)
	}
	return out
}

// BenchmarkReview is the review taking a change to one song of a 2,000-song
// import.
func BenchmarkReview(b *testing.B) {
	im := realImport(b, &fakeQueue{}, nil)
	ts := make([]spotify.Track, 2000)
	for i := range ts {
		ts[i] = road[1]
		ts[i].ID = strconv.Itoa(i)
	}
	pl := spotify.Playlist{URI: lists[1].URI, Name: "Big"}
	src := signedIn()
	src.tracks[pl.URI] = ts
	r := NewReview(&Env{Nav: &fakeNav{}, Src: src, Player: &fakePlayer{}, Import: im}, []spotify.Playlist{pl}, true)
	r.refresh()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		im.Skip(pl.URI, ts[i%len(ts)].ID)
		r.refresh()
		i++
	}
}
