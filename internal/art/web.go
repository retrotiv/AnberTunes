package art

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ujjwal313/boombrick/internal/netx"
)

// Web fetches pictures named by URL, such as YouTube Music's thumbnails, at
// the size they are drawn, newest first, on two goroutines. They are kept in
// memory only until the UI takes them: the UI holds them as textures and
// asks again if it lets one go.
type Web struct {
	http   *http.Client
	online func() bool
	log    *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	wake   chan struct{}
	ready  chan struct{}
	closed sync.Once

	// waits are how long a picture that failed waits to be asked for again,
	// and one missed while offline; tests shorten them.
	waits [2]time.Duration

	mu     sync.Mutex
	asked  []webKey // waiting, oldest first
	busy   map[webKey]bool
	done   map[webKey]*image.NRGBA
	failed map[webKey]time.Time // when each may be asked for again
}

type webKey struct {
	url string
	px  int
}

// Limits: pictures waiting (the oldest have scrolled away and are dropped),
// fetched and not yet taken, failures remembered, and one picture's size.
const (
	maxAsked   = 24
	maxFetched = 16
	maxFailed  = 256
	maxPicture = 2 << 20
	webRetry   = 30 * time.Second
	// offlineRetry is the wait for a picture missed while offline, so it
	// shows soon after the Wi-Fi is back. Asking again offline only reads
	// the status.
	offlineRetry = 3 * time.Second
)

var errOffline = errors.New("offline")

// NewWeb starts fetching through c while online reports true (nil: always).
func NewWeb(c *http.Client, online func() bool, log *slog.Logger) *Web {
	return newWeb(c, online, log, 2)
}

func newWeb(c *http.Client, online func() bool, log *slog.Logger, workers int) *Web {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Web{http: c, online: online, log: log, ctx: ctx, cancel: cancel,
		waits: [2]time.Duration{webRetry, offlineRetry},
		wake:  make(chan struct{}, 1), ready: make(chan struct{}, 1),
		busy: map[webKey]bool{}, done: map[webKey]*image.NRGBA{}, failed: map[webKey]time.Time{}}
	for range workers {
		w.wg.Add(1)
		go w.worker()
	}
	return w
}

// Close stops fetching, waits for the goroutines and closes Ready. It may be
// called more than once.
func (w *Web) Close() {
	w.closed.Do(func() {
		w.cancel()
		w.wg.Wait()
		close(w.ready)
	})
}

// Ready receives a value when a picture has been fetched, and is closed by
// Close.
func (w *Web) Ready() <-chan struct{} { return w.ready }

// Cover returns url's picture px pixels square, handing it over once, or nil
// while it is fetched. none is true when fetching it failed lately: in the
// last 30 s, or the last 3 s while offline.
func (w *Web) Cover(url string, px int) (img *image.NRGBA, none bool) {
	k := webKey{url, px}
	w.mu.Lock()
	defer w.mu.Unlock()
	if img, ok := w.done[k]; ok {
		delete(w.done, k)
		return img, false
	}
	if until, ok := w.failed[k]; ok {
		if time.Now().Before(until) {
			return nil, true
		}
		delete(w.failed, k)
	}
	if w.busy[k] || slices.Contains(w.asked, k) {
		return nil, false
	}
	if len(w.asked) == maxAsked {
		w.asked = slices.Delete(w.asked, 0, 1)
	}
	w.asked = append(w.asked, k)
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return nil, false
}

// take takes the newest picture asked for.
func (w *Web) take() (webKey, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(w.asked)
	if n == 0 {
		return webKey{}, false
	}
	k := w.asked[n-1]
	w.asked = w.asked[:n-1]
	w.busy[k] = true
	return k, true
}

func (w *Web) worker() {
	defer w.wg.Done()
	for w.ctx.Err() == nil {
		k, ok := w.take()
		if !ok {
			select {
			case <-w.ctx.Done():
			case <-w.wake:
			}
			continue
		}
		img, err := w.fetch(k)
		w.mu.Lock()
		delete(w.busy, k)
		switch {
		case w.ctx.Err() != nil:
		case err != nil:
			if len(w.failed) >= maxFailed {
				clear(w.failed)
			}
			wait := w.waits[0]
			if errors.Is(err, errOffline) {
				wait = w.waits[1]
			}
			w.failed[k] = time.Now().Add(wait)
		default:
			if len(w.done) >= maxFetched {
				for d := range w.done { // one the UI never took
					delete(w.done, d)
					break
				}
			}
			w.done[k] = img
		}
		w.mu.Unlock()
		if err == nil {
			select {
			case w.ready <- struct{}{}:
			default:
			}
		}
	}
}

// fetch gets k's picture and makes it px square. Decoders run on what the
// network sent, so a panic in one fails the picture rather than the app.
func (w *Web) fetch(k webKey) (img *image.NRGBA, err error) {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error("decoding a picture panicked", "url", k.url, "panic", r, "stack", string(debug.Stack()))
			img, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	if w.online != nil && !w.online() {
		return nil, errOffline
	}
	ctx, cancel := context.WithTimeout(w.ctx, 15*time.Second)
	defer cancel()
	data, err := netx.Get(ctx, w.http, Sized(k.url, k.px), maxPicture)
	if err == nil {
		var src image.Image
		if src, err = Decode(data); err == nil {
			return Square(src, k.px), nil
		}
	}
	if w.ctx.Err() == nil {
		w.log.Warn("fetch picture", "url", k.url, "err", err)
	}
	return nil, err
}

// Sized asks for url's picture px pixels square when it is on Google's
// image server, whose addresses end in options such as =w120-h120-l90-rj
// that it applies as it serves: -p crops to fill, -rj sends a JPEG. Other
// addresses are kept, and Square makes their pictures the size.
func Sized(url string, px int) string {
	i := strings.LastIndexByte(url, '=')
	if i < 0 || !strings.Contains(url[:i], ".googleusercontent.com/") {
		return url
	}
	return fmt.Sprintf("%s=w%d-h%d-p-l90-rj", url[:i], px, px)
}
