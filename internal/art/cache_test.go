package art

import (
	"context"
	"database/sql"
	"image"
	"image/color"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/netx"
	"github.com/ujjwal313/boombrick/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "art.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st.DB
}

// albumOn makes root/artist/title/01 a.mp3, with a 300 px cover.png in the
// colour given, or none for nil.
func albumOn(t *testing.T, root, artist, title string, cover *color.NRGBA) media.Cover {
	t.Helper()
	dir := filepath.Join(root, artist, title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	track := filepath.Join(dir, "01 a.mp3")
	os.WriteFile(track, []byte("not audio"), 0o644)
	if cover != nil {
		os.WriteFile(filepath.Join(dir, "cover.png"), encodePNG(t, solid(300, 300, *cover)), 0o644)
	}
	return media.Cover{Artist: artist, Album: title, Path: track}
}

type fixture struct {
	db   *sql.DB
	dir  string
	root string
}

func newFixture(t *testing.T) fixture {
	return fixture{db: openDB(t), dir: filepath.Join(t.TempDir(), "art"), root: t.TempDir()}
}

func (f fixture) cache(t *testing.T, find Finder, online bool) *Cache {
	t.Helper()
	c := New(Options{DB: f.db, Dir: f.dir, Lookup: find, Online: func() bool { return online }})
	t.Cleanup(c.Close)
	return c
}

// waitCover asks for cv at px until the cache hands it over.
func waitCover(t *testing.T, c *Cache, cv media.Cover, px int) *image.NRGBA {
	t.Helper()
	return waitArt(t, c, cv, px, false)
}

// waitArt is waitCover; with online set, "no art" answers are passed over
// while a search runs.
func waitArt(t *testing.T, c *Cache, cv media.Cover, px int, online bool) *image.NRGBA {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		img, none := c.Cover(cv, px)
		if img != nil {
			return img
		}
		if none && !online {
			t.Fatalf("no art for %s", cv.Album)
		}
		select {
		case <-c.Ready():
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for the art of %s", cv.Album)
	return nil
}

// waitNone asks for cv until the cache says it has no art.
func waitNone(t *testing.T, c *Cache, cv media.Cover) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if img, none := c.Cover(cv, 100); none {
			return
		} else if img != nil {
			t.Fatalf("art for %s, want none", cv.Album)
		}
		select {
		case <-c.Ready():
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting to hear %s has no art", cv.Album)
}

// cached lists the files under dir.
func cached(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	return files
}

func TestCoverFromAFolderImage(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := f.cache(t, nil, false)
	img := waitCover(t, c, cv, 100)
	if img.Rect.Dx() != 100 || !near(img.NRGBAAt(50, 50), red, 6) {
		t.Fatalf("cover %v, centre %v", img.Rect, img.NRGBAAt(50, 50))
	}
	hue, _, colour, ok := c.Colour(cv)
	want, _, _ := Colour(solid(48, 48, red))
	if !ok || !colour || hue < want-3 || hue > want+3 {
		t.Fatalf("Colour = %v, %v, %v; want about %v", hue, colour, ok, want)
	}
	if n := len(cached(t, f.dir)); n != 3 {
		t.Fatalf("%d cached files, want the master, the 100 px copy and the backdrop: %v", n, cached(t, f.dir))
	}
}

// apicMP3 is an ID3v2.3 tag with pic as its front cover.
func apicMP3(pic []byte) []byte {
	body := append([]byte("\x00image/jpeg\x00\x03\x00"), pic...)
	n := len(body)
	frame := append([]byte("APIC"), byte(n>>24), byte(n>>16), byte(n>>8), byte(n), 0, 0)
	frame = append(frame, body...)
	n = len(frame)
	tag := append([]byte("ID3\x03\x00\x00"), byte(n>>21&0x7f), byte(n>>14&0x7f), byte(n>>7&0x7f), byte(n&0x7f))
	return append(tag, frame...)
}

func TestBrokenEmbeddedPictureGivesWayToTheFolders(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	os.WriteFile(cv.Path, apicMP3([]byte("not a picture")), 0o644)
	if _, ok := Embedded(cv.Path); !ok {
		t.Fatal("the track's picture isn't found")
	}
	c := f.cache(t, nil, false)
	if img := waitCover(t, c, cv, 100); !near(img.NRGBAAt(50, 50), red, 6) {
		t.Fatalf("centre %v, want the folder's red", img.NRGBAAt(50, 50))
	}
}

func TestCoverIsHandedOverOnceAndReadAgain(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := f.cache(t, nil, false)
	waitCover(t, c, cv, 80)
	if img, none := c.Cover(cv, 80); img != nil || none {
		t.Fatalf("asked again at once: %v, %v; want nothing yet", img, none)
	}
	if img := waitCover(t, c, cv, 80); img.Rect.Dx() != 80 {
		t.Fatalf("read again at %v", img.Rect)
	}
}

func TestCacheBackdrop(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := f.cache(t, nil, false)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if img, _ := c.Backdrop(cv); img != nil {
			if img.Rect.Dx() != backdropPx {
				t.Fatalf("backdrop %v", img.Rect)
			}
			return
		}
	}
	t.Fatal("no backdrop")
}

func TestAlbumWithoutArt(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", nil)
	c := f.cache(t, nil, false)
	waitNone(t, c, cv)
	if _, _, colour, ok := c.Colour(cv); !ok || colour {
		t.Fatalf("Colour of an album without art: colour %v, known %v", colour, ok)
	}
	if img, none := c.Cover(media.Cover{}, 100); img != nil || !none {
		t.Fatal("a cover with no album has art")
	}
}

func TestArtIsReusedNextLaunch(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := New(Options{DB: f.db, Dir: f.dir})
	waitCover(t, c, cv, 100)
	c.Close()
	before := cached(t, f.dir)
	info, _ := os.Stat(before[0])

	c2 := f.cache(t, nil, false)
	waitCover(t, c2, cv, 100)
	after := cached(t, f.dir)
	if strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("files changed on the second launch: %v, then %v", before, after)
	}
	if again, _ := os.Stat(before[0]); !again.ModTime().Equal(info.ModTime()) {
		t.Fatal("the art was made again on the second launch")
	}
}

