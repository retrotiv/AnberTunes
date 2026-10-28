package app

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/download"
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

type queueDownloads struct {
	saved   map[string]string
	removed map[string]bool // forgotten by the queue
	got     [][]string
	firsts  []string
	paused  bool
}

func (q *queueDownloads) State(id string) (media.DownloadState, float32) {
	if _, ok := q.saved[id]; ok {
		return media.Saved, 1
	}
	if q.removed[id] {
		return media.Available, 0
	}
	return media.Queued, 0
}
func (q *queueDownloads) Get(songs []ytmusic.Song) {
	var ids []string
	for _, s := range songs {
		ids = append(ids, s.VideoID)
	}
	q.got = append(q.got, ids)
}
func (q *queueDownloads) First(id string)               { q.firsts = append(q.firsts, id) }
func (q *queueDownloads) Path(id string) (string, bool) { p, ok := q.saved[id]; return p, ok }
func (q *queueDownloads) Paused() bool                  { return q.paused }

type queuePlayer struct {
	played   []player.Track
	start    int
	queued   []player.Track
	shuffles []bool
	resolved []string
	dropped  []string
}

func (p *queuePlayer) SetShuffle(on bool)                { p.shuffles = append(p.shuffles, on) }
func (p *queuePlayer) Play(ts []player.Track, start int) { p.played, p.start = ts, start }
func (p *queuePlayer) Enqueue(ts []player.Track)         { p.queued = append(p.queued, ts...) }
func (p *queuePlayer) Resolve(id, path string)           { p.resolved = append(p.resolved, id+" "+path) }
func (p *queuePlayer) Drop(id string) int                { p.dropped = append(p.dropped, id); return 1 }

var ytSongs = []ytmusic.Song{
	{VideoID: "v1", Title: "Night Drive Theory", Artist: "Lumen Coast", Album: "Night Drive Theory", Duration: 231 * time.Second},
	{VideoID: "v2", Title: "Sodium Lights", Artist: "Lumen Coast", Album: "Night Drive Theory", AlbumArtist: "Lumen Coast"},
}

func TestPlayingYouTubeSongs(t *testing.T) {
	dl := &queueDownloads{saved: map[string]string{"v1": "/m/Boombrick/v1.opus"}}
	p := &queuePlayer{}
	y := ytActions{dl: dl, p: p}
	y.Play(ytSongs, 1)
	if len(p.played) != 2 || p.start != 1 || !slices.Equal(p.shuffles, []bool{false}) {
		t.Fatalf("played %+v from %d", p.played, p.start)
	}
	saved, pending := p.played[0], p.played[1]
	if saved.Path != "/m/Boombrick/v1.opus" || saved.VideoID != "v1" || saved.Pending() || saved.AlbumArtist != "Lumen Coast" {
		t.Fatalf("the saved song: %+v", saved)
	}
	if !pending.Pending() || pending.Title != "Sodium Lights" {
		t.Fatalf("the song to download: %+v", pending)
	}
	if len(dl.got) != 1 || !slices.Equal(dl.firsts, []string{"v2"}) {
		t.Fatalf("got %q, first %q", dl.got, dl.firsts)
	}
	y.Play(ytSongs, 0) // a saved song starts at once: nothing goes first
	if len(dl.firsts) != 1 {
		t.Fatalf("first %q", dl.firsts)
	}
	y.Enqueue(ytSongs[1:])
	if len(p.queued) != 1 || !p.queued[0].Pending() || len(dl.got) != 3 {
		t.Fatalf("queued %+v, got %q", p.queued, dl.got)
	}
}

func TestSettlingDownloads(t *testing.T) {
	p := &queuePlayer{}
	landed, dropped := settle(p, []download.Outcome{{VideoID: "v1", Path: "/m/v1.opus"}, {VideoID: "v2", Failure: download.Network}})
	if !landed || dropped != 1 || !slices.Equal(p.resolved, []string{"v1 /m/v1.opus"}) || !slices.Equal(p.dropped, []string{"v2"}) {
		t.Fatalf("landed %v, dropped %d; resolved %q, dropped %q", landed, dropped, p.resolved, p.dropped)
	}
	if landed, _ := settle(p, nil); landed {
		t.Fatal("nothing landed, yet it says so")
	}
}

