//go:build live

package importer

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// Matches Liked Songs, or the playlist BOOMBRICK_IMPORT_URI names, against
// YouTube Music itself, downloading nothing.
//
//	BOOMBRICK_SPOTIFY_HELPER=dist/desktop/bin/boombrick-spotify \
//	BOOMBRICK_SPOTIFY_DIR=<a data dir's spotify/> go test -tags live -run Live -v ./internal/importer
func TestLiveMatch(t *testing.T) {
	path, dir := os.Getenv("BOOMBRICK_SPOTIFY_HELPER"), os.Getenv("BOOMBRICK_SPOTIFY_DIR")
	if path == "" || dir == "" {
		t.Skip("BOOMBRICK_SPOTIFY_HELPER and BOOMBRICK_SPOTIFY_DIR aren't set")
	}
	h := spotify.NewHelper(path, dir, slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard)
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	uri := spotify.LikedURI // or another playlist, named by BOOMBRICK_IMPORT_URI
	if u := os.Getenv("BOOMBRICK_IMPORT_URI"); u != "" {
		uri = u
	}
	var liked []spotify.Track
	var err error
	for deadline := time.Now().Add(40 * time.Second); ; time.Sleep(time.Second) {
		if liked, err = h.Tracks(ctx, uri); err == nil || time.Now().After(deadline) {
			break
		}
	}
	if err != nil || len(liked) == 0 {
		t.Fatalf("%d liked songs, %v", len(liked), err)
	}

	yt := ytmusic.NewClient(&http.Client{Timeout: 15 * time.Second})
	var queued []ytmusic.Song
	im, err := New(Options{DB: openDB(t), Search: yt.Search, Get: func(s []ytmusic.Song) { queued = append(queued, s...) },
		State:  func(string) (media.DownloadState, float32) { return media.Queued, 0 },
		Remove: func(string) {}, Online: func() bool { return true },
		Log: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	im.Import(uri, "Import", liked)
	for deadline := time.Now().Add(time.Duration(len(liked))*3*time.Second + 10*time.Second); ; time.Sleep(100 * time.Millisecond) {
		if j, _ := im.Job(uri); j.Count(Waiting) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still matching")
		}
	}
	j, _ := im.Job(uri)
	names := map[State]string{Matched: "matched", Unsure: "unsure", NotFound: "not found", Have: "have"}
	for _, it := range j.Items {
		line := names[it.State] + ": " + strings.Join(it.Track.Artists, ", ") + " - " + it.Track.Title
		if len(it.Candidates) > 0 {
			c := it.Candidates[0]
			line += "  ->  " + c.Song.Artist + " - " + c.Song.Title + " (" + c.Song.Duration.String() + ", " +
				strconv.FormatFloat(c.Score, 'f', 2, 64) + ")"
		}
		t.Log(line)
	}
	t.Logf("%d matched, %d unsure, %d not found; %d queued", j.Count(Matched), j.Count(Unsure), j.Count(NotFound), len(queued))
	if j.Count(Matched) == 0 {
		t.Error("nothing matched surely")
	}
}
