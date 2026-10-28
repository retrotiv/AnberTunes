package art

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"iter"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/netx"
)

const (
	masterPx       = 512                 // the largest copy kept; every size is made from it
	maxJobs        = 256                 // work waiting beyond this drops the oldest
	maxDone        = 16                  // finished pictures the UI hasn't taken; no screen shows more than a dozen
	maxSearches    = 64                  // albums waiting for an online search
	maxSourceBytes = 32 << 20            // a cover file larger than this is skipped
	retryNotFound  = 30 * 24 * time.Hour // a search that found nothing is tried again after this
	retryFailed    = 10 * time.Minute    // and one that failed after this
	onlineSource   = "musicbrainz:"      // the source of art found online, before its release group
)

// Finder searches for an album's cover online. *Lookup is one.
type Finder interface {
	Find(ctx context.Context, artist, album string) (data []byte, id string, err error)
}

// Options configure a Cache.
type Options struct {
	DB     *sql.DB
	Dir    string      // where the copies are kept
	Lookup Finder      // nil: no searching online
	Online func() bool // whether the network is up; nil means it never is
	Log    *slog.Logger
}

// Cache prepares album art for the screens. It finds each album's picture,
// scales it once to the sizes asked for and keeps the copies on disk with the
// album's colour and the Lyrics backdrop, recording it all in the art table.
// The screens ask from the UI thread and never wait: what isn't ready is
// worked on in the background, newest request first, and Ready receives a
// value when something is.
type Cache struct {
	opts   Options
	ready  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	heavy  sync.Mutex // one picture decoded at full size at a time, to bound memory

	mu        sync.Mutex
	work      *sync.Cond // signalled when jobs arrive or the cache closes
	albums    map[albumKey]*album
	jobs      []job         // newest last; workers take the newest first
	done      []picture     // finished and not yet taken, oldest first
	searches  []albumKey    // albums waiting for an online search, newest last
	searching chan struct{} // wakes the searcher
	busyUntil time.Time     // no searches before this: offline, or MusicBrainz asked us to wait
	closed    bool
}

// albumKey names an album in memory. Unlike the joined string in the database
// it can be built from a Cover without allocating, which matters to the UI
// thread asking every frame.
type albumKey struct{ artist, album string }

func keyOf(cv media.Cover) albumKey { return albumKey{cv.Artist, cv.Album} }

// db is the key the art table knows the album by.
func (k albumKey) db() string { return media.Cover{Artist: k.artist, Album: k.album}.Key() }

// errWrite marks a failure to write the cache's files, which says nothing
// about the picture being processed.
var errWrite = errors.New("writing cached art")

type state uint8

const (
	unknown   state = iota // not looked at since launch
	resolving              // being looked for
	found                  // has art in file
	missing                // has none
)

// album is what the cache knows of one album since launch.
type album struct {
	cover       media.Cover
	state       state
	file        string
	hue, chroma float64
	colour      bool
	wanted      map[int]bool // sizes asked for and not yet taken; 0 is the backdrop
	lookedUp    int64        // the last online search, Unix ms
	searching   bool
	failed      time.Time // when the last online search failed, or its copies could not be written
	broken      bool      // its copies could not be written; not searched for, looked at again after retryFailed
}

type jobKind uint8

const (
	resolveJob jobKind = iota // find the album's art
	sizeJob                   // read or make a size
)

type job struct {
	key  albumKey
	kind jobKind
	px   int
}

type picture struct {
	key albumKey
	px  int
	img *image.NRGBA
}

// record is an album's row in the art table.
type record struct {
	key, file, source, track          string
	trackMtime, dirMtime, sourceMtime int64
	hue, chroma                       float64
	colour                            bool
	lookedUp                          int64
}

// New starts a cache with two workers, and a searcher when opts.Lookup is set.
func New(opts Options) *Cache { return newCache(opts, 2) }

func newCache(opts Options, workers int) *Cache {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	if opts.Online == nil {
		opts.Online = func() bool { return false }
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Cache{opts: opts, ready: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
		albums: map[albumKey]*album{}, searching: make(chan struct{}, 1)}
	c.work = sync.NewCond(&c.mu)
	for i := range workers {
		c.wg.Add(1)
		go c.worker(i == 0)
	}
	if opts.Lookup != nil {
		c.wg.Add(1)
		go c.searcher()
	}
	return c
}

// Close stops the workers and the searcher, waits for them and closes Ready.
// Work waiting for its turn to decode is dropped.
func (c *Cache) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.work.Broadcast()
	c.wg.Wait()
	close(c.ready) // only the workers and the searcher send on it
}