func TestReplacedCoverIsReadAgain(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := New(Options{DB: f.db, Dir: f.dir})
	waitCover(t, c, cv, 100)
	c.Close()

	path := filepath.Join(filepath.Dir(cv.Path), "cover.png")
	os.WriteFile(path, encodePNG(t, solid(300, 300, blue)), 0o644)
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(path, later, later)

	c2 := f.cache(t, nil, false)
	if img := waitCover(t, c2, cv, 100); !near(img.NRGBAAt(50, 50), blue, 6) {
		t.Fatalf("after replacing the cover: %v", img.NRGBAAt(50, 50))
	}
	if n := len(cached(t, f.dir)); n != 3 {
		t.Fatalf("%d cached files; the old art's should be gone: %v", n, cached(t, f.dir))
	}
}

type fakeFinder struct {
	mu    sync.Mutex
	calls int
	data  []byte
	err   error
}

func (f *fakeFinder) Find(ctx context.Context, artist, album string) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.data, "rg-1", f.err
}

func (f *fakeFinder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestArtFoundOnline(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", nil)
	find := &fakeFinder{data: encodeJPEG(t, solid(500, 500, green))}
	c := New(Options{DB: f.db, Dir: f.dir, Lookup: find, Online: func() bool { return true }})
	if img := waitArt(t, c, cv, 100, true); !near(img.NRGBAAt(50, 50), green, 8) {
		t.Fatalf("online cover %v", img.NRGBAAt(50, 50))
	}
	c.Close()
	var source string
	f.db.QueryRow(`SELECT source FROM art`).Scan(&source)
	if source != "musicbrainz:rg-1" {
		t.Fatalf("source %q", source)
	}
	c2 := f.cache(t, find, true)
	waitCover(t, c2, cv, 100)
	if n := find.count(); n != 1 {
		t.Fatalf("searched %d times; the second launch should use what was found", n)
	}
}

func TestNothingFoundOnlineIsRemembered(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", nil)
	find := &fakeFinder{err: ErrNotFound}
	c := New(Options{DB: f.db, Dir: f.dir, Lookup: find, Online: func() bool { return true }})
	waitNone(t, c, cv)
	var looked int64
	for deadline := time.Now().Add(2 * time.Second); looked == 0 && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		f.db.QueryRow(`SELECT looked_up FROM art`).Scan(&looked)
	}
	if looked == 0 {
		t.Fatal("the search that found nothing wasn't recorded")
	}
	c.Close()
	c2 := f.cache(t, find, true)
	waitNone(t, c2, cv)
	time.Sleep(100 * time.Millisecond)
	if n := find.count(); n != 1 {
		t.Fatalf("searched %d times; a search that found nothing waits 30 days", n)
	}
}

