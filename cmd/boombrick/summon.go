package main

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ujjwal313/boombrick/internal/background"
	"github.com/ujjwal313/boombrick/internal/platform"
)

// runSummon is the always-on music resident. It has no screen and no sound of
// its own: it waits for the music hotkey (hold L1+R1 and press A) and, if no
// background player is already playing and AnberTunes itself is not open,
// starts the background player on the saved queue. That lets the music be
// started and controlled from anywhere, the menu or a game, without opening
// AnberTunes first; once a background player runs, it owns the hotkeys and the
// resident stays out of the way. It is meant to be launched once at boot.
func runSummon() error {
	signal.Ignore(syscall.SIGHUP) // outlive the boot hook that started it
	reapChildren()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	prof := platform.Detect(programDir())
	lock := prof.PlayerLock()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// The in-game player's open combos (L1+R1+X and L1+R1+Y) also start the
	// resident player, so the player opens with nothing playing. Two readers
	// feed the same signal: the evdev reader catches the combo in the menu and
	// on emulators that leave the pad alone (PSP), and the command-file reader
	// catches it inside the heavy emulators (N64, Dreamcast) that grab the real
	// pad for themselves and hand the game a virtual one this process never
	// opened -- there the overlay is the one that sees the combo, and it writes
	// the view to the shared command file, which is the dependable trigger.
	press := make(chan struct{}, 1)
	go watchOpenCombo(ctx, press)
	go watchOverlayOpen(ctx, press, prof.CommandPipe())

	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-press:
			if time.Since(last) < 2*time.Second {
				continue // one start at a time: ignore a key held or pressed again at once
			}
			if _, held, _ := background.Holder(lock); held {
				continue // a player already owns the queue
			}
			if uiRunning(self) {
				continue // AnberTunes is open; it owns the music
			}
			last = time.Now()
			// Detached, resident: it opens idle and stays for the in-game player.
			background.Start(self, []string{"--background", "--resident"}, os.Environ(), 10*time.Second)
		}
	}
}

// watchOpenCombo signals press when the in-game player's open combo is pressed:
// L1+R1 held, then X or Y. It reads the pads straight from evdev, shared.
func watchOpenCombo(ctx context.Context, press chan struct{}) {
	const (
		keyX = 307
		keyY = 308
		keyL = 310
		keyR = 311
	)
	paths, _ := filepath.Glob("/dev/input/event*")
	var l1, r1 bool
	var mu sync.Mutex
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		go func(f *os.File) {
			defer f.Close()
			go func() { <-ctx.Done(); f.Close() }()
			buf := make([]byte, 24*64)
			for {
				n, err := f.Read(buf)
				if err != nil {
					return
				}
				for i := 0; i+24 <= n; i += 24 {
					if binary.LittleEndian.Uint16(buf[i+16:]) != 1 { // EV_KEY
						continue
					}
					code := binary.LittleEndian.Uint16(buf[i+18:])
					val := int32(binary.LittleEndian.Uint32(buf[i+20:]))
					mu.Lock()
					switch code {
					case keyL:
						l1 = val != 0
					case keyR:
						r1 = val != 0
					case keyX, keyY:
						if l1 && r1 && val == 1 {
							select {
							case press <- struct{}{}:
							default:
							}
						}
					}
					mu.Unlock()
				}
			}
		}(f)
	}
}

// watchOverlayOpen signals press when the in-game overlay asks to open the
// player. The overlay (inside the emulator) detects the combo on whatever pad
// the emulator exposes, including the virtual pad a heavy emulator creates,
// and appends the view to the shared command file. This process can miss the
// combo on evdev inside such an emulator -- the real pad is grabbed away and
// the virtual one did not exist when this process opened its devices at boot
// -- so the command file is the reliable trigger there. It only watches for an
// open (a non-hidden view); the resident, once started, reads the same file
// for the player's buttons. Reading starts at the end so a past session's
// commands are skipped, and a shrink (the resident trims the file) is followed.
func watchOverlayOpen(ctx context.Context, press chan struct{}, path string) {
	var off int64
	if fi, err := os.Stat(path); err == nil {
		off = fi.Size() // skip whatever a past session left
	}
	var pending string
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		size := fi.Size()
		if size < off { // trimmed or replaced: start over
			off, pending = 0, ""
		}
		if size > off {
			if _, err := f.Seek(off, io.SeekStart); err == nil {
				b, _ := io.ReadAll(f)
				off = size
				pending += string(b)
				for {
					i := strings.IndexByte(pending, '\n')
					if i < 0 {
						break
					}
					line := strings.TrimSpace(pending[:i])
					pending = pending[i+1:]
					if line == "view=reduced" || line == "view=expanded" {
						select {
						case press <- struct{}{}:
						default:
						}
					}
				}
			}
		}
		f.Close()
	}
}

// uiRunning reports whether the full AnberTunes screen is open: a process
// running the same binary with neither --background nor --summon. The resident
// must not start a background player over it.
func uiRunning(self string) bool {
	me := os.Getpid()
	base := filepath.Base(self)
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == me {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(b) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		if filepath.Base(args[0]) != base {
			continue
		}
		foreground := true
		for _, a := range args[1:] {
			if a == "--background" || a == "-background" || a == "--summon" || a == "-summon" {
				foreground = false
				break
			}
		}
		if foreground {
			return true
		}
	}
	return false
}

// reapChildren clears the background players the resident starts once they
// exit, so a long-lived resident leaves no zombies behind.
func reapChildren() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGCHLD)
	go func() {
		for range ch {
			for {
				var ws syscall.WaitStatus
				pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
				if pid <= 0 || err != nil {
					break
				}
			}
		}
	}()
}