// Ready receives a value when something asked for has been worked out: a
// picture, or what an album's art is. It is closed by Close.
func (c *Cache) Ready() <-chan struct{} { return c.ready }

func (c *Cache) notify() {
	select {
	case c.ready <- struct{}{}:
	default:
	}
}

// Cover returns cv's art px pixels square, once, when it is ready; until then
// it returns nil and works on it. none is true when there is no art as far as
// is known; an album being searched for online may still turn some up, so
// screens keep asking, as they do each frame they draw it, and Ready receives
// a value when it arrives. Art smaller than px comes at its own size, for the
// screen to scale.
func (c *Cache) Cover(cv media.Cover, px int) (img *image.NRGBA, none bool) {
	if px <= 0 {
		return nil, true
	}
	return c.take(cv, px)
}

// Backdrop returns cv's Lyrics backdrop, as Cover returns its art.
func (c *Cache) Backdrop(cv media.Cover) (img *image.NRGBA, none bool) { return c.take(cv, 0) }

// Colour reports the dominant hue and chroma of cv's art; colour is false when
// it is greyscale or there is none. ok is false until that is known, and
// asking starts finding out.
func (c *Cache) Colour(cv media.Cover) (hue, chroma float64, colour, ok bool) {
	if cv.Album == "" {
		return 0, 0, false, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.albumLocked(cv)
	switch a.state {
	case found:
		return a.hue, a.chroma, a.colour, true
	case missing:
		if !c.retryBrokenLocked(a) {
			return 0, 0, false, true
		}
	case unknown:
		c.resolveLocked(a)
	}
	return 0, 0, false, false
}

func (c *Cache) take(cv media.Cover, px int) (*image.NRGBA, bool) {
	if cv.Album == "" {
		return nil, true
	}
	key := keyOf(cv)
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.albumLocked(cv)
	switch a.state {
	case missing:
		if !c.retryBrokenLocked(a) {
			c.searchLocked(key, a)
			return nil, true
		}
	case unknown:
		c.resolveLocked(a)
	}
	for i, p := range c.done {
		if p.key == key && p.px == px {
			c.done = slices.Delete(c.done, i, i+1)
			delete(a.wanted, px)
			return p.img, false
		}
	}
	if !a.wanted[px] {
		a.wanted[px] = true
		if a.state == found {
			c.pushLocked(job{key: key, kind: sizeJob, px: px})
		}
		// Otherwise the size is made once the album is resolved.
	}
	return nil, false
}

func (c *Cache) albumLocked(cv media.Cover) *album {
	key := keyOf(cv)
	a := c.albums[key]
	if a == nil {
		a = &album{cover: cv, wanted: map[int]bool{}}
		c.albums[key] = a
	} else if a.cover.Path == "" {
		a.cover.Path = cv.Path
	}
	return a
}

// retryBrokenLocked looks into a again if its copies could not be written and
// it has been a while, in case the disk is better. It reports whether it did.
func (c *Cache) retryBrokenLocked(a *album) bool {
	if !a.broken || time.Since(a.failed) < retryFailed {
		return false
	}
	c.resolveLocked(a)
	return true
}

func (c *Cache) resolveLocked(a *album) {
	a.state, a.broken = resolving, false
	c.pushLocked(job{key: keyOf(a.cover), kind: resolveJob})
}

// pushLocked queues j ahead of everything waiting: what was asked for last is
// what is on screen now. Past maxJobs the oldest job is dropped; its album or
// size is asked for again when it is next drawn.
func (c *Cache) pushLocked(j job) {
	c.jobs = append(c.jobs, j)
	if len(c.jobs) > maxJobs {
		old := c.jobs[0]
		c.jobs = slices.Delete(c.jobs, 0, 1)
		if a := c.albums[old.key]; a != nil {
			if old.kind == resolveJob {
				a.state = unknown
			} else {
				delete(a.wanted, old.px)
			}
		}
	}
	c.work.Signal()
}

// next waits for a job and takes the newest, with its album's cover; ok is
// false once the cache is closed.
func (c *Cache) next() (job, media.Cover, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.jobs) == 0 && !c.closed {
		c.work.Wait()
	}
	if c.closed {
		return job{}, media.Cover{}, false
	}
	j := c.jobs[len(c.jobs)-1]
	c.jobs = c.jobs[:len(c.jobs)-1]
	return j, c.albums[j.key].cover, true
}

