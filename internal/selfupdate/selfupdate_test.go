package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/release"
)

// file is a zip entry.
type file struct {
	name, body string
	mode       os.FileMode
}

// The firmwares' paks, as their profiles name them.
var (
	nextUI   = Pak{Asset: "Boombrick.pak.zip", Dir: "Tools/tg5040/Boombrick.pak/", Launcher: "launch.sh"}
	crossMix = Pak{Asset: "Boombrick-CrossMix.zip", Dir: "Apps/Boombrick/", Launcher: "launch.sh"}
	knulli   = Pak{Asset: "Boombrick-Knulli.zip", Dir: "roms/ports/boombrick/", Launcher: "launch"}
)

// pak is p's release zip as the Makefile builds it.
func pak(p Pak, extra ...file) []file {
	top, _, _ := strings.Cut(p.Dir, "/")
	return append([]file{
		{name: top + "/", mode: os.ModeDir | 0o755},
		{name: p.Dir, mode: os.ModeDir | 0o755},
		{name: p.Dir + "boombrick", body: "new program", mode: 0o755},
		{name: p.Dir + p.Launcher, body: "#!/bin/sh\n", mode: 0o755},
		{name: p.Dir + "lib/libopus.so.0", body: "opus", mode: 0o644},
	}, extra...)
}

func makeZip(t *testing.T, files []file) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		h.SetMode(f.mode)
		zf, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		zf.Write([]byte(f.body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// server serves a release's zip, named asset, and SHA256SUMS, and counts
// downloads of the zip.
type server struct {
	srv   *httptest.Server
	asset string
	zip   []byte
	sum   string
	pulls atomic.Int32
	stall chan struct{} // if set, serve a few bytes, signal, and hang
}

func newServer(t *testing.T, asset string, files []file) *server {
	s := &server{asset: asset, zip: makeZip(t, files)}
	h := sha256.Sum256(s.zip)
	s.sum = hex.EncodeToString(h[:])
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/SHA256SUMS":
			w.Write([]byte(s.sum + "  " + asset + "\n"))
		case "/" + asset:
			s.pulls.Add(1)
			w.Header().Set("Content-Length", strconv.Itoa(len(s.zip)))
			if s.stall != nil {
				w.Write(s.zip[:10])
				w.(http.Flusher).Flush()
				s.stall <- struct{}{}
				<-r.Context().Done()
				return
			}
			w.Write(s.zip)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *server) release(tag string) release.Release {
	return release.Release{Tag: tag, Assets: map[string]string{
		s.asset: s.srv.URL + "/" + s.asset,
		Sums:    s.srv.URL + "/SHA256SUMS",
	}}
}

func TestStage(t *testing.T) {
	s := newServer(t, nextUI.Asset, pak(nextUI, file{name: "README.txt", body: "not the pak's", mode: 0o644}))
	dir := t.TempDir()
	var last, total int64
	if err := Stage(context.Background(), s.srv.Client(), s.release("v0.5.0"), dir, nextUI, func(d, n int64) { last, total = d, n }); err != nil {
		t.Fatal(err)
	}
	if last != int64(len(s.zip)) || total != last {
		t.Fatalf("progress ended at %d of %d", last, total)
	}
	staged := filepath.Join(dir, ".update", "pak")
	if b, _ := os.ReadFile(filepath.Join(staged, "lib", "libopus.so.0")); string(b) != "opus" {
		t.Fatal("a file in a folder of the pak is missing")
	}
	if info, err := os.Stat(filepath.Join(staged, "boombrick")); err != nil || info.Mode()&0o100 == 0 {
		t.Fatalf("the program: %v, %v", info, err)
	}
	for _, gone := range []string{filepath.Join(staged, "README.txt"), filepath.Join(staged, "Tools"), filepath.Join(dir, ".update", nextUI.Asset)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s is there", gone)
		}
	}
	if v, ok := Staged(dir); !ok || v != "0.5.0" {
		t.Fatalf("Staged = %q, %v", v, ok)
	}
}

// CrossMix's zip holds Apps/Boombrick/, and only that is staged; a zip for
// the other firmware holds nothing of it.
func TestStageCrossMix(t *testing.T) {
	s := newServer(t, crossMix.Asset, pak(crossMix))
	dir := t.TempDir()
	if err := Stage(context.Background(), s.srv.Client(), s.release("v0.5.0"), dir, crossMix, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".update", "pak", "boombrick")); string(b) != "new program" {
		t.Fatal("the program wasn't staged")
	}
	other := newServer(t, crossMix.Asset, pak(nextUI))
	dir = t.TempDir()
	if err := Stage(context.Background(), other.srv.Client(), other.release("v0.5.0"), dir, crossMix, nil); err == nil {
		t.Fatal("staged NextUI's pak for CrossMix")
	}
}

// Knulli's zip holds the script Knulli lists under Ports beside the app's
// folder, roms/ports/boombrick/, and only the folder is staged; its
// launcher has no .sh, which would list it under Ports too. A zip with
// launch.sh in its place isn't Knulli's.
func TestStageKnulli(t *testing.T) {
	s := newServer(t, knulli.Asset, pak(knulli, file{name: "roms/ports/Boombrick.sh", body: "#!/bin/bash\n", mode: 0o755}))
	dir := t.TempDir()
	if err := Stage(context.Background(), s.srv.Client(), s.release("v0.5.0"), dir, knulli, nil); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, ".update", "pak")
	if b, _ := os.ReadFile(filepath.Join(staged, "launch")); string(b) != "#!/bin/sh\n" {
		t.Fatal("the launcher wasn't staged")
	}
	if _, err := os.Stat(filepath.Join(staged, "Boombrick.sh")); !os.IsNotExist(err) {
		t.Fatal("the script beside the folder was staged")
	}
	other := newServer(t, knulli.Asset, pak(Pak{Asset: knulli.Asset, Dir: knulli.Dir, Launcher: "launch.sh"}))
	dir = t.TempDir()
	if err := Stage(context.Background(), other.srv.Client(), other.release("v0.5.0"), dir, knulli, nil); err == nil {
		t.Fatal("staged a zip without Knulli's launcher")
	}
}

