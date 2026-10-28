package download

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

func TestLeftoversAreThePartsOfSongsGone(t *testing.T) {
	e := newEnv(t)
	// The worker tidies the folder when it starts, then reads the card's space.
	eventually(t, "the worker to start", func() bool { return e.s.Status().Storage.Total > 0 })
	e.s.SetPaused(true)
	e.s.Get([]ytmusic.Song{song("v1", "Night Drive Theory", 1)})
	write := func(name string, n int) {
		if err := os.WriteFile(filepath.Join(e.f.parts, name), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("v1.251.webm.part", 300)  // queued: resumed later
	write("v9.251.webm.part", 1000) // its song is gone
	write("batch-7.txt", 50)        // the runner's

	if got := e.s.Leftovers(); got != 1000 {
		t.Fatalf("Leftovers = %d, want 1000", got)
	}
	if got := e.s.ClearLeftovers(); got != 1000 {
		t.Fatalf("ClearLeftovers freed %d, want 1000", got)
	}
	for name, want := range map[string]bool{"v1.251.webm.part": true, "v9.251.webm.part": false, "batch-7.txt": true} {
		if _, err := os.Stat(filepath.Join(e.f.parts, name)); (err == nil) != want {
			t.Errorf("%s there: %v, want %v", name, err == nil, want)
		}
	}
	if got := e.s.Leftovers(); got != 0 {
		t.Fatalf("Leftovers = %d after clearing them", got)
	}
}