// worker does jobs until the cache closes. The first worker clears away
// temporary files left by a run that was cut short before it takes any.
func (c *Cache) worker(first bool) {
	defer c.wg.Done()
	if first {
		sweep(c.opts.Dir)
	}
	for {
		j, cv, ok := c.next()
		if !ok {
			return
		}
		c.do(j, cv)
	}
}

// do does one job. A panic, which a decoder given a damaged picture may
// raise, is logged and leaves the album without art, as a picture that can't
// be decoded does, until it is next resolved.
func (c *Cache) do(j job, cv media.Cover) {
	defer func() {
		if p := recover(); p != nil {
			c.opts.Log.Error("album art panicked", "album", cv.Album, "panic", p, "stack", string(debug.Stack()))
			c.mu.Lock()
			a := c.albums[j.key]
			a.state, a.file = missing, ""
			clear(a.wanted)
			c.mu.Unlock()
			c.notify()
		}
	}()
	if j.kind == resolveJob {
		c.resolve(cv)
	} else {
		c.size(j.key, j.px)
	}
}

// decoding runs f holding c.heavy, so that one picture at a time is decoded at
// full size. It returns false without running f if the cache closed while it
// waited: quitting needn't wait on pictures nobody will see.
func (c *Cache) decoding(f func()) bool {
	c.heavy.Lock()
	defer c.heavy.Unlock()
	if c.ctx.Err() != nil {
		return false
	}
	f()
	return true
}

// resolve works out cv's art: what the table recorded, if nothing it was made
// from has changed, or else a fresh look in the track's tags and folder.
func (c *Cache) resolve(cv media.Cover) {
	key := keyOf(cv)
	old, have, err := c.load(key.db())
	if err != nil {
		c.opts.Log.Warn("read art record", "album", cv.Album, "err", err)
	}
	track := cv.Path
	if have && old.track != "" && exists(old.track) {
		track = old.track // the file the record's times were taken from
	}
	if have && c.current(old, track) {
		c.settle(key, old)
		return
	}
	r := record{key: key.db(), track: track, lookedUp: old.lookedUp}
	if have && strings.HasPrefix(old.source, onlineSource) && !c.exists(old.file) {
		r.lookedUp = 0 // the art found online has gone; it can be found again
	}
	var perr error
	if !c.decoding(func() {
		// A picture that won't decode, like an embedded one too big for
		// the device, gives way to the next.
		for data, source := range locals(track) {
			if perr = c.process(&r, data, source); perr == nil || errors.Is(perr, errWrite) {
				return
			}
			c.opts.Log.Warn("album art", "source", source, "err", perr)
		}
	}) {
		return
	}
	if errors.Is(perr, errWrite) {
		// Not the album's fault: record nothing, and don't go looking online
		// for what is already here. It's tried again after retryFailed.
		c.opts.Log.Warn("album art", "album", cv.Album, "err", perr)
		if have && strings.HasPrefix(old.source, onlineSource) && c.exists(old.file) {
			c.settle(key, old) // the art found online is still good
			return
		}
		c.settleBroken(key)
		return
	}
	if r.file == "" && have && strings.HasPrefix(old.source, onlineSource) && c.exists(old.file) {
		// Art found online stays until a picture turns up with the music.
		r.file, r.source, r.hue, r.chroma, r.colour = old.file, old.source, old.hue, old.chroma, old.colour
	}
	r.trackMtime, r.dirMtime, r.sourceMtime = stamps(track, r.source)
	if err := c.store(r, old.file); err != nil {
		c.opts.Log.Warn("save art record", "album", cv.Album, "err", err)
	}
	c.settle(key, r)
}

// current reports whether r still describes the art: nothing it was made
// from has changed since, and its files are there.
func (c *Cache) current(r record, track string) bool {
	t, d, s := stamps(track, r.source)
	return r.track == track && r.trackMtime == t && r.dirMtime == d && r.sourceMtime == s &&
		(r.file == "" || c.exists(r.file))
}

