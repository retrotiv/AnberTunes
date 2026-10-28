package resume

import (
	"database/sql"
	"errors"
	"time"

	"github.com/ujjwal313/boombrick/internal/player"
)

// Handover is what passes with the queue between Boombrick and its
// background player: whether music was playing, and the sleep timer.
type Handover struct {
	Playing bool
	Sleep   player.Sleep
}

// SaveHandover stores h beside the saved queue. With no queue saved there is
// nothing to hand over, and it stores nothing.
func SaveHandover(db *sql.DB, h Handover) error {
	var at int64
	if !h.Sleep.At.IsZero() {
		// Re-based on the wall clock as it reads now: the monotonic reading, which
		// a clock step leaves alone, gives the time left.
		at = time.Now().Add(time.Until(h.Sleep.At)).UnixMilli()
	}
	_, err := db.Exec(`UPDATE playback_state SET playing = ?, sleep_mode = ?, sleep_at = ? WHERE id = 1`,
		h.Playing, int(h.Sleep.Mode), at)
	return err
}

// LoadHandover reads what SaveHandover stored: the zero Handover when nothing
// was, or no queue was ever saved.
func LoadHandover(db *sql.DB) (Handover, error) {
	var (
		h        Handover
		mode, at int64
	)
	err := db.QueryRow(`SELECT playing, sleep_mode, sleep_at FROM playback_state WHERE id = 1`).Scan(&h.Playing, &mode, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Handover{}, nil
	} else if err != nil {
		return Handover{}, err
	}
	h.Sleep.Mode = player.SleepMode(mode)
	if at != 0 {
		h.Sleep.At = time.UnixMilli(at)
	}
	return h, nil
}
