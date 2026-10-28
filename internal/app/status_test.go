package app

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ujjwal313/boombrick/internal/platform"
)

func TestWatchStatus(t *testing.T) {
	var mu sync.Mutex
	reading := platform.Status{Battery: 80}
	reads := 0
	read := func() platform.Status {
		mu.Lock()
		defer mu.Unlock()
		reads++
		return reading
	}
	set := func(st platform.Status) {
		mu.Lock()
		defer mu.Unlock()
		reading = st
	}
	out := make(chan platform.Status, 1)
	wakes := make(chan struct{}, 16)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchStatus(read, 5*time.Millisecond, out, func() { wakes <- struct{}{} }, nil, stop)
	}()

	next := func(what string) platform.Status {
		t.Helper()
		select {
		case <-wakes:
		case <-time.After(2 * time.Second):
			t.Fatalf("no wake-up for %s", what)
		}
		return <-out
	}
	if got := next("the first reading"); got != (platform.Status{Battery: 80}) {
		t.Fatalf("first reading %+v", got)
	}
	// Unchanged readings aren't handed over.
	for {
		mu.Lock()
		n := reads
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case st := <-out:
		t.Fatalf("an unchanged reading was handed over: %+v", st)
	default:
	}
	set(platform.Status{Battery: 79, Online: true})
	if got := next("a change"); got != (platform.Status{Battery: 79, Online: true}) {
		t.Fatalf("after a change %+v", got)
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher didn't stop")
	}
}

// A reading the loop hasn't taken is replaced by the next, not queued behind
// it, and the watcher never blocks on it.
func TestWatchStatusKeepsOnlyTheLatest(t *testing.T) {
	var mu sync.Mutex
	battery := 50
	read := func() platform.Status {
		mu.Lock()
		defer mu.Unlock()
		battery--
		return platform.Status{Battery: battery}
	}
	out := make(chan platform.Status, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchStatus(read, time.Millisecond, out, func() {}, nil, stop)
	}()
	time.Sleep(30 * time.Millisecond)
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher blocked with a reading waiting")
	}
	mu.Lock()
	last := battery
	mu.Unlock()
	if got := <-out; got.Battery != last {
		t.Fatalf("waiting reading %d, want the latest, %d", got.Battery, last)
	}
}

// With the screen off nothing is read; as it comes back on the status is read
// at once, and every interval again after.
func TestWatchStatusWaitsWhileDark(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		reads := 0
		read := func() platform.Status {
			mu.Lock()
			defer mu.Unlock()
			reads++
			return platform.Status{Battery: 80}
		}
		count := func() int { mu.Lock(); defer mu.Unlock(); return reads }
		out := make(chan platform.Status, 1)
		lit, stop, done := make(chan bool, 1), make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			watchStatus(read, statusEvery, out, func() {}, lit, stop)
		}()
		after := func(d time.Duration) int {
			time.Sleep(d)
			synctest.Wait()
			return count()
		}
		if n := after(statusEvery + time.Millisecond); n != 2 {
			t.Fatalf("%d readings in the first 10 s, want 2", n)
		}
		lit <- false
		if n := after(time.Hour); n != 2 {
			t.Fatalf("%d readings with the screen off, want none", n-2)
		}
		lit <- true
		if n := after(0); n != 3 {
			t.Fatalf("%d readings as the screen came on, want 1", n-2)
		}
		if n := after(statusEvery + time.Millisecond); n != 4 {
			t.Fatalf("%d readings 10 s after, want 1", n-3)
		}
		close(stop)
		<-done
	})
}