func TestNoSearchOfflineOrForUnknownAlbums(t *testing.T) {
	f := newFixture(t)
	find := &fakeFinder{err: ErrNotFound}
	offline := f.cache(t, find, false)
	waitNone(t, offline, albumOn(t, f.root, "Odalys", "Copper Season", nil))

	online := f.cache(t, find, true)
	waitNone(t, online, albumOn(t, f.root, library.UnknownArtist, "Tapes", nil))
	waitNone(t, online, albumOn(t, f.root, "Odalys", library.UnknownAlbum, nil))
	time.Sleep(100 * time.Millisecond)
	if n := find.count(); n != 0 {
		t.Fatalf("searched %d times", n)
	}
}

func TestNewestJobFirstAndOldestDropped(t *testing.T) {
	f := newFixture(t)
	c := newCache(Options{DB: f.db, Dir: f.dir}, 0) // no workers: the queue stays put
	t.Cleanup(c.Close)
	var covers []media.Cover
	for i := range maxJobs + 1 {
		cv := media.Cover{Artist: "A", Album: string(rune('a'+i%26)) + strings.Repeat("x", i/26), Path: "/m/x.mp3"}
		covers = append(covers, cv)
		c.Cover(cv, 100)
	}
	if len(c.jobs) != maxJobs {
		t.Fatalf("%d jobs waiting, want %d", len(c.jobs), maxJobs)
	}
	if a := c.albums[keyOf(covers[0])]; a.state != unknown {
		t.Fatalf("the dropped album's state is %v; it should be asked for again", a.state)
	}
	if j, cv, ok := c.next(); !ok || j.kind != resolveJob || cv != covers[maxJobs] {
		t.Fatalf("next job %+v for %v, want the newest", j, cv)
	}
}

// logBuffer collects log output from the cache's goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) count(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), s)
}

func (b *logBuffer) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(b, nil))
}

// until polls cond for up to 5 s.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestFailedWriteIsNotNoArt(t *testing.T) {
	f := newFixture(t)
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("x"), 0o644)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	find := &fakeFinder{err: ErrNotFound}
	c := New(Options{DB: f.db, Dir: filepath.Join(blocker, "art"), Lookup: find, Online: func() bool { return true }})
	waitNone(t, c, cv)
	time.Sleep(100 * time.Millisecond)
	if n := find.count(); n != 0 {
		t.Fatalf("searched %d times for an album with its own cover", n)
	}
	var rows int
	f.db.QueryRow(`SELECT count(*) FROM art`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d art rows after a failed write", rows)
	}
	c.Close()

	os.Remove(blocker)
	c2 := New(Options{DB: f.db, Dir: filepath.Join(blocker, "art")})
	defer c2.Close()
	if img := waitCover(t, c2, cv, 100); !near(img.NRGBAAt(50, 50), red, 6) {
		t.Fatalf("cover after the folder became writable: %v", img.NRGBAAt(50, 50))
	}
}

