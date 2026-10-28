package platform

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// NextUI keeps its settings in POSIX shared memory, the file
// /dev/shm/SharedSettings: a struct of little-endian int32s, SettingsV11. These
// are the fields the volume is read from, by index.
const (
	settingsVersion   = 0
	settingsHeadphone = 3
	settingsSpeaker   = 4
	settingsMute      = 5
	settingsToggled   = 14
	settingsJack      = 27
	settingsSink      = 28
	settingsFields    = 29 // how many int32s are read

	settingsV11        = 11
	muteNoChange       = -69 // toggled_volume when nothing was muted from
	volumeLevels       = 20
	volumeSettingsFile = "/dev/shm/SharedSettings"
)

// How often the volume is read: often for a moment after a button press, as
// NextUI's volume keys change it, or after a change, as a key held changes it
// again; now and then otherwise.
const (
	volumeFast = 150 * time.Millisecond
	volumeSlow = 2 * time.Second
	volumeHold = 1500 * time.Millisecond // how long it is read often
)

// VolumeSettings is the file NextUI keeps its volume in, or "" under other
// firmwares.
func (p Profile) VolumeSettings() string {
	if p.Firmware != NextUI {
		return ""
	}
	return volumeSettingsFile
}

// nextUIVolume reads NextUI's volume, 0 to 20, from its settings as NextUI's
// GetVolume does. ok is false when they can't be read, aren't version 11 or
// hold a level out of range.
func nextUIVolume(r io.ReaderAt) (level int, ok bool) {
	var raw [settingsFields * 4]byte
	if n, _ := r.ReadAt(raw[:], 0); n < len(raw) {
		return 0, false
	}
	at := func(i int) int { return int(int32(binary.LittleEndian.Uint32(raw[i*4:]))) }
	if at(settingsVersion) != settingsV11 {
		return 0, false
	}
	switch {
	case at(settingsMute) != 0 && at(settingsToggled) != muteNoChange:
		level = at(settingsToggled)
	case at(settingsJack) != 0 || at(settingsSink) != 0:
		level = at(settingsHeadphone)
	default:
		level = at(settingsSpeaker)
	}
	if level < 0 || level > volumeLevels {
		return 0, false
	}
	return level, true
}

// volumeSource is what the volume is read from. *volumeFile is one.
type volumeSource interface {
	read() (level int, ok bool)
	close()
}

// volumeFile is NextUI's settings file, kept open between readings. A reading
// that fails closes it, so the next opens it afresh.
type volumeFile struct {
	path string
	f    *os.File
}

func (v *volumeFile) read() (int, bool) {
	if v.f == nil {
		f, err := os.Open(v.path)
		if err != nil {
			return 0, false
		}
		v.f = f
	}
	level, ok := nextUIVolume(v.f)
	if !ok {
		v.close()
	}
	return level, ok
}

func (v *volumeFile) close() {
	if v.f != nil {
		v.f.Close()
		v.f = nil
	}
}

// VolumeWatcher reads NextUI's volume for the volume pill: often for a moment
// after a button press or a change, now and then otherwise, and not at all
// while paused, as with the screen off. Pressed and Pause may be called from
// any goroutine.
type VolumeWatcher struct {
	src     volumeSource
	pressed chan struct{}
	paused  atomic.Bool
	// The level read last, the one the next reading is compared with, if
	// there is one: there isn't after a pause.
	last int
	have bool
}

// NewVolumeWatcher returns a watcher of the volume NextUI keeps in path. It
// reads it once now, the level later readings are compared with; ok reports
// whether that worked.
func NewVolumeWatcher(path string) (w *VolumeWatcher, ok bool) {
	return newVolumeWatcher(&volumeFile{path: path})
}

func newVolumeWatcher(src volumeSource) (*VolumeWatcher, bool) {
	w := &VolumeWatcher{src: src, pressed: make(chan struct{}, 1)}
	w.last, w.have = src.read()
	return w, w.have
}

// Pressed says a button went down, as a volume key may have: the volume is
// read at once, and then often for a moment.
func (w *VolumeWatcher) Pressed() {
	select {
	case w.pressed <- struct{}{}:
	default: // one is waiting already
	}
}

// Pause stops the reading, or starts it again at once. A change made while it
// was paused isn't reported: the first reading after is the starting point.
func (w *VolumeWatcher) Pause(paused bool) {
	w.paused.Store(paused)
	if !paused {
		w.Pressed()
	}
}

// Watch reads the volume until ctx ends and calls changed with each level
// that differs from the one read before; readings that fail are skipped. It
// closes the file when it returns.
func (w *VolumeWatcher) Watch(ctx context.Context, changed func(level int)) {
	defer w.src.close()
	t := time.NewTimer(volumeSlow)
	defer t.Stop()
	var fastUntil time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.pressed:
			fastUntil = time.Now().Add(volumeHold)
		case <-t.C:
		}
		if w.paused.Load() {
			t.Stop() // nothing is read until Pause lets it go
			w.have = false
			continue
		}
		if v, ok := w.src.read(); ok {
			if w.have && v != w.last {
				changed(v)
				fastUntil = time.Now().Add(volumeHold)
			}
			w.last, w.have = v, true
		}
		if time.Now().Before(fastUntil) {
			t.Reset(volumeFast)
		} else {
			t.Reset(volumeSlow)
		}
	}
}