// Playback waiting on a song puts its download first, hands the player a file
// that has already landed, and asks again for a song the queue forgot.
func TestWaitingForADownload(t *testing.T) {
	dl := &queueDownloads{saved: map[string]string{"v1": "/m/v1.opus"}, removed: map[string]bool{"v3": true}}
	p := &queuePlayer{}
	waitFor(dl, p, player.Track{VideoID: "v1", Title: "Night Drive Theory"})
	if !slices.Equal(p.resolved, []string{"v1 /m/v1.opus"}) || len(dl.firsts) != 0 {
		t.Fatalf("resolved %q, first %q", p.resolved, dl.firsts)
	}
	waitFor(dl, p, player.Track{VideoID: "v2", Title: "Sodium Lights"})
	if !slices.Equal(dl.firsts, []string{"v2"}) || len(dl.got) != 0 {
		t.Fatalf("first %q, got %q", dl.firsts, dl.got)
	}
	waitFor(dl, p, player.Track{VideoID: "v3", Title: "Tidal", Artist: "Lumen Coast"})
	if !slices.Equal(dl.firsts, []string{"v2", "v3"}) || len(dl.got) != 1 || !slices.Equal(dl.got[0], []string{"v3"}) {
		t.Fatalf("first %q, got %q", dl.firsts, dl.got)
	}
}

// Playing a song still to download while downloads are paused says so.
func TestPlayingWhileDownloadsArePaused(t *testing.T) {
	dl := &queueDownloads{saved: map[string]string{"v1": "/m/v1.opus"}, paused: true}
	var toasts []string
	y := ytActions{dl: dl, p: &queuePlayer{}, toast: func(msg string) { toasts = append(toasts, msg) }}
	y.Play(ytSongs, 0) // saved: it plays
	if len(toasts) != 0 {
		t.Fatalf("toasts %q", toasts)
	}
	y.Play(ytSongs, 1)
	if !slices.Equal(toasts, []string{"Downloads are paused"}) {
		t.Fatalf("toasts %q", toasts)
	}
	dl.paused = false
	y.Play(ytSongs, 1)
	if len(toasts) != 1 {
		t.Fatalf("toasts %q", toasts)
	}
}

// eventually waits up to 5 s for ok.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(2 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// liveQueue is a download queue for followDownloads, safe to use from its
// goroutine. Each Ready hands out the outcomes given since the last.
type liveQueue struct {
	mu     sync.Mutex
	ready  chan struct{}
	outs   []download.Outcome
	takes  int
	busy   bool
	firsts []string
}

func newLiveQueue() *liveQueue { return &liveQueue{ready: make(chan struct{})} }

func (q *liveQueue) Status() download.Status                     { return download.Status{} }
func (q *liveQueue) Retry(string, bool)                          {}
func (q *liveQueue) Remove(string)                               {}
func (q *liveQueue) SetPaused(bool)                              {}
func (q *liveQueue) State(string) (media.DownloadState, float32) { return media.Queued, 0 }
func (q *liveQueue) Get([]ytmusic.Song)                          {}
func (q *liveQueue) Path(string) (string, bool)                  { return "", false }
func (q *liveQueue) Paused() bool                                { return false }
func (q *liveQueue) Note() string                                { return "" }
func (q *liveQueue) Version() uint64                             { return 0 }
func (q *liveQueue) Downloader() download.Downloader             { return download.Downloader{} }
func (q *liveQueue) UpdateDownloader()                           {}
func (q *liveQueue) Ready() <-chan struct{}                      { return q.ready }

func (q *liveQueue) First(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.firsts = append(q.firsts, id)
}

func (q *liveQueue) Busy() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.busy
}

func (q *liveQueue) setBusy(on bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.busy = on
}

func (q *liveQueue) Outcomes() []download.Outcome {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.takes++
	o := q.outs
	q.outs = nil
	return o
}

// end hands outs to followDownloads with a Ready, once it takes the Ready.
func (q *liveQueue) end(outs ...download.Outcome) {
	q.mu.Lock()
	q.outs = append(q.outs, outs...)
	q.mu.Unlock()
	q.ready <- struct{}{}
}

func (q *liveQueue) taken() (int, []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.takes, slices.Clone(q.firsts)
}

// livePlayer records what settling asks of the player, safely.
type livePlayer struct {
	mu                sync.Mutex
	resolved, dropped []string
}

func (p *livePlayer) Resolve(id, path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolved = append(p.resolved, id+" "+path)
}

func (p *livePlayer) Drop(id string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dropped = append(p.dropped, id)
	return 1
}

func (p *livePlayer) calls() ([]string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.resolved), slices.Clone(p.dropped)
}

