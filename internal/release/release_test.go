package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const latestJSON = `{"tag_name": "v0.5.0", "name": "0.5.0", "assets": [
	{"name": "Boombrick.pak.zip", "browser_download_url": "https://example.com/Boombrick.pak.zip"},
	{"name": "SHA256SUMS", "browser_download_url": "https://example.com/SHA256SUMS"}]}`

// github serves the latest release of ujjwal313/boombrick and counts asks.
func github(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var asks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asks.Add(1)
		if r.URL.Path != "/repos/ujjwal313/boombrick/releases/latest" || r.Header.Get("User-Agent") == "" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(latestJSON))
	}))
	t.Cleanup(srv.Close)
	return srv, &asks
}

func TestLatest(t *testing.T) {
	srv, _ := github(t, http.StatusOK)
	r, err := Latest(context.Background(), srv.Client(), srv.URL, "ujjwal313/boombrick")
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v0.5.0" || r.Version() != "0.5.0" || r.Assets["SHA256SUMS"] != "https://example.com/SHA256SUMS" {
		t.Fatalf("release %+v", r)
	}
	if _, err := Latest(context.Background(), srv.Client(), srv.URL, "someone/else"); err == nil {
		t.Fatal("a missing repo gave no error")
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.5.0", "0.4.0", true},
		{"0.4.0", "0.5.0", false},
		{"0.10.0", "0.9.2", true},
		{"0.5.0", "0.5.0", false},
		{"2026.10.11.1", "2026.10.11", true},
		{"2026.10.11", "2026.09.27", true},
		{"0.5.0", "dev", true},
		{"0.5.0", "", true},
		{"dev", "dev", false},
		{"dev", "0.5.0", false},
		// Betas come between the version before and their own.
		{"0.2.0-beta.1", "0.1.1", true},
		{"0.1.1", "0.2.0-beta.1", false},
		{"0.2.0-beta.2", "0.2.0-beta.1", true},
		{"0.2.0-beta.10", "0.2.0-beta.9", true},
		{"0.2.0-rc.1", "0.2.0-beta.3", true},
		{"0.2.0", "0.2.0-beta.2", true},
		{"0.2.0-beta.2", "0.2.0", false},
		{"0.2.0-beta.1", "0.2.0-beta.1", false},
		{"0.2.0-beta.1", "dev", true},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func wait(t *testing.T, c *Checker) Release {
	t.Helper()
	select {
	case <-c.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("no answer")
	}
	r, ok := c.Latest()
	if !ok {
		t.Fatal("Ready without a release")
	}
	return r
}

func TestCheckerAsksAtMostEvery(t *testing.T) {
	srv, asks := github(t, http.StatusOK)
	c := newChecker(srv.Client(), srv.URL, "ujjwal313/boombrick", func() bool { return true }, time.Hour, nil)
	defer c.Close()
	if _, ok := c.Latest(); ok {
		t.Fatal("a release before any check")
	}
	c.Check()
	if r := wait(t, c); r.Version() != "0.5.0" {
		t.Fatalf("latest %q", r.Version())
	}
	c.Check()
	time.Sleep(50 * time.Millisecond)
	if n := asks.Load(); n != 1 {
		t.Fatalf("%d asks within the hour, want 1", n)
	}
}

func TestCheckerOffline(t *testing.T) {
	srv, asks := github(t, http.StatusOK)
	c := newChecker(srv.Client(), srv.URL, "ujjwal313/boombrick", func() bool { return false }, time.Hour, nil)
	defer c.Close()
	c.Check()
	time.Sleep(50 * time.Millisecond)
	if asks.Load() != 0 {
		t.Fatal("asked while offline")
	}
}

// Whether the device is online is read from sysfs: Check, called from the UI
// thread, leaves that to its goroutine.
func TestCheckDoesNotWaitToKnowIfOnline(t *testing.T) {
	srv, _ := github(t, http.StatusOK)
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	c := newChecker(srv.Client(), srv.URL, "ujjwal313/boombrick", func() bool { <-gate; return true }, time.Hour, nil)
	defer c.Close()
	defer open()
	done := make(chan struct{})
	go func() { c.Check(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Check waited to know whether the device is online")
	}
	open()
	wait(t, c)
}

func TestCheckerAfterAFailure(t *testing.T) {
	srv, asks := github(t, http.StatusServiceUnavailable)
	c := newChecker(srv.Client(), srv.URL, "ujjwal313/boombrick", nil, time.Hour, nil)
	defer c.Close()
	c.Check()
	for deadline := time.Now().Add(3 * time.Second); asks.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	c.Check()
	time.Sleep(50 * time.Millisecond)
	if n := asks.Load(); n != 1 {
		t.Fatalf("%d asks straight after a failure, want it to wait", n)
	}
	if _, ok := c.Latest(); ok {
		t.Fatal("a failed check gave a release")
	}
}

func TestCheckerClose(t *testing.T) {
	c := newChecker(http.DefaultClient, "http://127.0.0.1:1", "x/y", nil, time.Hour, nil)
	c.Close()
	c.Close()
	c.Check() // after Close: nothing happens
	if _, open := <-c.Ready(); open {
		t.Fatal("Ready is still open")
	}
}