// stamps are the modification times that show whether an album's art may
// have changed: its track's, for an embedded picture; its folders', for a
// cover file added or removed; and the cover file's own, for one replaced.
// Missing files count as 0.
func stamps(track, source string) (trackM, dirM, sourceM int64) {
	if track == "" {
		return 0, 0, 0
	}
	trackM = mtime(track)
	dir := filepath.Dir(track)
	dirM = mtime(dir)
	if ad := library.AlbumDir(track); ad != dir {
		dirM += mtime(ad)
	}
	if source != "" && source != track && !strings.HasPrefix(source, onlineSource) {
		sourceM = mtime(source)
	}
	return trackM, dirM, sourceM
}

func mtime(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime().UnixNano()
	}
	return 0
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// exists reports whether the master and the backdrop of the art named file are there.
func (c *Cache) exists(file string) bool {
	return exists(filepath.Join(c.opts.Dir, file+".jpg")) && exists(filepath.Join(c.opts.Dir, file+"-bg.png"))
}

// locals yields the pictures for the album of track, with where each came
// from: the one in its tags, then those beside it.
func locals(track string) iter.Seq2[[]byte, string] {
	return func(yield func([]byte, string) bool) {
		if track == "" {
			return
		}
		if data, ok := Embedded(track); ok && !yield(data, track) {
			return
		}
		for _, dir := range albumDirs(track) {
			f, ok := FolderImage(dir)
			if !ok {
				continue
			}
			if fi, err := os.Stat(f); err != nil || fi.Size() > maxSourceBytes {
				continue
			}
			if data, err := os.ReadFile(f); err == nil && !yield(data, f) {
				return
			}
		}
	}
}

// process decodes a picture and writes the master copy and the backdrop,
// filling in r's file, source and colour. The caller holds c.heavy. A failure
// to write is an errWrite.
func (c *Cache) process(r *record, data []byte, source string) error {
	master, err := func() (*image.NRGBA, error) {
		img, err := Decode(data)
		if err != nil {
			return nil, err
		}
		return Square(img, masterPx), nil
	}()
	if err != nil {
		return err
	}
	hue, chroma, ok := Colour(master)
	base := fileBase(r.key)
	if err := writeJPEG(filepath.Join(c.opts.Dir, base+".jpg"), master); err != nil {
		return fmt.Errorf("%w: %w", errWrite, err)
	}
	if err := writePNG(filepath.Join(c.opts.Dir, base+"-bg.png"), Backdrop(master)); err != nil {
		c.remove(base)
		return fmt.Errorf("%w: %w", errWrite, err)
	}
	r.file, r.source, r.hue, r.chroma, r.colour = base, source, hue, chroma, ok
	return nil
}

// settle records what resolving found: sizes asked for meanwhile are made
// now, and an album without art may be searched for online.
func (c *Cache) settle(key albumKey, r record) {
	c.mu.Lock()
	a := c.albums[key]
	a.lookedUp = r.lookedUp
	if r.file == "" {
		a.state, a.file = missing, ""
		clear(a.wanted)
		c.searchLocked(key, a)
	} else {
		a.state, a.file, a.hue, a.chroma, a.colour = found, r.file, r.hue, r.chroma, r.colour
		for px := range a.wanted {
			c.pushLocked(job{key: key, kind: sizeJob, px: px})
		}
	}
	c.mu.Unlock()
	c.notify()
}

// settleBroken leaves the album without art after a failure to write its
// copies. Nothing is recorded and no search is made for it; it is looked at
// again once retryFailed has passed, so a broken disk is not retried every
// time the album is drawn.
func (c *Cache) settleBroken(key albumKey) {
	c.mu.Lock()
	a := c.albums[key]
	a.state, a.file, a.broken = missing, "", true
	a.failed = time.Now()
	clear(a.wanted)
	c.mu.Unlock()
	c.notify()
}

// size reads, or makes, the album's art at px (the backdrop for 0) and
// leaves it for the UI to take.
func (c *Cache) size(key albumKey, px int) {
	c.mu.Lock()
	a := c.albums[key]
	file, album := a.file, a.cover.Album
	c.mu.Unlock()
	if file == "" {
		return // resolved again since, and now without art
	}
	var img *image.NRGBA
	var err error
	if px == 0 {
		img, err = readImage(filepath.Join(c.opts.Dir, file+"-bg.png"))
	} else {
		img, err = c.sized(file, px)
	}
	if err != nil {
		// A copy is gone or damaged: delete them all so the art is made again from
		// its source when it is next asked for.
		c.opts.Log.Warn("cached art", "album", album, "err", err)
		c.remove(file)
	}
	c.mu.Lock()
	if err != nil {
		if a.file == file {
			a.state = unknown
			clear(a.wanted)
		}
	} else {
		c.done = append(c.done, picture{key, px, img})
		if len(c.done) > maxDone {
			old := c.done[0]
			c.done = slices.Delete(c.done, 0, 1)
			delete(c.albums[old.key].wanted, old.px)
		}
	}
	c.mu.Unlock()
	c.notify()
}