func TestUnreadableCopiesAreRebuilt(t *testing.T) {
	for _, damage := range []string{"backdrop", "master"} {
		t.Run(damage, func(t *testing.T) {
			f := newFixture(t)
			cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
			var logs logBuffer
			c := New(Options{DB: f.db, Dir: f.dir, Log: logs.logger()})
			t.Cleanup(c.Close)
			waitCover(t, c, cv, 100)

			var ask func() *image.NRGBA
			if damage == "backdrop" {
				for _, p := range cached(t, f.dir) {
					if strings.HasSuffix(p, "-bg.png") {
						os.WriteFile(p, []byte("torn"), 0o644)
					}
				}
				ask = func() *image.NRGBA { img, _ := c.Backdrop(cv); return img }
			} else {
				for _, p := range cached(t, f.dir) {
					if strings.HasSuffix(p, ".jpg") && !strings.HasSuffix(p, "-100.jpg") {
						os.WriteFile(p, []byte("torn"), 0o644)
					}
				}
				ask = func() *image.NRGBA { img, _ := c.Cover(cv, 60); return img }
			}
			var img *image.NRGBA
			for polls := 0; img == nil && polls < 500; polls++ {
				if img = ask(); img == nil {
					select {
					case <-c.Ready():
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
			if img == nil {
				t.Fatal("the art was never rebuilt")
			}
			if n := logs.count("cached art"); n != 1 {
				t.Fatalf("%d warnings about the cached art, want 1", n)
			}
		})
	}
}

// gatedFinder holds its first search until released, then answers 503.
type gatedFinder struct {
	mu    sync.Mutex
	calls int
	first chan struct{}
	gate  chan struct{}
}

func (g *gatedFinder) Find(ctx context.Context, artist, album string) ([]byte, string, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.mu.Unlock()
	if n == 1 {
		close(g.first)
		<-g.gate
		return nil, "", &netx.StatusError{Code: 503, RetryAfter: 30 * time.Second}
	}
	return nil, "", ErrNotFound
}

func TestBusyAnswerHoldsBackQueuedSearches(t *testing.T) {
	f := newFixture(t)
	g := &gatedFinder{first: make(chan struct{}), gate: make(chan struct{})}
	c := New(Options{DB: f.db, Dir: f.dir, Lookup: g, Online: func() bool { return true }})
	t.Cleanup(c.Close)
	for _, title := range []string{"One", "Two", "Three"} {
		c.Cover(albumOn(t, f.root, "Odalys", title, nil), 100)
	}
	<-g.first
	until(t, "the other searches to queue", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.searches) == 2
	})
	close(g.gate)
	until(t, "the queue to be dropped", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.searches) == 0
	})
	time.Sleep(50 * time.Millisecond)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.calls != 1 {
		t.Fatalf("%d searches; the ones queued behind a 503 should wait", g.calls)
	}
}

func TestVanishedOnlineArtIsSearchedAgain(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", nil)
	find := &fakeFinder{data: encodeJPEG(t, solid(500, 500, green))}
	c := New(Options{DB: f.db, Dir: f.dir, Lookup: find, Online: func() bool { return true }})
	waitArt(t, c, cv, 100, true)
	c.Close()
	for _, p := range cached(t, f.dir) {
		os.Remove(p)
	}
	c2 := f.cache(t, find, true)
	waitArt(t, c2, cv, 100, true)
	if n := find.count(); n != 2 {
		t.Fatalf("searched %d times; art that went missing should be found again", n)
	}
}

func TestCopiesAreKeptInSubfolders(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := f.cache(t, nil, false)
	waitCover(t, c, cv, 100)
	var file string
	f.db.QueryRow(`SELECT file FROM art`).Scan(&file)
	sub, base, ok := strings.Cut(file, "/")
	if !ok || len(sub) != 2 || !strings.HasPrefix(base, sub) {
		t.Fatalf("file %q; want it in a folder named by its first two characters", file)
	}
	for _, p := range cached(t, f.dir) {
		if filepath.Dir(p) != filepath.Join(f.dir, sub) {
			t.Errorf("%s is outside %s", p, sub)
		}
	}
}

func TestStaleTempFilesAreSwept(t *testing.T) {
	f := newFixture(t)
	os.MkdirAll(filepath.Join(f.dir, "ab"), 0o755)
	stale := []string{filepath.Join(f.dir, "abc-def.jpg.tmp"), filepath.Join(f.dir, "ab", "abd-def.jpg.k2x9.tmp")}
	writing := filepath.Join(f.dir, "ab", "abe-def.jpg."+runTag+".tmp") // this run's, being written
	for _, p := range append(stale, writing) {
		os.WriteFile(p, []byte("half"), 0o644)
	}
	// One worker, which sweeps before it takes its first job.
	c := newCache(Options{DB: f.db, Dir: f.dir}, 1)
	t.Cleanup(c.Close)
	waitCover(t, c, albumOn(t, f.root, "Odalys", "Copper Season", &red), 100)
	for _, p := range stale {
		if exists(p) {
			t.Errorf("%s survived the start", p)
		}
	}
	if !exists(writing) {
		t.Error("a temporary file of this run's was swept")
	}
}

