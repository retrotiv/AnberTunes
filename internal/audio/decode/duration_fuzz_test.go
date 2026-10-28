package decode

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var fuzzExts = []string{".mp3", ".flac", ".opus", ".m4a"}

// FuzzDuration feeds damaged files to the header readers the scanner runs
// on every file on the card. They may fail but must not panic or hang.
func FuzzDuration(f *testing.F) {
	for _, ext := range fuzzExts {
		f.Add(ext, []byte{})
	}
	for _, name := range []string{"tone-44100.mp3", "vbr-44100.mp3", "tone-48000.flac", "tone-48000.opus", "tone-44100.m4a", "faststart-44100.m4a"} {
		if data, err := os.ReadFile(filepath.Join("testdata", name)); err == nil {
			f.Add(filepath.Ext(name), data[:min(len(data), 64<<10)])
		}
	}
	f.Fuzz(func(t *testing.T, ext string, data []byte) {
		if !slices.Contains(fuzzExts, ext) {
			return
		}
		path := filepath.Join(t.TempDir(), "f"+ext)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		Duration(path)
	})
}
