package spotify

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEnv makes the test binary run as a fake helper, as NewHelper starts it.
const fakeEnv = "BOOMBRICK_FAKE_SPOTIFY_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) != "" && len(os.Args) == 4 && os.Args[1] == "serve" {
		fakeHelper(os.Args[3])
		return
	}
	os.Exit(m.Run())
}

// fakeLength is every fake track's length: two seconds.
const fakeLength = 2 * Rate

// fakeHelper answers on socket until its input closes. Track "t1" plays
// frames whose samples are their own index over 1e6; "cut" breaks off after
// 30000 frames the first time; "gone", "busy" and "out" are refused.
func fakeHelper(socket string) {
	var streams atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Status{State: Ready, User: "sam", Premium: true})
	})
	mux.HandleFunc("GET /tracks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Track{{ID: "t1", Title: r.URL.Query().Get("uri")}})
	})
	mux.HandleFunc("GET /streams", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strconv.Itoa(int(streams.Load()))))
	})
	mux.HandleFunc("POST /crash", func(w http.ResponseWriter, r *http.Request) { os.Exit(3) })
	mux.HandleFunc("GET /stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		n := streams.Add(1)
		switch r.PathValue("id") {
		case "gone":
			http.Error(w, "restricted", http.StatusForbidden)
			return
		case "busy":
			w.Header().Set("Retry-After", "40")
			http.Error(w, "key refused", http.StatusTooManyRequests)
			return
		case "out":
			http.Error(w, "signed out", http.StatusUnauthorized)
			return
		}
		ms, _ := strconv.ParseInt(r.URL.Query().Get("ms"), 10, 64)
		w.Header().Set(HeaderRate, strconv.Itoa(Rate))
		w.Header().Set(HeaderDuration, strconv.Itoa(fakeLength*1000/Rate))
		buf := make([]byte, 8)
		for f := ms * Rate / 1000; f < fakeLength; f++ {
			if r.PathValue("id") == "cut" && n == 1 && f == 30000 {
				panic(http.ErrAbortHandler)
			}
			v := math.Float32bits(float32(f) / 1e6)
			binary.LittleEndian.PutUint32(buf, v)
			binary.LittleEndian.PutUint32(buf[4:], v)
			if _, err := w.Write(buf); err != nil {
				return
			}
		}
	})
	os.Remove(socket)
	l, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(1)
	}
	go http.Serve(l, mux)
	io.Copy(io.Discard, os.Stdin)
	os.Remove(socket)
}

func newHelper(t *testing.T, path string) *Helper {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	h := NewHelper(path, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard)
	h.socket = t.TempDir() + "/spotify.sock"
	t.Cleanup(func() { h.Close() })
	return h
}

func TestHelperStartsAnswersAndStops(t *testing.T) {
	h := newHelper(t, os.Args[0])
	ctx := context.Background()
	st, err := h.Status(ctx)
	if err != nil || st.State != Ready || st.User != "sam" {
		t.Fatalf("status %+v, %v", st, err)
	}
	ts, err := h.Tracks(ctx, LikedURI)
	if err != nil || len(ts) != 1 || ts[0].Title != LikedURI {
		t.Fatalf("tracks %+v, %v", ts, err)
	}
	h.mu.Lock()
	exited := h.exited
	h.mu.Unlock()
	h.Close()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the helper didn't stop")
	}
}

