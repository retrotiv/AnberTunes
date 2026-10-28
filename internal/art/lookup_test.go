package art

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/netx"
)

// fakeServices serves MusicBrainz searches from groups and covers for the
// ids in covers.
type fakeServices struct {
	mu       sync.Mutex
	groups   string            // the release-groups JSON array
	covers   map[string]string // release group id -> body
	queries  []string
	coverHit []string
	status   int // when set, MusicBrainz answers with it
}

func (f *fakeServices) start(t *testing.T) *Lookup {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Boombrick/") {
			t.Errorf("request without the user agent: %s", r.URL)
		}
		switch {
		case r.URL.Path == "/ws/2/release-group/":
			f.queries = append(f.queries, r.URL.Query().Get("query"))
			if r.URL.Query().Get("fmt") != "json" {
				t.Errorf("search without fmt=json: %s", r.URL)
			}
			if f.status != 0 {
				w.WriteHeader(f.status)
				return
			}
			fmt.Fprintf(w, `{"release-groups": %s}`, f.groups)
		case strings.HasPrefix(r.URL.Path, "/release-group/") && strings.HasSuffix(r.URL.Path, "/front-500"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/release-group/"), "/front-500")
			f.coverHit = append(f.coverHit, id)
			body, ok := f.covers[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(body))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	l := NewLookup(netx.NewClient())
	l.MusicBrainz, l.CoverArt = srv.URL, srv.URL
	l.sleep = func(context.Context, time.Duration) error { return nil }
	return l
}

// asked returns the searches and cover fetches the server has seen. The
// handler runs on the server's goroutines, so read them under the lock.
func (f *fakeServices) asked() (queries, covers []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.queries), slices.Clone(f.coverHit)
}

func group(id string, score int, title string, credits ...string) string {
	var cs []string
	for i, c := range credits {
		join := ""
		if i < len(credits)-1 {
			join = " & "
		}
		cs = append(cs, fmt.Sprintf(`{"name": %q, "joinphrase": %q}`, c, join))
	}
	return fmt.Sprintf(`{"id": %q, "score": %d, "title": %q, "artist-credit": [%s]}`, id, score, title, strings.Join(cs, ","))
}

// typed gives the release group g a primary type and secondary types.
func typed(g, primary string, secondary ...string) string {
	qs := []string{}
	for _, s := range secondary {
		qs = append(qs, fmt.Sprintf("%q", s))
	}
	return fmt.Sprintf(`{"primary-type": %q, "secondary-types": [%s], `, primary, strings.Join(qs, ",")) + g[1:]
}

func TestFindPrefersTheAlbum(t *testing.T) {
	for _, c := range []struct {
		name   string
		groups []string
		want   string
	}{
		{"before a single", []string{
			typed(group("rg-single", 100, "Thriller", "Michael Jackson"), "Single"),
			typed(group("rg-album", 100, "Thriller", "Michael Jackson"), "Album"),
		}, "rg-album"},
		{"before a live album and an EP", []string{
			typed(group("rg-ep", 100, "Thriller", "Michael Jackson"), "EP"),
			typed(group("rg-live", 100, "Thriller", "Michael Jackson"), "Album", "Live"),
			typed(group("rg-album", 95, "Thriller", "Michael Jackson"), "Album"),
		}, "rg-album"},
		{"a live album before an EP", []string{
			typed(group("rg-ep", 100, "Thriller", "Michael Jackson"), "EP"),
			typed(group("rg-live", 95, "Thriller", "Michael Jackson"), "Album", "Live"),
		}, "rg-live"},
		{"a single when nothing else matches", []string{
			typed(group("rg-other", 100, "Thriller Remixes", "Michael Jackson"), "Album"),
			typed(group("rg-single", 100, "Thriller", "Michael Jackson"), "Single"),
		}, "rg-single"},
	} {
		covers := map[string]string{}
		for _, id := range []string{"rg-single", "rg-album", "rg-ep", "rg-live", "rg-other"} {
			covers[id] = id
		}
		f := &fakeServices{groups: "[" + strings.Join(c.groups, ",") + "]", covers: covers}
		l := f.start(t)
		if data, id, err := l.Find(context.Background(), "Michael Jackson", "Thriller"); err != nil || id != c.want || string(data) != c.want {
			t.Errorf("%s: Find = %q, %q, %v; want %s", c.name, data, id, err, c.want)
		}
	}
}

// Albums of one name by one artist, as a self-titled series, can't be told
// apart: better no cover than another album's.
func TestFindGivesUpOnTiedAlbums(t *testing.T) {
	f := &fakeServices{
		groups: "[" + typed(group("rg-red", 100, "Weezer", "Weezer"), "Album") + "," +
			typed(group("rg-green", 100, "Weezer", "Weezer"), "Album") + "," +
			typed(group("rg-single", 100, "Weezer", "Weezer"), "Single") + "]",
		covers: map[string]string{"rg-red": "red", "rg-green": "green", "rg-single": "single"},
	}
	l := f.start(t)
	if data, id, err := l.Find(context.Background(), "Weezer", "Weezer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find = %q, %q, %v; want ErrNotFound", data, id, err)
	}
	if _, covers := f.asked(); len(covers) != 0 {
		t.Fatalf("fetched covers %v", covers)
	}
}

// A missing cover moves on to the next album, but never down to a single.
func TestFindStaysWithTheBestKind(t *testing.T) {
	f := &fakeServices{
		groups: "[" + typed(group("rg-1", 100, "Thriller", "Michael Jackson"), "Album") + "," +
			typed(group("rg-single", 100, "Thriller", "Michael Jackson"), "Single") + "]",
		covers: map[string]string{"rg-single": "single"},
	}
	l := f.start(t)
	if data, id, err := l.Find(context.Background(), "Michael Jackson", "Thriller"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find = %q, %q, %v; want ErrNotFound", data, id, err)
	}
}

