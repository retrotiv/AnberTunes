package app

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/resume"
)

// waitTake waits for the loader's read to finish and takes it.
func waitTake(t *testing.T, l *loader, final bool) loaded {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if r, ok := l.take(final); ok {
			return r
		}
	}
	t.Fatal("no read finished")
	return loaded{}
}

func TestLoaderQueuesOneReadBehindTheRunningOne(t *testing.T) {
	gate := make(chan struct{})
	var indexReads, progressReads atomic.Int32
	l := newLoader(
		func() (*library.Index, error) { indexReads.Add(1); <-gate; return library.NewIndex(nil), nil },
		func() ([]resume.Progress, error) { progressReads.Add(1); return nil, nil },
		func() {})
	l.start(true, false)
	l.start(false, false) // queued behind the first
	l.start(true, true)   // merged into the queued read, which now reads the index too
	gate <- struct{}{}
	r := waitTake(t, l, true)
	if r.ix == nil || r.final {
		t.Fatalf("first read = %+v; want the index, read before a scan ended", r)
	}
	gate <- struct{}{}
	r = waitTake(t, l, true)
	if r.ix == nil || !r.final {
		t.Fatalf("queued read = %+v; want the index, read after a scan ended", r)
	}
	if n, m := indexReads.Load(), progressReads.Load(); n != 2 || m != 2 {
		t.Fatalf("three asks gave %d index and %d progress reads; want 2 and 2", n, m)
	}
	time.Sleep(10 * time.Millisecond)
	if _, ok := l.take(true); ok {
		t.Fatal("a third read ran")
	}
}

func TestLoaderReadsOnlyProgressWhenAsked(t *testing.T) {
	l := newLoader(
		func() (*library.Index, error) { t.Error("the index was read"); return nil, nil },
		func() ([]resume.Progress, error) { return []resume.Progress{{Album: "Copper Season"}}, nil },
		func() {})
	l.start(false, true)
	if r := waitTake(t, l, true); r.ix != nil || len(r.progress) != 1 || r.err != nil {
		t.Fatalf("progress read = %+v", r)
	}
}

func TestLoaderStopsAtAnIndexError(t *testing.T) {
	bad := errors.New("disk")
	l := newLoader(
		func() (*library.Index, error) { return nil, bad },
		func() ([]resume.Progress, error) { t.Error("progress was read after an error"); return nil, nil },
		func() {})
	l.start(true, true)
	if r := waitTake(t, l, true); !errors.Is(r.err, bad) {
		t.Fatalf("read = %+v, want the index error", r)
	}
}

func TestReloadAfter(t *testing.T) {
	for _, c := range []struct {
		ended, changed, saved, shown bool
		read, index                  bool
	}{
		{read: false},
		{changed: true, shown: true, read: true, index: true},
		{ended: true, shown: true, read: false},
		{ended: true, shown: false, read: true, index: true},
		{saved: true, shown: true, read: true, index: false},
		{changed: true, saved: true, shown: true, read: true, index: true},
	} {
		read, index := reloadAfter(c.ended, c.changed, c.saved, c.shown)
		if read != c.read || index != c.index {
			t.Errorf("reloadAfter(ended %v, changed %v, saved %v, shown %v) = %v, %v; want %v, %v",
				c.ended, c.changed, c.saved, c.shown, read, index, c.read, c.index)
		}
	}
}
