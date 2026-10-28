//go:build live

package download

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/audio/decode"
	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/lyrics"
	"github.com/ujjwal313/boombrick/internal/netx"
	"github.com/ujjwal313/boombrick/internal/remux"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// TestLiveDownload does what the app does for one song: installs yt-dlp,
// finds a Creative Commons song on YouTube Music, downloads it with QuickJS,
// and tags, remuxes, files and decodes it.
func TestLiveDownload(t *testing.T) {
	qjs, _ := filepath.Abs("../../dist/desktop/bin/qjs")
	if _, err := os.Stat(qjs); err != nil {
		t.Skip("no QuickJS: run make build first")
	}
	web := netx.NewClient()
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	in := NewInstaller(web, filepath.Join(dir, "bin"))
	start := time.Now()
	if _, err := in.Install(ctx, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("installed yt-dlp in %v", time.Since(start).Round(time.Millisecond))
	ytc := ytmusic.NewClient(web)
	items, err := ytc.Search(ctx, "Kevin MacLeod Monkeys Spinning Monkeys", ytmusic.Songs)
	if err != nil || len(items) == 0 {
		t.Fatalf("search: %d, %v", len(items), err)
	}
	song := items[0].Song()
	r := &Runner{Program: in.Program(), QJS: qjs, Cache: filepath.Join(dir, "bin", "cache"), Parts: filepath.Join(dir, "parts"),
		Formats: "251/250/249"}
	start, landed := time.Now(), ""
	err = r.Run(ctx, []string{song.VideoID}, func(e Event) {
		if e.Path != "" {
			landed = e.Path
		}
		if e.Failure != NoFailure {
			t.Errorf("failed (%d): %s", e.Failure, e.Message)
		}
	})
	if err != nil || landed == "" {
		t.Fatalf("download: %v", err)
	}
	t.Logf("downloaded %q in %v", song.Title, time.Since(start).Round(time.Millisecond))
	start = time.Now()
	found, err := r.Search(ctx, "Kevin MacLeod Monkeys Spinning Monkeys")
	if err != nil || len(found) == 0 {
		t.Fatalf("yt-dlp search: %d, %v", len(found), err)
	}
	t.Logf("yt-dlp search: %d results in %v, the first %+v", len(found), time.Since(start).Round(time.Millisecond), found[0])
	f := &finisher{db: openDB(t), music: filepath.Join(dir, "Music"), http: web, albums: ytc, lyrics: lyrics.NewClient(web),
		formats: library.Formats{Supported: decode.Supported, Duration: decode.Duration}, remux: remux.File, log: slog.Default()}
	path, size, _, err := f.finish(ctx, song, landed)
	if err != nil {
		t.Fatal(err)
	}
	src, err := decode.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if secs := src.Frames() / int64(src.SampleRate()); secs < 60 {
		t.Fatalf("decodes to %d s", secs)
	}
	tags, err := library.ReadTags(path)
	if err != nil || tags.Title == "" || tags.Artist == "" {
		t.Fatalf("tags %+v, %v", tags, err)
	}
	t.Logf("%s: %d bytes, %+v", path, size, tags)
}