// Quitting doesn't wait on pictures nobody will see: work waiting for its
// turn to decode gives up once the cache is closing.
func TestCloseSkipsWaitingDecodes(t *testing.T) {
	f := newFixture(t)
	c := New(Options{DB: f.db, Dir: f.dir})
	c.heavy.Lock()
	for _, title := range []string{"One", "Two"} {
		c.Cover(albumOn(t, f.root, "Odalys", title, &red), 100)
	}
	until(t, "the workers to take the jobs", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.jobs) == 0
	})
	closeAndRelease(t, c)
	if files := cached(t, f.dir); len(files) != 0 {
		t.Fatalf("art made while closing: %v", files)
	}
}

func TestCloseSkipsDecodingWhatWasFound(t *testing.T) {
	f := newFixture(t)
	find := &heldFinder{called: make(chan struct{}), release: make(chan struct{}), data: encodeJPEG(t, solid(500, 500, green))}
	c := New(Options{DB: f.db, Dir: f.dir, Lookup: find, Online: func() bool { return true }})
	cv := albumOn(t, f.root, "Odalys", "Copper Season", nil)
	c.Cover(cv, 100)
	<-find.called
	c.heavy.Lock()
	close(find.release)
	until(t, "the search to come back", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.albums[keyOf(cv)].lookedUp != 0
	})
	time.Sleep(50 * time.Millisecond) // and wait for its turn to decode
	closeAndRelease(t, c)
	if files := cached(t, f.dir); len(files) != 0 {
		t.Fatalf("art made while closing: %v", files)
	}
}

// closeAndRelease closes c while the test holds c.heavy, letting go of it
// once the cache is closing.
func closeAndRelease(t *testing.T, c *Cache) {
	t.Helper()
	closed := make(chan struct{})
	go func() {
		c.Close()
		close(closed)
	}()
	until(t, "the cache to start closing", func() bool { return c.ctx.Err() != nil })
	c.heavy.Unlock()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close didn't return")
	}
}

// heldFinder holds its search until released, then finds data.
type heldFinder struct {
	called, release chan struct{}
	data            []byte
}

func (h *heldFinder) Find(ctx context.Context, artist, album string) ([]byte, string, error) {
	close(h.called)
	<-h.release
	return h.data, "rg-1", nil
}

func TestPanicLeavesTheAlbumWithoutArt(t *testing.T) {
	f := newFixture(t)
	var logs logBuffer
	// Without a database, reading an album's record panics.
	c := New(Options{Dir: f.dir, Log: logs.logger()})
	t.Cleanup(c.Close)
	for _, title := range []string{"One", "Two"} {
		waitNone(t, c, albumOn(t, f.root, "Odalys", title, &red))
	}
	if n := logs.count("album art panicked"); n != 2 {
		t.Fatalf("%d panics logged, want 2", n)
	}
	if n := logs.count("debug.Stack"); n != 2 {
		t.Fatalf("%d stacks logged, want 2", n)
	}
}

// panicFinder panics when asked for the album "Damaged" and finds nothing
// for any other.
type panicFinder struct{ fakeFinder }

func (p *panicFinder) Find(ctx context.Context, artist, album string) ([]byte, string, error) {
	p.fakeFinder.Find(ctx, artist, album)
	if album == "Damaged" {
		panic("damaged download")
	}
	return nil, "", ErrNotFound
}

// A search that panicked is not one that found nothing: it is tried again
// after a while, not in a month.
func TestSearchPanicIsNotNothingFound(t *testing.T) {
	f := newFixture(t)
	var logs logBuffer
	find := &panicFinder{}
	c := New(Options{DB: f.db, Dir: f.dir, Lookup: find, Online: func() bool { return true }, Log: logs.logger()})
	t.Cleanup(c.Close)
	damaged := albumOn(t, f.root, "Odalys", "Damaged", nil)
	waitNone(t, c, damaged)
	until(t, "the panic", func() bool { return logs.count("art search panicked") == 1 })
	until(t, "the search to be forgotten", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		a := c.albums[keyOf(damaged)]
		return !a.searching && a.lookedUp == 0 && !a.failed.IsZero()
	})
	var looked int64
	f.db.QueryRow(`SELECT looked_up FROM art`).Scan(&looked)
	if looked != 0 {
		t.Fatalf("looked_up %d after a panic; want none", looked)
	}

	// The searcher carries on, and doesn't try the damaged album again at once.
	waitNone(t, c, albumOn(t, f.root, "Odalys", "Plain", nil))
	until(t, "the next search", func() bool { return find.count() == 2 })
	c.Cover(damaged, 100)
	time.Sleep(50 * time.Millisecond)
	if n := find.count(); n != 2 {
		t.Fatalf("%d searches; the damaged album was searched again at once", n)
	}
}

