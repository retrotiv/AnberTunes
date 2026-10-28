package art

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Size is how much room the cached copies take.
func (c *Cache) Size() (int64, error) { return dirSize(c.opts.Dir) }

// Clear deletes every cached copy and empties the art table, as Settings'
// Clear caches does, and reports the bytes freed. Albums' art is found again,
// in their music or online, as they are shown. It waits for a picture being
// written to be done, and must not be called on the UI thread.
func (c *Cache) Clear() (int64, error) {
	c.heavy.Lock() // no copy is being written meanwhile
	defer c.heavy.Unlock()
	c.mu.Lock()
	for _, a := range c.albums {
		a.state, a.file, a.lookedUp, a.failed, a.broken, a.searching = unknown, "", 0, time.Time{}, false, false
		clear(a.wanted)
	}
	c.jobs, c.done, c.searches = c.jobs[:0], nil, c.searches[:0]
	c.busyUntil = time.Time{}
	c.mu.Unlock()
	defer c.notify() // screens ask again, and are given art made afresh

	before, _ := dirSize(c.opts.Dir)
	if _, err := c.opts.DB.Exec(`DELETE FROM art`); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(c.opts.Dir)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	for _, e := range entries {
		os.RemoveAll(filepath.Join(c.opts.Dir, e.Name()))
	}
	after, _ := dirSize(c.opts.Dir)
	return before - after, nil
}

// dirSize adds up the sizes of the files under dir; a missing dir is empty.
func dirSize(dir string) (int64, error) {
	var n int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n, err
}
