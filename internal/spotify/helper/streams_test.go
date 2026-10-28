package helper

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devgianlu/go-librespot/session"
)

// silence is an endless track of silence.
type silence struct {
	ms     atomic.Int64
	closed atomic.Bool
}

func (s *silence) SeekMs(ms int64) error         { s.ms.Store(ms); return nil }
func (s *silence) Read(p []float32) (int, error) { clear(p); return len(p), nil }
func (s *silence) Close() error                  { s.closed.Store(true); return nil }

// A request for a stream another is still reading takes it over, so a seek
// doesn't open the track again; one let go is kept for the next request.
func TestStreamsAreTakenOverAndKept(t *testing.T) {
	const key = "4Q0qVhFQa7j6jRKzo3HDmP/160/true"
	s := &Librespot{busy: map[string]*stream{}}
	first := s.adopt(key, &silence{}, 0, nil)
	ended := make(chan error, 1)
	go func() {
		buf := make([]float32, 64)
		for {
			if _, err := first.Read(buf); err != nil {
				first.Release()
				ended <- err
				return
			}
		}
	}()
	ctx := context.Background()
	if got := s.claim(ctx, key); got != first {
		t.Fatal("the stream being read wasn't taken over")
	}
	if err := <-ended; !errors.Is(err, errTaken) {
		t.Fatalf("its first reader ended with %v", err)
	}
	if _, err := first.Read(make([]float32, 4)); err != nil {
		t.Fatalf("reading after taking over: %v", err)
	}
	first.Release()
	if got := s.claim(ctx, key); got != first {
		t.Fatal("a stream let go wasn't kept")
	}
	if got := s.claim(ctx, "4Q0qVhFQa7j6jRKzo3HDmP/320/true"); got != nil {
		t.Fatal("claimed a stream at a quality never opened")
	}
}

// Streams let go for good are closed: those past keepMax, and those of a
// session that has gone.
func TestStreamsLetGoAreClosed(t *testing.T) {
	s := &Librespot{busy: map[string]*stream{}}
	var srcs []*silence
	for i := range keepMax + 1 {
		src := &silence{}
		srcs = append(srcs, src)
		s.adopt(string(rune('a'+i)), src, 0, nil).Release()
	}
	gone := &silence{}
	st := s.adopt("old", gone, 0, nil)
	st.sess = &session.Session{} // opened in a session since dropped
	st.Release()
	waitClosed(t, srcs[0], gone)
	for _, src := range srcs[1:] {
		if src.closed.Load() {
			t.Fatal("a stream kept was closed")
		}
	}
}

// short is a track that has ended.
type short struct{ silence }

func (s *short) Read(p []float32) (int, error) { return 0, io.EOF }

// A stream read to its end is closed when let go, not kept: by then it holds
// the whole file.
func TestStreamsReadToTheEndAreNotKept(t *testing.T) {
	s := &Librespot{busy: map[string]*stream{}}
	src := &short{}
	st := s.adopt("a", src, 0, nil)
	if _, err := st.Read(make([]float32, 4)); !errors.Is(err, io.EOF) {
		t.Fatalf("read %v", err)
	}
	st.Release()
	waitClosed(t, &src.silence)
	if got := s.claim(context.Background(), "a"); got != nil {
		t.Fatal("a stream read to its end was kept")
	}
}

func waitClosed(t *testing.T, srcs ...*silence) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for _, src := range srcs {
		for !src.closed.Load() {
			if time.Now().After(deadline) {
				t.Fatal("a stream let go wasn't closed")
			}
			time.Sleep(time.Millisecond)
		}
	}
}
