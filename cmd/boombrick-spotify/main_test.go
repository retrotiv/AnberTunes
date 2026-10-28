package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/spotify"
)

// The helper answers on its socket, signed out until signed in, and stops
// when the pipe from Boombrick closes, removing the socket.
func TestServeUntilTheParentGoes(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "spotify.sock")
	parent, end := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serve(filepath.Join(dir, "spotify"), socket, parent, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	var st spotify.Status
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		resp, err := c.Get("http://helper/status")
		if err == nil {
			json.NewDecoder(resp.Body).Decode(&st)
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no answer on the socket: %v", err)
		}
	}
	if st.State != spotify.SignedOut {
		t.Fatalf("a new helper is %q", st.State)
	}

	end.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still serving after the parent went")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("the socket is left behind: %v", err)
	}
}
