//go:build live

package ytmusic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/netx"
)

// TestLive asks the real YouTube Music. With BOOMBRICK_RECORD=1 it saves the
// responses into testdata for TestRecordedResponses.
func TestLive(t *testing.T) {
	c := NewClient(netx.NewClient())
	file := ""
	if os.Getenv("BOOMBRICK_RECORD") != "" {
		c.raw = func(_ string, body []byte) {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join("testdata", file), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var albumID string
	for _, s := range []struct {
		file string
		f    Filter
	}{{"search-all.json", All}, {"search-songs.json", Songs}, {"search-albums.json", Albums}, {"search-artists.json", Artists}} {
		file = s.file
		items, err := c.Search(ctx, "radiohead ok computer", s.f)
		if err != nil || checkItems(items) == 0 {
			t.Fatalf("%s: %d results, %v", FilterNames[s.f], len(items), err)
		}
		if s.f == Albums {
			albumID = items[0].ID
		}
	}
	file = "album.json"
	a, err := c.Album(ctx, albumID)
	if err != nil || len(a.Tracks) == 0 {
		t.Fatalf("album %s: %+v, %v", albumID, a, err)
	}
	file = "artist.json"
	ar, err := c.Artist(ctx, a.ArtistID)
	if err != nil || len(ar.Albums) == 0 {
		t.Fatalf("artist %s: %+v, %v", a.ArtistID, ar, err)
	}
	t.Logf("album %q by %s: %d tracks; %s has %d releases", a.Title, a.Artist, len(a.Tracks), ar.Name, len(ar.Albums))
}