// After a crash the helper isn't started again until its pause is over.
func TestHelperRestartsAfterAPause(t *testing.T) {
	h := newHelper(t, os.Args[0])
	h.backoff = func(int) time.Duration { return 300 * time.Millisecond }
	ctx := context.Background()
	if _, err := h.Status(ctx); err != nil {
		t.Fatal(err)
	}
	h.do(ctx, http.MethodPost, "/crash")
	for h.isRunning() {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := h.Status(ctx); !errors.Is(err, ErrNoHelper) {
		t.Fatalf("asked during the pause: %v", err)
	}
	time.Sleep(350 * time.Millisecond)
	if st, err := h.Status(ctx); err != nil || st.State != Ready {
		t.Fatalf("after the pause: %+v, %v", st, err)
	}
}

func (h *Helper) isRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

func TestMissingHelper(t *testing.T) {
	h := newHelper(t, t.TempDir()+"/no-such-helper")
	if _, err := h.Status(context.Background()); !errors.Is(err, ErrNoHelper) {
		t.Fatalf("status from a missing helper: %v", err)
	}
	if h.backoff(1) != time.Second || h.backoff(3) != 4*time.Second || h.backoff(20) != time.Minute {
		t.Fatal("the pause doesn't double up to a minute")
	}
}

// readAll reads s to its end, checking each frame is the one after the last.
func readAll(t *testing.T, s *Stream, from int64) int64 {
	t.Helper()
	buf := make([]float32, 2*1000)
	next := from
	deadline := time.Now().Add(10 * time.Second)
	for {
		n, err := s.Read(buf)
		for i := range n {
			if got := int64(math.Round(float64(buf[2*i]) * 1e6)); got != next {
				t.Fatalf("frame %d where %d was due", got, next)
			}
			next++
		}
		if errors.Is(err, io.EOF) {
			return next
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("stuck at frame %d", next)
		}
	}
}

func streams(t *testing.T, h *Helper) int {
	t.Helper()
	resp, err := h.do(context.Background(), http.MethodGet, "/streams")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	n, _ := strconv.Atoi(string(b))
	return n
}

func TestStreamPlaysToTheEnd(t *testing.T) {
	h := newHelper(t, os.Args[0])
	s, err := h.Open(context.Background(), "t1", 0, 160, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.SampleRate() != Rate || s.Frames() != fakeLength || s.Kbps() != 160 {
		t.Fatalf("rate %d, frames %d, kbps %d", s.SampleRate(), s.Frames(), s.Kbps())
	}
	if end := readAll(t, s, 0); end != fakeLength {
		t.Fatalf("ended at %d", end)
	}
}

// Opened part way it starts at exactly the frame asked for, though the
// helper starts at a whole millisecond; seeking to where it is asks nothing,
// elsewhere it asks the helper from there.
func TestStreamStartsAndSeeksToTheFrame(t *testing.T) {
	h := newHelper(t, os.Args[0])
	s, err := h.Open(context.Background(), "t1", 44123, 160, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SeekFrame(44123)
	if n := streams(t, h); n != 1 {
		t.Fatalf("%d requests for a seek to where it was", n)
	}
	s.SeekFrame(70000)
	if end := readAll(t, s, 70000); end != fakeLength {
		t.Fatalf("ended at %d", end)
	}
	if n := streams(t, h); n != 2 {
		t.Fatalf("%d requests", n)
	}
}

// A stream that breaks part way is picked up from where it got to, with no
// frame lost or repeated.
func TestStreamPicksUpWhereItBroke(t *testing.T) {
	h := newHelper(t, os.Args[0])
	s, err := h.Open(context.Background(), "cut", 0, 160, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if end := readAll(t, s, 0); end != fakeLength {
		t.Fatalf("ended at %d", end)
	}
	if n := streams(t, h); n != 2 {
		t.Fatalf("%d requests", n)
	}
}

func TestStreamRefusals(t *testing.T) {
	h := newHelper(t, os.Args[0])
	ctx := context.Background()
	if _, err := h.Open(ctx, "gone", 0, 160, false); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("restricted: %v", err)
	}
	var th *Throttled
	if _, err := h.Open(ctx, "busy", 0, 160, false); !errors.As(err, &th) || th.Wait != 40*time.Second {
		t.Fatalf("throttled: %v", err)
	}
	if _, err := h.Open(ctx, "out", 0, 160, false); !errors.Is(err, ErrSignedOut) {
		t.Fatalf("signed out: %v", err)
	}
}
