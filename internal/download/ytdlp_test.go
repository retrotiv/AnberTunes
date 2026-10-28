package download

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
)

// release serves a yt-dlp release: SHA2-256SUMS and the zip named asset,
// holding the files given. It counts downloads of the zip.
type release struct {
	srv   *httptest.Server
	zip   []byte
	sum   string
	pulls atomic.Int32

	chunked bool          // serve the zip without a Content-Length
	stall   chan struct{} // if set, serve a few bytes, signal, and hang
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o644)
		if name == "yt-dlp_test" {
			h.SetMode(0o755)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newRelease(t *testing.T, files map[string]string) *release {
	r := &release{}
	r.set(t, files)
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/SHA2-256SUMS":
			w.Write([]byte("0000  yt-dlp_other.zip\n" + r.sum + "  yt-dlp_test.zip\n"))
		case "/yt-dlp_test.zip":
			r.pulls.Add(1)
			if r.stall != nil {
				w.Header().Set("Content-Length", strconv.Itoa(len(r.zip)))
				w.Write(r.zip[:10])
				w.(http.Flusher).Flush()
				r.stall <- struct{}{}
				<-req.Context().Done()
				return
			}
			if r.chunked {
				// No Content-Length: flushing makes the body chunked.
				half := len(r.zip) / 2
				w.Write(r.zip[:half])
				w.(http.Flusher).Flush()
				w.Write(r.zip[half:])
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(r.zip)))
			w.Write(r.zip)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *release) set(t *testing.T, files map[string]string) {
	r.zip = makeZip(t, files)
	s := sha256.Sum256(r.zip)
	r.sum = hex.EncodeToString(s[:])
}

func testInstaller(r *release, dir string) *Installer {
	return &Installer{HTTP: r.srv.Client(), Dir: dir, Asset: "yt-dlp_test.zip", Base: r.srv.URL + "/"}
}

func TestVersion(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "#!/bin/sh\necho 2026.10.11\n"})
	in := testInstaller(r, t.TempDir())
	if v, _ := in.Version(); v != "" {
		t.Fatalf("version %q before installing", v)
	}
	if _, err := in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if v, at := in.Version(); v != "2026.10.11" || time.Since(at) > time.Minute {
		t.Fatalf("version %q, installed %v", v, at)
	}
	// An install made before versions were kept has none until it is asked.
	os.Remove(filepath.Join(in.Dir, "yt-dlp", "VERSION"))
	if v, _ := in.Version(); v != "" {
		t.Fatalf("version %q without the file", v)
	}
	if err := in.RecordVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, _ := in.Version(); v != "2026.10.11" {
		t.Fatalf("recorded %q", v)
	}
	// What isn't a version isn't kept as one.
	os.WriteFile(in.Program(), []byte("#!/bin/sh\necho 'Usage: yt-dlp [OPTIONS] URL'\n"), 0o755)
	if err := in.RecordVersion(context.Background()); err == nil {
		t.Fatal("kept a usage line as the version")
	}
}

func TestInstallAndUpdate(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "#!/bin/sh\necho 1\n", "_internal/lib.txt": "old"})
	dir := t.TempDir()
	in := testInstaller(r, dir)
	if in.Program() != "" {
		t.Fatal("installed before installing")
	}
	var last, total int64
	did, err := in.Install(context.Background(), func(d, n int64) { last, total = d, n })
	if err != nil || !did {
		t.Fatalf("install: %v, %v", did, err)
	}
	prog := in.Program()
	info, err := os.Stat(prog)
	if err != nil || info.Mode()&0o100 == 0 || prog != filepath.Join(dir, "yt-dlp", "yt-dlp_test") {
		t.Fatalf("program %q: %v, %v", prog, info, err)
	}
	if last != int64(len(r.zip)) || total != last {
		t.Fatalf("progress ended at %d of %d", last, total)
	}

	// The same release again is left alone.
	if did, err := in.Install(context.Background(), nil); did || err != nil || r.pulls.Load() != 1 {
		t.Fatalf("again: %v, %v, %d downloads", did, err, r.pulls.Load())
	}

	// A new release replaces the folder whole.
	r.set(t, map[string]string{"yt-dlp_test": "#!/bin/sh\necho 2\n", "_internal/new.txt": "new"})
	if did, err := in.Install(context.Background(), nil); !did || err != nil {
		t.Fatalf("update: %v, %v", did, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "yt-dlp", "_internal", "lib.txt")); !os.IsNotExist(err) {
		t.Fatal("the old release's files were kept")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "yt-dlp", "_internal", "new.txt")); string(b) != "new" {
		t.Fatal("the new release's files are missing")
	}
	for _, left := range []string{"yt-dlp.new", "yt-dlp.old", "yt-dlp.zip.part"} {
		if _, err := os.Stat(filepath.Join(dir, left)); !os.IsNotExist(err) {
			t.Errorf("%s was left behind", left)
		}
	}
}