// No screen shows more than a dozen covers, so pictures the UI hasn't taken
// are kept to 16 and a fast scroll doesn't pile up decoded art.
func TestUntakenPicturesAreBounded(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := newCache(Options{DB: f.db, Dir: f.dir}, 1)
	t.Cleanup(c.Close)
	waitCover(t, c, cv, 100)
	for px := 20; px < 40; px++ {
		c.size(keyOf(cv), px)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := len(c.done); n != 16 {
		t.Fatalf("%d pictures waiting, want 16", n)
	}
}

func TestReadyEndsWithTheCache(t *testing.T) {
	f := newFixture(t)
	c := New(Options{DB: f.db, Dir: f.dir})
	waitCover(t, c, albumOn(t, f.root, "Odalys", "Copper Season", &red), 100)
	c.Close()
	ended := make(chan struct{})
	go func() {
		for range c.Ready() {
		}
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("Ready is still open after Close")
	}
}

func TestAskingAboutKnownAlbumsAllocatesNothing(t *testing.T) {
	f := newFixture(t)
	c := newCache(Options{DB: f.db, Dir: f.dir}, 0)
	t.Cleanup(c.Close)
	have := media.Cover{Artist: "A", Album: "Have", Path: "/m/a.mp3"}
	none := media.Cover{Artist: "A", Album: "None", Path: "/m/b.mp3"}
	c.albums[keyOf(have)] = &album{cover: have, state: found, colour: true, wanted: map[int]bool{100: true}}
	c.albums[keyOf(none)] = &album{cover: none, state: missing, wanted: map[int]bool{}}
	if n := testing.AllocsPerRun(100, func() {
		c.Cover(have, 100)
		c.Colour(have)
		c.Cover(none, 100)
		c.Colour(none)
	}); n != 0 {
		t.Fatalf("%v allocations per ask", n)
	}
}

// brokenFolder makes the cache folder impossible to create, by putting a file
// where it should be. mend puts it right.
func brokenFolder(t *testing.T, dir string) (mend func()) {
	t.Helper()
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() {
		os.Remove(dir)
		os.MkdirAll(dir, 0o755)
	}
}

// forgetFailure makes the cache's last failure with cv look old.
func forgetFailure(c *Cache, cv media.Cover) {
	c.mu.Lock()
	c.albums[keyOf(cv)].failed = time.Now().Add(-time.Hour)
	c.mu.Unlock()
}

func TestBrokenFolderIsRetriedNotSearched(t *testing.T) {
	f := newFixture(t)
	brokenFolder(t, f.dir)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	find := &fakeFinder{err: ErrNotFound}
	c := f.cache(t, find, true)
	waitNone(t, c, cv)

	forgetFailure(c, cv)
	if img, none := c.Cover(cv, 100); img != nil || none {
		t.Fatalf("asked after the failure was forgotten: %v, %v; want it looked into again", img, none)
	}
	waitNone(t, c, cv)
	time.Sleep(100 * time.Millisecond)
	if n := find.count(); n != 0 {
		t.Fatalf("searched %d times for an album with its own cover", n)
	}
}

func TestBrokenFolderMendedInTheSameSession(t *testing.T) {
	f := newFixture(t)
	mend := brokenFolder(t, f.dir)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	find := &fakeFinder{data: encodeJPEG(t, solid(500, 500, green))}
	c := f.cache(t, find, true)
	waitNone(t, c, cv)

	mend()
	forgetFailure(c, cv)
	if img := waitArt(t, c, cv, 100, true); !near(img.NRGBAAt(50, 50), red, 6) {
		t.Fatalf("cover after the folder was mended: %v", img.NRGBAAt(50, 50))
	}
	if n := find.count(); n != 0 {
		t.Fatalf("searched %d times", n)
	}
}
