package art

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSized(t *testing.T) {
	cases := map[string]string{
		"https://yt3.googleusercontent.com/abc=w120-h120-l90-rj": "https://yt3.googleusercontent.com/abc=w73-h73-p-l90-rj",
		"https://lh3.googleusercontent.com/x=w120-h120-p-l90-rj": "https://lh3.googleusercontent.com/x=w73-h73-p-l90-rj",
		"https://i.ytimg.com/vi/abc/hqdefault.jpg":               "https://i.ytimg.com/vi/abc/hqdefault.jpg",
		"https://lh3.googleusercontent.com/no-options":           "https://lh3.googleusercontent.com/no-options",
		"https://example.com/picture?size=w120":                  "https://example.com/picture?size=w120",
	}
	for in, want := range cases {
		if got := Sized(in, 73); got != want {
			t.Errorf("Sized(%q) = %q, want %q", in, got, want)
		}
	}
}

// pictureServer serves a 100 x 60 JPEG, red on the left and blue on the
// right, or the status given, and counts requests.
func pictureServer(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := range 60 {
		for x := range 100 {
			c := color.RGBA{R: 255, A: 255}
			if x >= 50 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != http.StatusOK {
			http.Error(w, "no", status)
			return
		}
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func waitReady(t *testing.T, w *Web) {
	t.Helper()
	select {
	case <-w.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("no picture came")
	}
}

// waitMissing asks for url until Web reports there is none.
func waitMissing(t *testing.T, w *Web, url string) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if _, none := w.Cover(url, 40); none {
			return
		}
	}
	t.Fatal("never reported as missing")
}

func TestWebFetchesAtTheSizeAskedAndHandsOver(t *testing.T) {
	srv, hits := pictureServer(t, http.StatusOK)
	w := NewWeb(srv.Client(), nil, slog.New(slog.DiscardHandler))
	defer w.Close()
	url := srv.URL + "/thumb.jpg"
	if img, none := w.Cover(url, 40); img != nil || none {
		t.Fatalf("before fetching: %v, %v", img, none)
	}
	waitReady(t, w)
	img, none := w.Cover(url, 40)
	if img == nil || none || img.Rect.Dx() != 40 || img.Rect.Dy() != 40 {
		t.Fatalf("after: %v, %v", img, none)
	}
	// Cropped from the middle: red on the left, blue on the right.
	if l, r := img.NRGBAAt(2, 20), img.NRGBAAt(37, 20); l.R < 200 || r.B < 200 {
		t.Fatalf("left %v, right %v", l, r)
	}
	// Handed over once: asking again fetches it again.
	if img, _ := w.Cover(url, 40); img != nil {
		t.Fatal("handed over twice")
	}
	waitReady(t, w)
	if n := hits.Load(); n != 2 {
		t.Fatalf("%d requests", n)
	}
}

func TestWebRemembersFailures(t *testing.T) {
	srv, hits := pictureServer(t, http.StatusNotFound)
	w := NewWeb(srv.Client(), nil, slog.New(slog.DiscardHandler))
	defer w.Close()
	waitMissing(t, w, srv.URL+"/missing.jpg")
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d requests", n)
	}
}

func TestWebOffline(t *testing.T) {
	srv, hits := pictureServer(t, http.StatusOK)
	w := NewWeb(srv.Client(), func() bool { return false }, slog.New(slog.DiscardHandler))
	defer w.Close()
	waitMissing(t, w, srv.URL+"/thumb.jpg")
	if hits.Load() != 0 {
		t.Fatal("fetched while offline")
	}
}

// A picture missed while offline is fetched soon after the Wi-Fi is back, not
// only after the wait a failed one has.
func TestWebFetchesOnceBackOnline(t *testing.T) {
	srv, hits := pictureServer(t, http.StatusOK)
	var online atomic.Bool
	w := newWeb(srv.Client(), online.Load, slog.New(slog.DiscardHandler), 1)
	w.waits[1] = 20 * time.Millisecond
	defer w.Close()
	url := srv.URL + "/thumb.jpg"
	waitMissing(t, w, url)
	online.Store(true)
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if img, _ := w.Cover(url, 40); img != nil {
			if n := hits.Load(); n != 1 {
				t.Fatalf("%d requests", n)
			}
			return
		}
	}
	t.Fatal("not fetched once back online")
}

// The newest asked go first, and the oldest are dropped past maxAsked, as a
// fast scroll leaves them behind.
func TestWebQueue(t *testing.T) {
	w := newWeb(http.DefaultClient, nil, slog.New(slog.DiscardHandler), 0)
	defer w.Close()
	url := func(i int) string { return "https://example.com/" + string(rune('a'+i)) }
	for i := range maxAsked + 3 {
		w.Cover(url(i), 40)
	}
	if len(w.asked) != maxAsked || w.asked[0].url != url(3) {
		t.Fatalf("%d asked, the oldest %q", len(w.asked), w.asked[0].url)
	}
	if k, ok := w.take(); !ok || k.url != url(maxAsked+2) {
		t.Fatalf("took %q", k.url)
	}
	if w.Cover(url(3), 40); len(w.asked) != maxAsked-1 {
		t.Fatal("a picture already asked for was asked for twice")
	}
}

func TestWebClosingTwice(t *testing.T) {
	w := NewWeb(http.DefaultClient, nil, slog.New(slog.DiscardHandler))
	w.Close()
	w.Close()
}