func TestABadDownloadLeavesTheInstallAlone(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "one"})
	dir := t.TempDir()
	in := testInstaller(r, dir)
	if _, err := in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	r.set(t, map[string]string{"yt-dlp_test": "two"})
	r.zip = bytes.Clone(r.zip)
	r.zip[len(r.zip)/2] ^= 0xff // no longer what the sum says
	if _, err := in.Install(context.Background(), nil); err == nil {
		t.Fatal("a zip that doesn't match its sum was installed")
	}
	if b, _ := os.ReadFile(in.Program()); string(b) != "one" {
		t.Fatalf("the installed program is now %q", b)
	}

	r.set(t, map[string]string{"yt-dlp_test": "three", "../escape.txt": "out"})
	if _, err := in.Install(context.Background(), nil); err == nil {
		t.Fatal("a zip reaching outside its folder was installed")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("a file was written outside the folder")
	}

	r.set(t, map[string]string{"something_else": "four"})
	if _, err := in.Install(context.Background(), nil); err == nil {
		t.Fatal("a zip without the program was installed")
	}
	if b, _ := os.ReadFile(in.Program()); string(b) != "one" {
		t.Fatalf("the installed program is now %q", b)
	}
}

// limits sets the limits for one test.
func limits(t *testing.T, zipBytes, unpacked int64, entries int) {
	t.Helper()
	z, u, e := maxZip, maxUnpacked, maxEntries
	maxZip, maxUnpacked, maxEntries = zipBytes, unpacked, entries
	t.Cleanup(func() { maxZip, maxUnpacked, maxEntries = z, u, e })
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestInterruptedUpdatesAreTidied(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "one", "_internal/a.txt": "a"})
	dir := t.TempDir()
	in := testInstaller(r, dir)
	if _, err := in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	junk := func() {
		for _, d := range []string{"yt-dlp.old", "yt-dlp.new"} {
			os.MkdirAll(filepath.Join(dir, d), 0o755)
			os.WriteFile(filepath.Join(dir, d, "junk"), []byte("x"), 0o644)
		}
		os.WriteFile(filepath.Join(dir, "yt-dlp.zip.part"), []byte("x"), 0o644)
	}

	// The release is the one installed: nothing downloads, the junk goes.
	junk()
	if did, err := in.Install(context.Background(), nil); did || err != nil || r.pulls.Load() != 1 {
		t.Fatalf("unchanged: %v, %v, %d downloads", did, err, r.pulls.Load())
	}
	for _, left := range []string{"yt-dlp.new", "yt-dlp.old", "yt-dlp.zip.part"} {
		if exists(filepath.Join(dir, left)) {
			t.Errorf("%s was left behind", left)
		}
	}

	// A crash between the two renames: no folder, only .old. Download again.
	if err := os.Rename(filepath.Join(dir, "yt-dlp"), filepath.Join(dir, "yt-dlp.old")); err != nil {
		t.Fatal(err)
	}
	junk()
	if in.Program() != "" {
		t.Fatal("a program with no folder")
	}
	if did, err := in.Install(context.Background(), nil); !did || err != nil || r.pulls.Load() != 2 {
		t.Fatalf("recover: %v, %v, %d downloads", did, err, r.pulls.Load())
	}
	if b, _ := os.ReadFile(in.Program()); string(b) != "one" {
		t.Fatalf("program is %q", b)
	}
	for _, left := range []string{"yt-dlp.new", "yt-dlp.old", "yt-dlp.zip.part"} {
		if exists(filepath.Join(dir, left)) {
			t.Errorf("%s was left behind", left)
		}
	}

	// A stale .new ahead of an update is not unpacked over.
	junk()
	r.set(t, map[string]string{"yt-dlp_test": "two"})
	if did, err := in.Install(context.Background(), nil); !did || err != nil {
		t.Fatalf("update: %v, %v", did, err)
	}
	if exists(filepath.Join(dir, "yt-dlp", "junk")) || exists(filepath.Join(dir, "yt-dlp", "_internal")) {
		t.Fatal("leftovers reached the install")
	}
}

