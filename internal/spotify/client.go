package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNoHelper means the helper isn't running: it is missing, failed to start
// or crashed, and is started again after a pause.
var ErrNoHelper = errors.New("spotify: the helper isn't running")

// Helper runs boombrick-spotify and asks it things. It starts the helper the
// first time it is needed, and again after it exits, waiting longer after
// each crash.
type Helper struct {
	path, dir, socket string
	log               *slog.Logger
	out               io.Writer // the helper's own log lines
	web               *http.Client
	backoff           func(crashes int) time.Duration

	mu      sync.Mutex
	running bool
	stdin   io.WriteCloser // closing it tells the helper to quit
	up      chan struct{}  // closed once the helper answers on its socket
	exited  chan struct{}  // closed when the helper exits
	crashes int
	next    time.Time // when the helper may be started again
	closed  bool
}

// NewHelper runs the helper at path, keeping its credentials in dir and
// writing its log lines to out.
func NewHelper(path, dir string, log *slog.Logger, out io.Writer) *Helper {
	h := &Helper{
		path:    path,
		dir:     dir,
		socket:  filepath.Join(os.TempDir(), fmt.Sprintf("boombrick-spotify-%d.sock", os.Getpid())),
		log:     log,
		out:     out,
		backoff: backoff,
	}
	h.web = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", h.socket)
		},
	}}
	return h
}

// backoff is the pause before starting the helper again after its nth crash
// in a row: 1 s, doubling to a minute.
func backoff(crashes int) time.Duration {
	return min(time.Second<<min(crashes-1, 6), time.Minute)
}

// Close stops the helper.
func (h *Helper) Close() error {
	h.mu.Lock()
	h.closed = true
	running, exited := h.running, h.exited
	if running {
		h.stdin.Close()
	}
	h.mu.Unlock()
	if running {
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
		}
	}
	return nil
}

// start runs the helper unless it is running, and waits until it answers.
func (h *Helper) start(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return ErrNoHelper
	}
	if !h.running {
		if time.Now().Before(h.next) {
			h.mu.Unlock()
			return ErrNoHelper
		}
		if err := h.launchLocked(); err != nil {
			h.crashedLocked(time.Now())
			h.mu.Unlock()
			h.log.Warn("start spotify helper", "err", err)
			return fmt.Errorf("%w: %w", ErrNoHelper, err)
		}
	}
	up, exited := h.up, h.exited
	h.mu.Unlock()
	select {
	case <-up:
		return nil
	case <-exited:
		return ErrNoHelper
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Helper) launchLocked() error {
	cmd := exec.Command(h.path, "serve", h.dir, h.socket)
	cmd.Stdout, cmd.Stderr = h.out, h.out
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	up, exited, started := make(chan struct{}), make(chan struct{}), time.Now()
	h.running, h.stdin, h.up, h.exited = true, stdin, up, exited
	go func() {
		err := cmd.Wait()
		h.mu.Lock()
		defer h.mu.Unlock()
		h.running = false
		close(exited)
		if !h.closed {
			h.log.Warn("spotify helper exited", "err", err)
			h.crashedLocked(started)
		}
	}()
	go func() {
		for {
			if c, err := net.Dial("unix", h.socket); err == nil {
				c.Close()
				close(up)
				return
			}
			select {
			case <-exited:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	return nil
}

// crashedLocked counts a crash of a helper started at started; one that ran
// a good while starts the count again.
func (h *Helper) crashedLocked(started time.Time) {
	if time.Since(started) > time.Minute {
		h.crashes = 0
	}
	h.crashes++
	h.next = time.Now().Add(h.backoff(h.crashes))
}

// Status is where the helper's session is.
func (h *Helper) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, h.call(ctx, http.MethodGet, "/status", &s)
}

// Login starts a device sign-in and returns its code.
func (h *Helper) Login(ctx context.Context) (Login, error) {
	var l Login
	return l, h.call(ctx, http.MethodPost, "/login", &l)
}

// Logout signs out, forgetting the credentials.
func (h *Helper) Logout(ctx context.Context) error {
	return h.call(ctx, http.MethodPost, "/logout", nil)
}

// Playlists lists the user's playlists.
func (h *Helper) Playlists(ctx context.Context) ([]Playlist, error) {
	var ps []Playlist
	return ps, h.call(ctx, http.MethodGet, "/playlists", &ps)
}

// Tracks lists a playlist's tracks, or Liked Songs' (LikedURI).
func (h *Helper) Tracks(ctx context.Context, uri string) ([]Track, error) {
	var ts []Track
	return ts, h.call(ctx, http.MethodGet, "/tracks?uri="+url.QueryEscape(uri), &ts)
}

func (h *Helper) call(ctx context.Context, method, path string, v any) error {
	resp, err := h.do(ctx, method, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// do asks the helper, starting it if need be; a reply other than success
// comes back as its error.
func (h *Helper) do(ctx context.Context, method, path string) (*http.Response, error) {
	if err := h.start(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://helper"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.web.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", ErrNoHelper, err)
	}
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, ErrorOf(resp.StatusCode, resp.Header.Get("Retry-After"), strings.TrimSpace(string(msg)))
	}
	return resp, nil
}