// following starts followDownloads on an app with no window, and returns a
// channel closed when it ends.
func following(q *liveQueue, p *livePlayer) (*App, chan struct{}) {
	a := &App{closed: true, stop: make(chan struct{}), waits: make(chan player.Track, 1)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.followDownloads(q, p)
	}()
	return a, done
}

func ended(t *testing.T, done chan struct{}, how string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("followDownloads didn't end on %s", how)
	}
}

// Each Ready settles what ended once, and tells poll what to show: songs to
// load into the library, and songs dropped for failing, but not those
// removed on the Downloads tab, which says so itself.
func TestFollowingDownloads(t *testing.T) {
	q, p := newLiveQueue(), &livePlayer{}
	a, done := following(q, p)
	q.end(download.Outcome{VideoID: "v1", Path: "/m/v1.opus"})
	eventually(t, "v1 resolved", func() bool { r, _ := p.calls(); return len(r) == 1 })
	eventually(t, "the frame marked", a.downloaded.Load)
	if n, _ := q.taken(); n != 1 || !a.landed.Load() || a.skipped.Load() {
		t.Fatalf("%d takes, landed %v, skipped %v", n, a.landed.Load(), a.skipped.Load())
	}
	a.landed.Store(false)
	q.end(download.Outcome{VideoID: "v3", Removed: true})
	eventually(t, "v3 dropped", func() bool { _, d := p.calls(); return slices.Equal(d, []string{"v3"}) })
	if n, _ := q.taken(); n != 2 || a.skipped.Load() || a.landed.Load() {
		t.Fatalf("%d takes; a removed song: skipped %v, landed %v", n, a.skipped.Load(), a.landed.Load())
	}
	q.end(download.Outcome{VideoID: "v2", Failure: download.Network})
	eventually(t, "v2 dropped", func() bool { _, d := p.calls(); return slices.Equal(d, []string{"v3", "v2"}) })
	eventually(t, "the toast asked for", a.skipped.Load)
	if n, _ := q.taken(); n != 3 {
		t.Fatalf("%d takes", n)
	}
	close(a.stop)
	ended(t, done, "stop")

	q2 := newLiveQueue()
	_, done2 := following(q2, p)
	close(q2.ready)
	ended(t, done2, "a closed Ready")
}

// The newest track playback waits on is the one asked for first; one it
// stopped waiting on before followDownloads took it is not.
func TestAskFirstKeepsTheNewest(t *testing.T) {
	q := newLiveQueue()
	a := &App{closed: true, stop: make(chan struct{}), waits: make(chan player.Track, 1)}
	a.askFirst(player.Track{VideoID: "v1"})
	a.askFirst(player.Track{VideoID: "v2"})
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.followDownloads(q, &livePlayer{})
	}()
	eventually(t, "v2 asked for", func() bool { _, f := q.taken(); return len(f) == 1 })
	a.askFirst(player.Track{VideoID: "v3"})
	eventually(t, "v3 asked for", func() bool { _, f := q.taken(); return len(f) == 2 })
	if _, f := q.taken(); !slices.Equal(f, []string{"v2", "v3"}) {
		t.Fatalf("asked for %q", f)
	}
	close(a.stop)
	ended(t, done, "stop")
}

// Songs landing while the queue is busy reload the library at most once in
// reloadEvery; one due when the queue goes idle reloads at once.
func TestLandingsReloadTheLibrarySparingly(t *testing.T) {
	old := reloadEvery
	reloadEvery = 2 * time.Second
	q, p := newLiveQueue(), &livePlayer{}
	a, done := following(q, p)
	defer func() {
		close(a.stop)
		<-done
		reloadEvery = old
	}()
	q.setBusy(true)
	q.end(download.Outcome{VideoID: "v1", Path: "/m/v1.opus"})
	eventually(t, "the first reload", a.landed.Load)
	a.landed.Store(false)
	q.end(download.Outcome{VideoID: "v2", Path: "/m/v2.opus"})
	eventually(t, "v2 resolved", func() bool { r, _ := p.calls(); return len(r) == 2 })
	time.Sleep(50 * time.Millisecond)
	if a.landed.Load() {
		t.Fatal("a second reload within reloadEvery")
	}
	start := time.Now()
	q.setBusy(false)
	q.end() // the batch is over
	eventually(t, "the reload due", a.landed.Load)
	if took := time.Since(start); took >= reloadEvery/2 {
		t.Fatalf("the reload due came after %v", took)
	}
	a.landed.Store(false)
	q.end(download.Outcome{VideoID: "v3", Path: "/m/v3.opus"})
	eventually(t, "a reload for a song landing on an idle queue", a.landed.Load)
}