func TestCancelDuringDownload(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "one"})
	r.stall = make(chan struct{}, 1)
	dir := t.TempDir()
	in := testInstaller(r, dir)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-r.stall
		cancel()
	}()
	if _, err := in.Install(ctx, nil); err == nil {
		t.Fatal("a cancelled install succeeded")
	}
	if in.Program() != "" || exists(filepath.Join(dir, "yt-dlp.zip.part")) {
		t.Fatal("a cancelled install left something")
	}
}

func TestUnknownLength(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "one", "_internal/a.txt": "a"})
	r.chunked = true
	dir := t.TempDir()
	in := testInstaller(r, dir)
	var last, total int64
	if did, err := in.Install(context.Background(), func(d, n int64) { last, total = d, n }); !did || err != nil {
		t.Fatalf("install: %v, %v", did, err)
	}
	if last != int64(len(r.zip)) || total != -1 {
		t.Fatalf("progress ended at %d of %d", last, total)
	}
}

func TestDownloadOverTheCap(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		r := newRelease(t, map[string]string{"yt-dlp_test": "one", "_internal/a.txt": strings.Repeat("a", 4000)})
		r.chunked = chunked
		limits(t, int64(len(r.zip))-1, 400<<20, 10000)
		dir := t.TempDir()
		in := testInstaller(r, dir)
		if _, err := in.Install(context.Background(), nil); err == nil {
			t.Fatalf("chunked=%v: an oversized download was installed", chunked)
		}
		if in.Program() != "" || exists(filepath.Join(dir, "yt-dlp.zip.part")) {
			t.Fatalf("chunked=%v: left something behind", chunked)
		}
	}
}

func TestUnpackedLimits(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "one", "_internal/a.txt": strings.Repeat("a", 5000)})
	for name, lim := range map[string]func(){
		"size":    func() { limits(t, 200<<20, 1000, 10000) },
		"entries": func() { limits(t, 200<<20, 400<<20, 1) },
	} {
		lim()
		in := testInstaller(r, t.TempDir())
		if _, err := in.Install(context.Background(), nil); err == nil || in.Program() != "" {
			t.Errorf("%s: an oversized zip was installed (%v)", name, err)
		}
	}
}

func TestOddEntriesAreRefused(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "one"})
	dir := t.TempDir()
	in := testInstaller(r, dir)
	if _, err := in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	r.set(t, map[string]string{"yt-dlp_test": "two", "/abs.txt": "out"})
	if _, err := in.Install(context.Background(), nil); err == nil {
		t.Fatal("an absolute path was installed")
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("yt-dlp_test")
	f.Write([]byte("three"))
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(os.ModeSymlink | 0o777)
	f, _ = w.CreateHeader(h)
	f.Write([]byte("/etc/passwd"))
	w.Close()
	r.zip = buf.Bytes()
	s := sha256.Sum256(r.zip)
	r.sum = hex.EncodeToString(s[:])
	if _, err := in.Install(context.Background(), nil); err == nil {
		t.Fatal("a symlink was installed")
	}
	if b, _ := os.ReadFile(in.Program()); string(b) != "one" {
		t.Fatalf("the installed program is now %q", b)
	}
	if exists(filepath.Join(dir, "yt-dlp.new")) {
		t.Fatal("yt-dlp.new was left behind")
	}
}

func TestUppercaseSum(t *testing.T) {
	r := newRelease(t, map[string]string{"yt-dlp_test": "one"})
	r.sum = strings.ToUpper(r.sum)
	in := testInstaller(r, t.TempDir())
	if did, err := in.Install(context.Background(), nil); !did || err != nil {
		t.Fatalf("install: %v, %v", did, err)
	}
	if did, err := in.Install(context.Background(), nil); did || err != nil || r.pulls.Load() != 1 {
		t.Fatalf("again: %v, %v, %d downloads", did, err, r.pulls.Load())
	}
}
