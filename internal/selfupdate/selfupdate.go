// Package selfupdate stages a newer Boombrick beside the running one: the
// release's zip for its firmware, checked against its SHA256SUMS, and its
// own folder's files unpacked into the app's .update folder. The app's
// launch.sh moves them into place when Boombrick next starts; the running
// program is never touched.
package selfupdate

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ujjwal313/boombrick/internal/netx"
	"github.com/ujjwal313/boombrick/internal/release"
)

// Sums is the release's list of checksums.
const Sums = "SHA256SUMS"

// Pak is a firmware's update: the release file, the folder in its zip that
// holds the app's own files, as "Apps/Boombrick/", and the script there
// that starts the app and moves a staged update in.
type Pak struct {
	Asset, Dir, Launcher string
}

// The limits on an update: the zip is about 14 MB.
var (
	maxZip      int64 = 64 << 20
	maxUnpacked int64 = 160 << 20
	maxEntries        = 1000
)

// Staged reports the version staged in pakDir for the next launch, if one is.
func Staged(pakDir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(pakDir, ".update", "ready"))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// Stage fetches r's zip for pak into pakDir/.update, replacing whatever is
// there, reporting progress as the zip comes. The ready file is written
// last, after a sync, so launch.sh never moves in an update that isn't whole;
// a failed stage leaves no .update behind.
func Stage(ctx context.Context, c *http.Client, r release.Release, pakDir string, pak Pak, progress func(done, total int64)) (err error) {
	zipURL, sumsURL := r.Assets[pak.Asset], r.Assets[Sums]
	if zipURL == "" || sumsURL == "" {
		return fmt.Errorf("release %s has no %s or %s", r.Tag, pak.Asset, Sums)
	}
	sums, err := netx.Get(ctx, c, sumsURL, 64<<10)
	if err != nil {
		return fmt.Errorf("%s: %w", Sums, err)
	}
	want, ok := netx.SumFor(sums, pak.Asset)
	if !ok {
		return fmt.Errorf("%s: no %s", Sums, pak.Asset)
	}
	dir := filepath.Join(pakDir, ".update")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	zipPath := filepath.Join(dir, pak.Asset)
	if err := netx.Download(ctx, c, zipURL, zipPath, want, maxZip, progress); err != nil {
		return fmt.Errorf("%s: %w", pak.Asset, err)
	}
	if err := unpack(ctx, zipPath, filepath.Join(dir, "pak"), pak); err != nil {
		return err
	}
	if err := os.Remove(zipPath); err != nil {
		return err
	}
	for _, f := range []string{"boombrick", pak.Launcher} {
		if _, err := os.Stat(filepath.Join(dir, "pak", f)); err != nil {
			return fmt.Errorf("%s holds no %s", pak.Asset, f)
		}
	}
	// FAT and exFAT can lose file data to a power cut; the ready file must
	// not outlive the files it vouches for.
	syscall.Sync()
	if err := os.WriteFile(filepath.Join(dir, "ready"), []byte(r.Version()+"\n"), 0o644); err != nil {
		return err
	}
	syscall.Sync()
	return nil
}

// unpack writes the files in pak's folder of the zip at src into the folder
// dst, refusing entries that would land outside it, that are neither folders
// nor plain files, or that make it larger than a pak can be.
func unpack(ctx context.Context, src, dst string, pak Pak) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	if len(r.File) > maxEntries {
		return fmt.Errorf("%s holds %d entries", pak.Asset, len(r.File))
	}
	left := maxUnpacked
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(f.Name, pak.Dir)
		if !ok || rel == "" {
			continue // the folders above the pak
		}
		name := filepath.FromSlash(rel)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("%s holds %q, outside the pak", pak.Asset, f.Name)
		}
		p := filepath.Join(dst, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&fs.ModeType != 0 {
			return fmt.Errorf("%s holds %q, which is not a plain file", pak.Asset, f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		n, err := unpackFile(f, p, left)
		if err != nil {
			return err
		}
		left -= n
	}
	return nil
}

// unpackFile writes f to path, at most limit bytes, and returns how many.
func unpackFile(f *zip.File, path string, limit int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm()|0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, limit+1))
	if err == nil && n > limit {
		err = errors.New("the update unpacks to too much")
	}
	if err != nil {
		out.Close()
		return n, err
	}
	return n, out.Close()
}

// Status is an update as Settings shows it.
type Status struct {
	Staging  bool
	Progress float32 // how far the download has got, 0 to 1
	Staged   string  // the version waiting for the next launch, "" when none
	Failed   bool    // the last try failed
}

// tellEvery is how often progress is told while a download comes.
const tellEvery = 250 * time.Millisecond

// Updater stages a release on a goroutine of its own for Settings, which
// calls it from the UI thread and never waits. Ready receives a value when
// its status changes.
type Updater struct {
	http   *http.Client
	dir    string
	pak    Pak
	log    *slog.Logger
	ready  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	st     Status
	told   time.Time
	closed bool
}

// New returns an updater for the app in pakDir, which installs pak from each
// release, knowing any update staged there already.
func New(c *http.Client, pakDir string, pak Pak, log *slog.Logger) *Updater {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	u := &Updater{http: c, dir: pakDir, pak: pak, log: log, ready: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
	u.st.Staged, _ = Staged(pakDir)
	return u
}

// Stage fetches r and stages it, unless a release is being staged or r is
// staged already. It returns at once.
func (u *Updater) Stage(r release.Release) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed || u.st.Staging || u.st.Staged == r.Version() {
		return
	}
	u.st = Status{Staging: true}
	u.wg.Add(1)
	go u.stage(r)
}

func (u *Updater) stage(r release.Release) {
	defer u.wg.Done()
	ctx, cancel := context.WithTimeout(u.ctx, 10*time.Minute)
	defer cancel()
	err := Stage(ctx, u.http, r, u.dir, u.pak, u.progress)
	u.mu.Lock()
	u.st.Staging = false
	u.st.Failed = err != nil
	// A stage that failed early leaves the update staged before it in place.
	u.st.Staged, _ = Staged(u.dir)
	u.mu.Unlock()
	switch {
	case err == nil:
		u.log.Info("staged an update", "version", r.Version())
	case !errors.Is(err, context.Canceled):
		u.log.Warn("stage an update", "version", r.Version(), "err", err)
	}
	u.tell()
}

func (u *Updater) progress(done, total int64) {
	if total <= 0 {
		return
	}
	u.mu.Lock()
	u.st.Progress = float32(done) / float32(total)
	tell := done >= total || time.Since(u.told) >= tellEvery
	if tell {
		u.told = time.Now()
	}
	u.mu.Unlock()
	if tell {
		u.tell()
	}
}

func (u *Updater) tell() {
	select {
	case u.ready <- struct{}{}:
	default:
	}
}

// Status reports the update.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

// Ready receives a value when the status changes, a few times a second at
// most while a download comes. It is closed by Close.
func (u *Updater) Ready() <-chan struct{} { return u.ready }

// Close stops a stage under way, which leaves nothing staged, waits for it
// and closes Ready.
func (u *Updater) Close() {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return
	}
	u.closed = true
	u.mu.Unlock()
	u.cancel()
	u.wg.Wait()
	close(u.ready)
}