// sized reads the art at px, making that copy from the master the first time.
func (c *Cache) sized(file string, px int) (*image.NRGBA, error) {
	path := filepath.Join(c.opts.Dir, fmt.Sprintf("%s-%d.jpg", file, px))
	if img, err := readImage(path); err == nil {
		return img, nil
	}
	master, err := readImage(filepath.Join(c.opts.Dir, file+".jpg"))
	if err != nil {
		return nil, err
	}
	if px >= master.Rect.Dx() {
		return master, nil // small art: the screen scales it up
	}
	img := Square(master, px)
	if err := writeJPEG(path, img); err != nil {
		c.opts.Log.Warn("save art copy", "path", path, "err", err)
	}
	return img, nil
}

// searchLocked queues an online search for album a, which has no art, unless
// it can't be searched for or was lately.
func (c *Cache) searchLocked(key albumKey, a *album) {
	cv := a.cover
	if c.opts.Lookup == nil || a.searching || a.broken || cv.Artist == "" || cv.Artist == library.UnknownArtist ||
		cv.Album == library.UnknownAlbum {
		return
	}
	now := time.Now()
	if now.Sub(time.UnixMilli(a.lookedUp)) < retryNotFound || now.Sub(a.failed) < retryFailed || now.Before(c.busyUntil) {
		return
	}
	a.searching = true
	c.searches = append(c.searches, key)
	if len(c.searches) > maxSearches {
		c.albums[c.searches[0]].searching = false
		c.searches = slices.Delete(c.searches, 0, 1)
	}
	select {
	case c.searching <- struct{}{}:
	default:
	}
}

// searcher runs the queued online searches one at a time, newest first.
func (c *Cache) searcher() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.searching:
		}
		for c.ctx.Err() == nil {
			c.mu.Lock()
			if len(c.searches) == 0 {
				c.mu.Unlock()
				break
			}
			key := c.searches[len(c.searches)-1]
			c.searches = c.searches[:len(c.searches)-1]
			if time.Now().Before(c.busyUntil) {
				// Asked to wait since these were queued.
				c.dropSearchesLocked(key)
				c.mu.Unlock()
				break
			}
			cv, prev := c.albums[key].cover, c.albums[key].lookedUp
			c.mu.Unlock()
			if !c.opts.Online() {
				c.offline(key)
				break
			}
			c.trySearch(key, cv, prev)
		}
	}
}

// trySearch is search, recovering from a panic, which a decoder given a
// damaged download may raise. The panic is logged and the search counts as
// failed, not as one that found nothing: the album's last search stays at
// prev, and it is tried again after retryFailed.
func (c *Cache) trySearch(key albumKey, cv media.Cover, prev int64) {
	defer func() {
		if p := recover(); p != nil {
			c.opts.Log.Error("art search panicked", "album", cv.Album, "panic", p, "stack", string(debug.Stack()))
			c.mu.Lock()
			a := c.albums[key]
			a.searching, a.lookedUp, a.failed = false, prev, time.Now()
			c.mu.Unlock()
		}
	}()
	c.search(key, cv)
}

// offline drops the queued searches and holds new ones off for 30 s; albums
// are queued again when they are next shown.
func (c *Cache) offline(key albumKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropSearchesLocked(key)
	c.busyUntil = time.Now().Add(30 * time.Second)
}

// dropSearchesLocked forgets the queued searches, and key, which was taken off
// the queue.
func (c *Cache) dropSearchesLocked(key albumKey) {
	c.albums[key].searching = false
	for _, k := range c.searches {
		c.albums[k].searching = false
	}
	c.searches = c.searches[:0]
}

