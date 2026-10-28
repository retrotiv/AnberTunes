package decode

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	for _, c := range []struct {
		name string
		tol  time.Duration
	}{
		{"tone-48000.flac", time.Millisecond},
		{"tone-48000.opus", time.Millisecond},
		{"tone-44100.m4a", time.Millisecond},
		{"tone-44100.mp3", 30 * time.Millisecond},   // LAME's Info header counts the frames
		{"vbr-44100.mp3", 30 * time.Millisecond},    // Xing header
		{"noxing-44100.mp3", 60 * time.Millisecond}, // estimated from the bitrate
	} {
		t.Run(c.name, func(t *testing.T) {
			d, err := Duration(fixture(t, c.name))
			if err != nil {
				t.Fatal(err)
			}
			if diff := d - time.Second; diff < -c.tol || diff > c.tol {
				t.Fatalf("Duration = %v, want 1s ± %v", d, c.tol)
			}
		})
	}
}

func TestDurationOfJunk(t *testing.T) {
	dir := t.TempDir()
	for _, ext := range []string{".mp3", ".flac", ".opus", ".m4a"} {
		path := filepath.Join(dir, "junk"+ext)
		if err := os.WriteFile(path, []byte("this is not audio, just some text in a file"), 0o644); err != nil {
			t.Fatal(err)
		}
		if d, err := Duration(path); err == nil {
			t.Errorf("Duration(%s) = %v, want an error", ext, d)
		}
	}
	if _, err := Duration(filepath.Join(dir, "song.ogg")); err == nil {
		t.Error("Duration of an unsupported format should fail")
	}
}