func TestStageRefuses(t *testing.T) {
	cases := map[string][]file{
		"an entry outside the pak": pak(nextUI, file{name: nextUI.Dir + "../../../evil", body: "x", mode: 0o644}),
		"a link":                   pak(nextUI, file{name: nextUI.Dir + "link", body: "/etc/passwd", mode: os.ModeSymlink | 0o777}),
		"no program":               pak(nextUI)[:2],
	}
	for what, files := range cases {
		s := newServer(t, nextUI.Asset, files)
		dir := t.TempDir()
		if err := Stage(context.Background(), s.srv.Client(), s.release("v0.5.0"), dir, nextUI, nil); err == nil {
			t.Errorf("%s: staged", what)
		}
		if _, err := os.Stat(filepath.Join(dir, ".update")); !os.IsNotExist(err) {
			t.Errorf("%s: .update was left behind", what)
		}
	}
	// A zip that isn't the one SHA256SUMS lists.
	s := newServer(t, nextUI.Asset, pak(nextUI))
	s.sum = "00" + s.sum[2:]
	dir := t.TempDir()
	if err := Stage(context.Background(), s.srv.Client(), s.release("v0.5.0"), dir, nextUI, nil); err == nil {
		t.Error("staged a zip with the wrong checksum")
	}
	if _, ok := Staged(dir); ok {
		t.Error("a bad zip is staged")
	}
	// A release without the files.
	if err := Stage(context.Background(), s.srv.Client(), release.Release{Tag: "v0.5.0"}, dir, nextUI, nil); err == nil {
		t.Error("staged a release with no assets")
	}
}

func waitFor(t *testing.T, u *Updater, ok func(Status) bool) Status {
	t.Helper()
	for deadline := time.After(5 * time.Second); ; {
		if st := u.Status(); ok(st) {
			return st
		}
		select {
		case <-u.Ready():
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out at %+v", u.Status())
		}
	}
}

func TestUpdater(t *testing.T) {
	s := newServer(t, nextUI.Asset, pak(nextUI))
	dir := t.TempDir()
	u := New(s.srv.Client(), dir, nextUI, nil)
	defer u.Close()
	if st := u.Status(); st != (Status{}) {
		t.Fatalf("before staging: %+v", st)
	}
	u.Stage(s.release("v0.5.0"))
	if !u.Status().Staging {
		t.Fatal("not staging")
	}
	st := waitFor(t, u, func(st Status) bool { return !st.Staging })
	if st.Staged != "0.5.0" || st.Failed || st.Progress != 1 {
		t.Fatalf("staged: %+v", st)
	}
	// The same release again does nothing; a new updater knows it is staged.
	u.Stage(s.release("v0.5.0"))
	if u.Status().Staging || s.pulls.Load() != 1 {
		t.Fatalf("staged again: %+v, %d downloads", u.Status(), s.pulls.Load())
	}
	again := New(s.srv.Client(), dir, nextUI, nil)
	defer again.Close()
	if st := again.Status(); st.Staged != "0.5.0" {
		t.Fatalf("a new updater: %+v", st)
	}
}

func TestUpdaterFails(t *testing.T) {
	s := newServer(t, nextUI.Asset, pak(nextUI))
	s.sum = "00" + s.sum[2:]
	u := New(s.srv.Client(), t.TempDir(), nextUI, nil)
	defer u.Close()
	u.Stage(s.release("v0.5.0"))
	st := waitFor(t, u, func(st Status) bool { return !st.Staging })
	if !st.Failed || st.Staged != "" {
		t.Fatalf("after a bad zip: %+v", st)
	}
}

// A newer release that can't be fetched leaves the one staged before it,
// which launch.sh still installs, showing in Settings.
func TestUpdaterKeepsTheStagedUpdateWhenTheNextFails(t *testing.T) {
	s := newServer(t, nextUI.Asset, pak(nextUI))
	dir := t.TempDir()
	if err := Stage(context.Background(), s.srv.Client(), s.release("v0.5.0"), dir, nextUI, nil); err != nil {
		t.Fatal(err)
	}
	u := New(s.srv.Client(), dir, nextUI, nil)
	defer u.Close()
	broken := s.release("v0.6.0")
	broken.Assets[Sums] = s.srv.URL + "/missing"
	u.Stage(broken)
	st := waitFor(t, u, func(st Status) bool { return !st.Staging })
	if !st.Failed || st.Staged != "0.5.0" {
		t.Fatalf("after a failed stage: %+v", st)
	}
}

func TestCloseStopsAStage(t *testing.T) {
	s := newServer(t, nextUI.Asset, pak(nextUI))
	s.stall = make(chan struct{}, 1)
	dir := t.TempDir()
	u := New(s.srv.Client(), dir, nextUI, nil)
	u.Stage(s.release("v0.5.0"))
	select {
	case <-s.stall:
	case <-time.After(3 * time.Second):
		t.Fatal("the download never began")
	}
	u.Close()
	u.Close()
	if _, err := os.Stat(filepath.Join(dir, ".update")); !os.IsNotExist(err) {
		t.Fatal("a stopped stage left .update")
	}
	u.Stage(s.release("v0.5.0")) // after Close: nothing happens
	drained := make(chan struct{})
	go func() {
		for range u.Ready() {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("Ready is still open")
	}
}
