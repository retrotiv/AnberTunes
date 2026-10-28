package lyrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/netx"
)

var rust = Track{Path: "/m/1.flac", Title: "Rust", Artist: "Odalys", Album: "Copper Season", Duration: 222 * time.Second}

// lrclib serves get and search from canned bodies; an empty get body is a
// 404. The function it returns lists the requests made, read under a lock
// since the handler runs on the server's goroutines.
func lrclib(t *testing.T, get, search string, status int) (*Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Boombrick/") {
			t.Errorf("no user agent on %s", r.URL)
		}
		mu.Lock()
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		switch r.URL.Path {
		case "/api/get":
			if get == "" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(get))
		case "/api/search":
			w.Write([]byte(search))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient(netx.NewClient())
	c.Base = srv.URL
	return c, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
}

func TestGetExact(t *testing.T) {
	c, asked := lrclib(t, `{"instrumental": false, "plainLyrics": "Rust", "syncedLyrics": "[00:01.00]Rust"}`, "[]", 0)
	r, err := c.Get(context.Background(), rust)
	if err != nil || r.Synced != "[00:01.00]Rust" || r.Plain != "Rust" {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	want := "/api/get?album_name=Copper+Season&artist_name=Odalys&duration=222&track_name=Rust"
	if a := asked(); len(a) != 1 || a[0] != want {
		t.Fatalf("asked %v, want %s", a, want)
	}
}

func TestGetFallsBackToSearch(t *testing.T) {
	search := `[
		{"trackName": "Rust", "artistName": "Odalys", "duration": 180, "syncedLyrics": "[00:01.00]Wrong length"},
		{"trackName": "Rust", "artistName": "Odalys", "duration": 223.4, "plainLyrics": "Plain only"},
		{"trackName": "Rust (Remastered)", "artistName": "odalys ", "duration": 221.0, "plainLyrics": "Rust", "syncedLyrics": "[00:01.00]Rust"}
	]`
	c, asked := lrclib(t, "", search, 0)
	r, err := c.Get(context.Background(), rust)
	if err != nil || r.Synced != "[00:01.00]Rust" {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	if a := asked(); len(a) != 2 || !strings.HasPrefix(a[1], "/api/search?") {
		t.Fatalf("asked %v", a)
	}
}

// A search result as long as the track is still another song unless its
// title or artist is the track's.
func TestGetWithALengthWantsTheTitleOrArtist(t *testing.T) {
	search := `[
		{"trackName": "Something Else", "artistName": "Someone", "duration": 222, "syncedLyrics": "[00:01.00]Wrong song"},
		{"trackName": " RUST", "artistName": "Odalys & Friends", "duration": 222.5, "plainLyrics": "Right song"}
	]`
	c, _ := lrclib(t, "", search, 0)
	if r, err := c.Get(context.Background(), rust); err != nil || r.Plain != "Right song" {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	c, _ = lrclib(t, "", `[{"trackName": "Something Else", "artistName": "Someone", "duration": 222, "plainLyrics": "x"}]`, 0)
	if _, err := c.Get(context.Background(), rust); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestGetWithoutALengthSearches(t *testing.T) {
	search := `[
		{"trackName": "Rust (Live)", "artistName": "Odalys", "syncedLyrics": "[00:01.00]Live"},
		{"trackName": " rust ", "artistName": "ODALYS", "plainLyrics": "Any"}
	]`
	c, asked := lrclib(t, `{"syncedLyrics": "x"}`, search, 0)
	tr := rust
	tr.Duration = 0
	if r, err := c.Get(context.Background(), tr); err != nil || r.Plain != "Any" {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	if a := asked(); len(a) != 1 || !strings.HasPrefix(a[0], "/api/search?") {
		t.Fatalf("asked %v", a)
	}
}

func TestGetWithoutALengthWantsTheSameSong(t *testing.T) {
	c, _ := lrclib(t, "", `[{"trackName": "Rusty", "artistName": "Odalys", "plainLyrics": "Other song"}]`, 0)
	tr := rust
	tr.Duration = 0
	if _, err := c.Get(context.Background(), tr); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestGetEmptyExactSearches(t *testing.T) {
	search := `[{"trackName": "Rust", "artistName": "Odalys", "duration": 222, "plainLyrics": "Rust"}]`
	c, asked := lrclib(t, `{"instrumental": false, "plainLyrics": "", "syncedLyrics": ""}`, search, 0)
	r, err := c.Get(context.Background(), rust)
	if err != nil || r.Plain != "Rust" {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	if a := asked(); len(a) != 2 {
		t.Fatalf("asked %v", a)
	}
}

func TestGetEscapesTheQuery(t *testing.T) {
	c, asked := lrclib(t, `{"plainLyrics": "x"}`, "[]", 0)
	tr := rust
	tr.Title = "Rock & Roll #1 + Café"
	if _, err := c.Get(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
	a := asked()
	_, raw, _ := strings.Cut(a[0], "?")
	q, err := url.ParseQuery(raw)
	if err != nil || q.Get("track_name") != tr.Title || q.Get("artist_name") != "Odalys" {
		t.Fatalf("query %q decodes to %v, %v", raw, q, err)
	}
}

func TestGetFindsNothing(t *testing.T) {
	c, _ := lrclib(t, "", `[{"duration": 100, "syncedLyrics": "[00:01.00]Too short"}]`, 0)
	if _, err := c.Get(context.Background(), rust); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestGetInstrumental(t *testing.T) {
	c, _ := lrclib(t, `{"instrumental": true}`, "[]", 0)
	if r, err := c.Get(context.Background(), rust); err != nil || !r.Instrumental {
		t.Fatalf("Get = %+v, %v", r, err)
	}
}

func TestGetServerError(t *testing.T) {
	c, _ := lrclib(t, "", "", http.StatusInternalServerError)
	var se *netx.StatusError
	if _, err := c.Get(context.Background(), rust); !errors.As(err, &se) || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want the status error", err)
	}
}
