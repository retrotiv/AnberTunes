package lyrics

import "database/sql"

// Size is how much room the lyrics fetched from LRCLIB take in the store.
func (s *Service) Size() (int64, error) {
	var n sql.NullInt64
	err := s.opts.DB.QueryRow(`SELECT SUM(length(CAST(body AS BLOB))) FROM lyrics`).Scan(&n)
	return n.Int64, err
}

// Clear forgets the lyrics fetched from LRCLIB, as Settings' Clear caches
// does, and reports the bytes freed; they are fetched again as their tracks
// play. Lyrics in tags and .lrc files are untouched. It must not be called
// on the UI thread.
func (s *Service) Clear() (int64, error) {
	n, err := s.Size()
	if err != nil {
		return 0, err
	}
	if _, err := s.opts.DB.Exec(`DELETE FROM lyrics`); err != nil {
		return 0, err
	}
	s.mu.Lock()
	// Answers are looked for again when next asked; one being looked for
	// meanwhile lands nowhere, as for a track pushed out of entries.
	s.entries, s.order = map[string]*entry{}, nil
	s.mu.Unlock()
	select {
	case s.ready <- struct{}{}:
	default:
	}
	return n, nil
}
