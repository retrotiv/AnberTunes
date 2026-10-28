package download

import (
	"os"
	"path/filepath"
	"strings"
)

// Leftovers is how much room the partial files of songs no longer queued or
// failed take: what ClearLeftovers deletes. Files a batch or the finisher is
// working on, and those of songs that can still be downloaded, aren't counted.
func (s *Service) Leftovers() int64 {
	var n int64
	for _, f := range s.leftovers() {
		if info, err := os.Stat(f); err == nil {
			n += info.Size()
		}
	}
	return n
}

// ClearLeftovers deletes those partial files, as Settings' Clear caches does,
// and reports the bytes freed.
func (s *Service) ClearLeftovers() int64 {
	var n int64
	for _, f := range s.leftovers() {
		if info, err := os.Stat(f); err == nil && os.Remove(f) == nil {
			n += info.Size()
		}
	}
	return n
}

// leftovers lists the partial files of songs the queue no longer has, or has
// saved, as tidy does at start, leaving out the files being finished and the
// runner's batch files.
func (s *Service) leftovers() []string {
	files, _ := filepath.Glob(filepath.Join(s.parts, "*"))
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{s.finishing: true}
	for _, l := range s.landing {
		keep[l.src] = true
	}
	var out []string
	for _, f := range files {
		base := filepath.Base(f)
		if keep[f] || strings.HasPrefix(base, "batch-") {
			continue
		}
		id, _, _ := strings.Cut(base, ".")
		if r := s.rows[id]; r != nil && r.state != saved {
			continue // queued or failed: kept to be resumed
		}
		out = append(out, f)
	}
	return out
}
