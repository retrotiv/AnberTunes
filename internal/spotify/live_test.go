//go:build live

package spotify_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/audio"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/store"
)

// realTime is an output that takes as long to write audio as it lasts.
type realTime struct{}

func (realTime) Open(int) error { return nil }
func (realTime) Write(pcm []int16) error {
	time.Sleep(time.Duration(len(pcm)/2) * time.Second / audio.OutputRate)
	return nil
}
func (realTime) Drop() (int, error) { return 0, nil }
func (realTime) Close() error       { return nil }

// Plays the first two Liked Songs through the player, as Boombrick does:
// the first from its start, then from 15 s before its end, so the second
// opens near the end and follows it.
//
//	BOOMBRICK_SPOTIFY_HELPER=dist/desktop/bin/boombrick-spotify \
//	BOOMBRICK_SPOTIFY_DIR=<a data dir's spotify/> go test -tags live -run Live -v ./internal/spotify
func TestLivePlayback(t *testing.T) {
	path, dir := os.Getenv("BOOMBRICK_SPOTIFY_HELPER"), os.Getenv("BOOMBRICK_SPOTIFY_DIR")
	if path == "" || dir == "" {
		t.Skip("BOOMBRICK_SPOTIFY_HELPER and BOOMBRICK_SPOTIFY_DIR aren't set")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	h := spotify.NewHelper(path, dir, log, os.Stderr)
	defer h.Close()
	ctx := context.Background()

	st, err := h.Status(ctx)
	for deadline := time.Now().Add(30 * time.Second); err == nil && st.State == spotify.Connecting && time.Now().Before(deadline); {
		time.Sleep(time.Second)
		st, err = h.Status(ctx)
	}
	if err != nil || st.State != spotify.Ready {
		t.Fatalf("status %+v, %v", st, err)
	}
	liked, err := h.Tracks(ctx, spotify.LikedURI)
	if err != nil || len(liked) < 2 {
		t.Fatalf("%d liked songs, %v", len(liked), err)
	}
	var q []player.Track
	for _, s := range liked[:2] {
		q = append(q, player.Track{SpotifyID: s.ID, Title: s.Title, Duration: time.Duration(s.Duration) * time.Millisecond})
	}

	var mu sync.Mutex
	var notices []player.Notice
	p := player.New(realTime{}, func(string) (audio.Source, error) { return nil, errors.New("no files here") },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer p.Close()
	p.SetStreaming(func(ctx context.Context, t player.Track, frame int64, level bool) (audio.Source, error) {
		s, err := h.Open(ctx, t.SpotifyID, frame, spotify.DefaultKbps, level)
		if err != nil {
			return nil, err
		}
		return s, nil
	}, func(n player.Notice) {
		mu.Lock()
		defer mu.Unlock()
		notices = append(notices, n)
	})

	start := time.Now()
	p.Play(q, 0)
	wait(t, 15*time.Second, "the first song to play", func() bool {
		s := p.State()
		return !s.Loading && s.Playing && s.Elapsed > 0
	})
	t.Logf("%q plays %v after Play", q[0].Title, time.Since(start).Round(time.Millisecond))
	time.Sleep(3 * time.Second)
	if s := p.State(); s.Elapsed < 2*time.Second || s.Elapsed > 4*time.Second || s.Kbps != spotify.DefaultKbps {
		t.Fatalf("after 3 s: %v played at %d kbps", s.Elapsed, s.Kbps)
	}

	p.Seek(q[0].Duration - 15*time.Second)
	wait(t, 25*time.Second, "the second song to follow", func() bool { return p.State().Index == 1 })
	wait(t, 5*time.Second, "the second song to play", func() bool {
		s := p.State()
		return s.Playing && s.Elapsed > time.Second
	})
	t.Logf("%q follows", q[1].Title)
	mu.Lock()
	defer mu.Unlock()
	if len(notices) > 0 {
		t.Fatalf("notices %+v", notices)
	}
}

func wait(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(d); !cond(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// Asks the library for the account, the playlists and Liked Songs through
// the helper, with the names Spotify gives for their owners.
func TestLiveLibrary(t *testing.T) {
	path, dir := os.Getenv("BOOMBRICK_SPOTIFY_HELPER"), os.Getenv("BOOMBRICK_SPOTIFY_DIR")
	if path == "" || dir == "" {
		t.Skip("BOOMBRICK_SPOTIFY_HELPER and BOOMBRICK_SPOTIFY_DIR aren't set")
	}
	h := spotify.NewHelper(path, dir, slog.New(slog.NewTextHandler(os.Stderr, nil)), os.Stderr)
	defer h.Close()
	s, err := store.Open(filepath.Join(t.TempDir(), "boombrick.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib := spotify.NewLibrary(h, s.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer lib.Close()

	wait(t, 40*time.Second, "the account's name", func() bool { st, _ := lib.Status(); return st.Name != "" })
	st, _ := lib.Status()
	t.Logf("signed in as %q (premium %v)", st.Name, st.Premium)
	wait(t, 30*time.Second, "the playlists", func() bool { _, have, _ := lib.Playlists(); return have })
	pls, _, _ := lib.Playlists()
	spotifys := 0
	for _, p := range pls {
		t.Logf("%s · %d songs · %s", p.Name, p.Count, p.Owner)
		if p.Owner == "Spotify" {
			spotifys++
		}
	}
	if spotifys == 0 {
		t.Error("no playlist is Spotify's own, or its owner wasn't named")
	}
	wait(t, 30*time.Second, "Liked Songs", func() bool { _, have, _ := lib.Tracks(spotify.LikedURI); return have })
	liked, _, _ := lib.Tracks(spotify.LikedURI)
	t.Logf("%d liked songs", len(liked))
}
