package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
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

	// watchHotkeys turns L1+R1+A into Toggle; here Toggle means "summon". Next
	// and Prev do nothing with no music to control.
	press := make(chan struct{}, 1)
	go watchHotkeys(ctx, summoner{press})

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
				continue // a background player already owns the queue and the hotkeys
			}
			if uiRunning(self) {
				continue // AnberTunes is open; it owns the music
			}
			last = time.Now()
			// Detached; it plays the saved queue and takes the hotkeys over. No
			// saved queue, or nothing it can play, just means nothing starts.
			background.Start(self, []string{"--background"}, os.Environ(), 10*time.Second)
		}
	}
}

// summoner is a control whose Toggle signals a summon, for watchHotkeys to
// drive. The signal is dropped when one is already pending.
type summoner struct{ ch chan struct{} }

func (s summoner) Toggle() {
	select {
	case s.ch <- struct{}{}:
	default:
	}
}
func (s summoner) Next() {}
func (s summoner) Prev() {}

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
