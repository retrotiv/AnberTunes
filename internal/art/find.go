package art

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dhowden/tag"

	"github.com/ujjwal313/boombrick/internal/library"
)

// Embedded returns the picture in the tags of the file at path: ID3's APIC,
// a FLAC or Ogg picture block, or MP4's covr.
func Embedded(path string) (data []byte, ok bool) {
	// The tag package can panic on a truncated or damaged tag.
	defer func() {
		if recover() != nil {
			data, ok = nil, false
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, false
	}
	if p := m.Picture(); p != nil && len(p.Data) > 0 {
		return p.Data, true
	}
	return nil, false
}

// The files beside an album's tracks that hold its art, best first, and the
// formats they may be in.
var (
	coverNames = []string{"cover", "folder", "front"}
	coverExts  = []string{".jpg", ".jpeg", ".png", ".webp"}
)

// FolderImage finds an album's picture in dir: cover, folder or front, as
// JPEG, PNG or WebP, in any case.
func FolderImage(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	best, rank := "", len(coverNames)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		ext := filepath.Ext(name)
		if !slices.Contains(coverExts, ext) {
			continue
		}
		if i := slices.Index(coverNames, strings.TrimSuffix(name, ext)); i >= 0 && i < rank {
			best, rank = filepath.Join(dir, e.Name()), i
		}
	}
	return best, best != ""
}

// albumDirs are the folders to look in for a track's album art: its own, and
// the album's when the track sits in a disc folder.
func albumDirs(track string) []string {
	dirs := []string{filepath.Dir(track)}
	if d := library.AlbumDir(track); d != dirs[0] {
		dirs = append(dirs, d)
	}
	return dirs
}
