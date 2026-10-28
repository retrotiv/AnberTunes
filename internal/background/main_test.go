package background

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestMain makes the test binary stand in for a background player when
// BACKGROUND_TEST names the part to play, on the lock BACKGROUND_LOCK names:
//
//	hold      takes the lock, says it plays, and stops on SIGTERM
//	stubborn  the same, but ignores SIGTERM
//	silent    takes the lock and never says it plays
//	slow      the same, but on SIGTERM writes the lock's path plus ".stopped" and stops
//	deaf      the same as silent, but ignores SIGTERM
//	starter   starts a holder, then exits at once
//	nopid     takes the lock without writing its ID, says it plays, and stops on SIGTERM
func TestMain(m *testing.M) {
	lock := os.Getenv("BACKGROUND_LOCK")
	switch os.Getenv("BACKGROUND_TEST") {
	case "":
		os.Exit(m.Run())
	case "hold":
		hold(lock, false)
	case "stubborn":
		hold(lock, true)
	case "silent":
		if _, err := Acquire(lock); err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Minute)
	case "slow", "deaf":
		slow(lock, os.Getenv("BACKGROUND_TEST") == "deaf")
	case "nopid":
		nopid(lock)
	case "starter":
		if _, err := Start(os.Args[0], nil, append(os.Environ(), "BACKGROUND_TEST=hold"), 5*time.Second); err != nil {
			os.Exit(3)
		}
	}
	os.Exit(0)
}

func hold(lock string, stubborn bool) {
	ready := Readier()
	l, err := Acquire(lock)
	if err != nil {
		os.Exit(2)
	}
	stop := make(chan os.Signal, 1)
	if stubborn {
		signal.Ignore(syscall.SIGTERM)
	} else {
		signal.Notify(stop, syscall.SIGTERM)
	}
	ready()
	select {
	case <-stop:
	case <-time.After(time.Minute):
	}
	l.Release()
}

// slow takes the lock and never says it plays, like a player still signing in
// to Spotify. Asked to stop, it notes that it did, unless it is deaf.
func slow(lock string, deaf bool) {
	l, err := Acquire(lock)
	if err != nil {
		os.Exit(2)
	}
	stop := make(chan os.Signal, 1)
	if deaf {
		signal.Ignore(syscall.SIGTERM)
	} else {
		signal.Notify(stop, syscall.SIGTERM)
	}
	select {
	case <-stop:
		os.WriteFile(lock+".stopped", nil, 0o644)
	case <-time.After(time.Minute):
	}
	l.Release()
}

func nopid(lock string) {
	ready := Readier()
	f, err := os.OpenFile(lock, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		os.Exit(2)
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		os.Exit(2)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM)
	ready()
	select {
	case <-stop:
	case <-time.After(time.Minute):
	}
}