func TestFindFetchesTheMatchingCover(t *testing.T) {
	f := &fakeServices{
		groups: "[" + group("rg-1", 100, "Copper Season", "Odalys") + "]",
		covers: map[string]string{"rg-1": "jpeg bytes"},
	}
	l := f.start(t)
	data, id, err := l.Find(context.Background(), "Odalys", "Copper Season")
	if err != nil || string(data) != "jpeg bytes" || id != "rg-1" {
		t.Fatalf("Find = %q, %q, %v", data, id, err)
	}
	if q, _ := f.asked(); q[0] != `releasegroup:"Copper Season" AND artist:"Odalys"` {
		t.Fatalf("query %q", q)
	}
}

func TestFindSkipsPoorMatches(t *testing.T) {
	for _, c := range []struct {
		name, groups string
	}{
		{"another artist", group("rg-1", 100, "Copper Season", "The Hollow Pines")},
		{"another title", group("rg-1", 100, "Copper Season II", "Odalys")},
		{"a low score", group("rg-1", 80, "Copper Season", "Odalys")},
		{"no results", ""},
	} {
		f := &fakeServices{groups: "[" + c.groups + "]", covers: map[string]string{"rg-1": "x"}}
		l := f.start(t)
		if _, _, err := l.Find(context.Background(), "Odalys", "Copper Season"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: Find = %v, want ErrNotFound", c.name, err)
		}
		if _, covers := f.asked(); len(covers) != 0 {
			t.Errorf("%s: fetched covers %v", c.name, covers)
		}
	}
}

func TestFindTriesTheNextGroupWithoutACover(t *testing.T) {
	f := &fakeServices{
		groups: "[" + group("rg-1", 100, "Copper Season", "Odalys") + "," + group("rg-2", 95, "Copper Season", "Odalys") + "]",
		covers: map[string]string{"rg-2": "second"},
	}
	l := f.start(t)
	if data, id, err := l.Find(context.Background(), "Odalys", "Copper Season"); err != nil || string(data) != "second" || id != "rg-2" {
		t.Fatalf("Find = %q, %q, %v", data, id, err)
	}
}

func TestFindReportsABusyService(t *testing.T) {
	f := &fakeServices{status: http.StatusServiceUnavailable}
	l := f.start(t)
	_, _, err := l.Find(context.Background(), "Odalys", "Copper Season")
	var se *netx.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusServiceUnavailable {
		t.Fatalf("Find against a busy MusicBrainz = %v", err)
	}
}

func TestFindEscapesTheQuery(t *testing.T) {
	f := &fakeServices{groups: "[]"}
	l := f.start(t)
	l.Find(context.Background(), `AC\DC`, `Say "Hi"`)
	if q, _ := f.asked(); q[0] != `releasegroup:"Say \"Hi\"" AND artist:"AC\\DC"` {
		t.Fatalf("query %q", q)
	}
}

func TestFindIgnoresEmptyNames(t *testing.T) {
	f := &fakeServices{groups: "[]"}
	l := f.start(t)
	for _, c := range [][2]string{{"", "X"}, {"A", " "}} {
		if _, _, err := l.Find(context.Background(), c[0], c[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("Find(%q, %q) = %v, want ErrNotFound", c[0], c[1], err)
		}
	}
	if q, covers := f.asked(); len(q) != 0 || len(covers) != 0 {
		t.Fatalf("requests made: %v %v", q, covers)
	}
}

func TestFindWaitsBetweenSearches(t *testing.T) {
	f := &fakeServices{groups: "[]"}
	l := f.start(t)
	now := time.Unix(1000, 0)
	var waits []time.Duration
	l.now = func() time.Time { return now }
	l.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); now = now.Add(d); return nil }
	l.Find(context.Background(), "A", "B")
	l.Find(context.Background(), "A", "C")
	now = now.Add(5 * time.Second)
	l.Find(context.Background(), "A", "D")
	if len(waits) != 3 || waits[0] != 0 || waits[1] != mbGap || waits[2] != 0 {
		t.Fatalf("waits %v; want 0, %v, 0", waits, mbGap)
	}
}

func TestSameName(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		title bool
		same  bool
	}{
		{"Copper Season", "copper season", false, true},
		{"The Hollow Pines", "Hollow Pines", false, true},
		{"Beyoncé", "Beyonce", false, true},
		{"Simon & Garfunkel", "Simon and Garfunkel", false, true},
		{"AC/DC", "ACDC", false, true},
		{"Abbey Road (Remastered 2009)", "Abbey Road", true, true},
		{"Abbey Road [Deluxe]", "Abbey Road", true, true},
		{"Abbey Road (Remastered 2009)", "Abbey Road", false, false},
		{"Abbey Road", "Let It Be", true, false},
		{"The", "the", false, true},
		{"Greatest Hits (Vol. 2)", "Greatest Hits (Vol. 1)", true, false},
		{"(Live)", "(Demo)", true, false},
		{"Abbey Road", "Abbey Road (Remastered)", true, true},
		{"÷", "+", false, false},
		{"÷", "÷", false, true},
		{"!!!", "???", false, false},
	} {
		if got := sameName(c.a, c.b, c.title); got != c.same {
			t.Errorf("sameName(%q, %q, title %v) = %v", c.a, c.b, c.title, got)
		}
	}
}