func (c *Cache) search(key albumKey, cv media.Cover) {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	data, id, err := c.opts.Lookup.Find(ctx, cv.Artist, cv.Album)
	now := time.Now()
	c.mu.Lock()
	a := c.albums[key]
	prev := a.lookedUp
	a.searching = false
	if err == nil || errors.Is(err, ErrNotFound) {
		// Marked now, not when the result is saved, so that asking for the album
		// in between doesn't queue the same search again.
		a.lookedUp = now.UnixMilli()
	} else {
		a.failed = now
		var se *netx.StatusError
		if errors.As(err, &se) && (se.Code == http.StatusServiceUnavailable || se.Code == http.StatusTooManyRequests) {
			c.busyUntil = now.Add(max(se.RetryAfter, time.Minute))
		}
	}
	c.mu.Unlock()
	switch {
	case c.ctx.Err() != nil:
		return
	case errors.Is(err, ErrNotFound):
		c.lookedUp(key, now)
		return
	case err != nil:
		c.opts.Log.Warn("art search", "album", cv.Album, "err", err)
		return
	}
	old, _, err := c.load(key.db())
	if err != nil {
		c.opts.Log.Warn("read art record", "album", cv.Album, "err", err)
	}
	r := record{key: key.db(), track: old.track, lookedUp: now.UnixMilli()}
	if r.track == "" {
		r.track = cv.Path
	}
	if !c.decoding(func() { err = c.process(&r, data, onlineSource+id) }) {
		return
	}
	if errors.Is(err, errWrite) {
		// Found, but not saved: not a search that found nothing. Try again later.
		c.opts.Log.Warn("album art", "source", onlineSource+id, "err", err)
		c.mu.Lock()
		a.lookedUp, a.failed = prev, now
		c.mu.Unlock()
		return
	} else if err != nil {
		c.opts.Log.Warn("album art", "source", onlineSource+id, "err", err)
		c.lookedUp(key, now)
		return
	}
	r.trackMtime, r.dirMtime, r.sourceMtime = stamps(r.track, r.source)
	if err := c.store(r, old.file); err != nil {
		c.opts.Log.Warn("save art record", "album", cv.Album, "err", err)
	}
	c.settle(key, r)
}

// lookedUp records that an online search for key found nothing at t.
func (c *Cache) lookedUp(key albumKey, t time.Time) {
	ms := t.UnixMilli()
	if _, err := c.opts.DB.Exec(`UPDATE art SET looked_up = ? WHERE key = ?`, ms, key.db()); err != nil {
		c.opts.Log.Warn("save art record", "err", err)
	}
	c.mu.Lock()
	c.albums[key].lookedUp = ms
	c.mu.Unlock()
}

func (c *Cache) load(key string) (record, bool, error) {
	r := record{key: key}
	err := c.opts.DB.QueryRow(`SELECT file, source, track, track_mtime, dir_mtime, source_mtime, hue, chroma, colour, looked_up
		FROM art WHERE key = ?`, key).Scan(&r.file, &r.source, &r.track, &r.trackMtime, &r.dirMtime, &r.sourceMtime,
		&r.hue, &r.chroma, &r.colour, &r.lookedUp)
	if errors.Is(err, sql.ErrNoRows) {
		return record{key: key}, false, nil
	}
	if err != nil {
		return record{key: key}, false, err
	}
	return r, true, nil
}

// store saves r and deletes the copies of the art it replaces.
func (c *Cache) store(r record, oldFile string) error {
	_, err := c.opts.DB.Exec(`INSERT INTO art (key, file, source, track, track_mtime, dir_mtime, source_mtime, hue, chroma, colour, looked_up)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET file = excluded.file, source = excluded.source, track = excluded.track,
			track_mtime = excluded.track_mtime, dir_mtime = excluded.dir_mtime, source_mtime = excluded.source_mtime,
			hue = excluded.hue, chroma = excluded.chroma, colour = excluded.colour, looked_up = excluded.looked_up`,
		r.key, r.file, r.source, r.track, r.trackMtime, r.dirMtime, r.sourceMtime, r.hue, r.chroma, r.colour, r.lookedUp)
	if err == nil && oldFile != "" && oldFile != r.file {
		c.remove(oldFile)
	}
	return err
}

// remove deletes the cached copies named base, in base's folder.
func (c *Cache) remove(base string) {
	paths, _ := filepath.Glob(filepath.Join(c.opts.Dir, base+"*"))
	for _, p := range paths {
		os.Remove(p)
	}
}
