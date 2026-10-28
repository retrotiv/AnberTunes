// Package background plays Boombrick's music with no screen once Boombrick
// is left, as while a game runs, and hands it back. A lock says that the
// background player owns the queue; Start starts it detached and hears that
// it plays; Stop stops it; Run is its loop; HandOver and TakeBack pass the
// queue one way and the other.
package background

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Lock is held by the background player while it owns the queue. It is a
// flock, so a process that dies lets it go.
type Lock struct{ f *os.File }

// ErrHeld is Acquire's error when another process holds the lock.
var ErrHeld = errors.New("background: the player lock is held")

// Acquire takes the lock at path and writes this process's ID in it.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	// Holder's probe holds a shared lock for a moment, which fails an
	// exclusive request: ask again for a little while before saying it's held.
	end := time.Now().Add(100 * time.Millisecond)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		if time.Now().After(end) {
			f.Close()
			return nil, ErrHeld
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release lets the lock go. It empties the file first, so that only a process
// that dies leaves its ID behind.
func (l *Lock) Release() error {
	err := l.f.Truncate(0)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Holder reports the ID of the process that holds the lock at path, if one
// does. A lock just taken may not have its ID written yet: the ID is then 0.
func Holder(path string) (pid int, held bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	} else if err != nil {
		return 0, false, err
	}
	defer f.Close()
	// A shared lock is had only when no one holds the lock; closing the file
	// lets it go again.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		return 0, false, nil
	} else if !errors.Is(err, syscall.EWOULDBLOCK) {
		return 0, false, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return 0, true, err
	}
	pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, true, nil
}

// Stop asks the background player holding the lock at path to stop, as
// Boombrick takes the music back: SIGTERM, and SIGKILL once wait is over. It
// reports whether one held the lock, once that one has let it go.
func Stop(path string, wait time.Duration) (took bool, err error) {
	pid, held, err := Holder(path)
	for end := time.Now().Add(time.Second); held && pid <= 0 && err == nil && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond) // just taken: its ID is coming
		pid, held, err = Holder(path)
	}
	switch {
	case err != nil:
		return held, err
	case !held:
		return false, nil
	case pid <= 0:
		return true, errors.New("background: the lock's holder has no ID")
	}
	syscall.Kill(pid, syscall.SIGTERM)
	if free(path, wait) {
		return true, nil
	}
	syscall.Kill(pid, syscall.SIGKILL)
	if free(path, time.Second) {
		return true, nil
	}
	return true, fmt.Errorf("background: player %d didn't stop", pid)
}

// free waits up to wait for the lock at path to be free.
func free(path string, wait time.Duration) bool {
	for end := time.Now().Add(wait); ; time.Sleep(20 * time.Millisecond) {
		if _, held, err := Holder(path); err == nil && !held {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}
