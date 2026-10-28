package background

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func lockPath(t *testing.T) string { return filepath.Join(t.TempDir(), "player.lock") }

// childEnv runs the test binary as the part of a background player named.
func childEnv(part, lock string) []string {
	return append(os.Environ(), "BACKGROUND_TEST="+part, "BACKGROUND_LOCK="+lock)
}

func waitFree(t *testing.T, path string) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, held, err := Holder(path); err == nil && !held {
			return
		}
		if time.Now().After(end) {
			t.Fatal("the lock is still held")
		}
	}
}

// One process at a time holds the lock, and says who it is.
func TestTheLockIsOnesAtATime(t *testing.T) {
	path := lockPath(t)
	if _, held, err := Holder(path); held || err != nil {
		t.Fatalf("no lock file: held %v, %v", held, err)
	}
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrHeld) {
		t.Fatalf("a second Acquire: %v", err)
	}
	if pid, held, err := Holder(path); !held || pid != os.Getpid() || err != nil {
		t.Fatalf("Holder = %d, %v, %v", pid, held, err)
	}
	l.Release()
	if _, held, _ := Holder(path); held {
		t.Fatal("released, the lock is still held")
	}
	l, err = Acquire(path)
	if err != nil {
		t.Fatalf("taking it again: %v", err)
	}
	l.Release()
}

// Start returns once the background player says it plays, and Stop asks it
// to stop and waits until it has let the lock go.
func TestStartAndStop(t *testing.T) {
	path := lockPath(t)
	pid, err := Start(os.Args[0], nil, childEnv("hold", path), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(path, time.Second) })
	if sid, err := getsid(pid); err != nil || sid != pid {
		t.Fatalf("the player's session is %d, %v; it should lead its own, %d", sid, err, pid)
	}
	if got, held, _ := Holder(path); !held || got != pid {
		t.Fatalf("Holder = %d, %v; started %d", got, held, pid)
	}
	if took, err := Stop(path, 5*time.Second); !took || err != nil {
		t.Fatalf("Stop = %v, %v", took, err)
	}
	if _, held, _ := Holder(path); held {
		t.Fatal("stopped, the lock is still held")
	}
	if took, err := Stop(path, time.Second); took || err != nil {
		t.Fatalf("with none running, Stop = %v, %v", took, err)
	}
}

// One that never says it plays is killed, and Start fails.
func TestStartGivesUpOnASilentPlayer(t *testing.T) {
	path := lockPath(t)
	if _, err := Start(os.Args[0], nil, childEnv("silent", path), 300*time.Millisecond); err == nil {
		t.Fatal("Start returned for a player that never played")
	}
	waitFree(t, path)
}

// One still starting when the wait is over is asked to stop, as Stop asks, so
// that it lets go of the sound card and its Spotify helper: Start returns once
// it has gone.
func TestStartStopsASlowPlayerGently(t *testing.T) {
	path := lockPath(t)
	if _, err := Start(os.Args[0], nil, childEnv("slow", path), 300*time.Millisecond); err == nil {
		t.Fatal("Start returned for a player that never played")
	}
	if _, err := os.Stat(path + ".stopped"); err != nil {
		t.Fatal("the player was killed without being asked to stop")
	}
	if _, held, _ := Holder(path); held {
		t.Fatal("Start returned before the player had gone")
	}
}

// One that doesn't stop when asked is killed after stopWait.
func TestStartKillsASlowPlayerThatWontStop(t *testing.T) {
	defer func(d time.Duration) { stopWait = d }(stopWait)
	stopWait = 200 * time.Millisecond
	path := lockPath(t)
	began := time.Now()
	if _, err := Start(os.Args[0], nil, childEnv("deaf", path), 300*time.Millisecond); err == nil {
		t.Fatal("Start returned for a player that never played")
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("Start took %v to give up", took)
	}
	if _, held, _ := Holder(path); held {
		t.Fatal("Start returned before the player had gone")
	}
}

// One that ignores SIGTERM is killed once the wait is over.
func TestStopKillsAStubbornPlayer(t *testing.T) {
	path := lockPath(t)
	if _, err := Start(os.Args[0], nil, childEnv("stubborn", path), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(path, time.Second) })
	if took, err := Stop(path, 200*time.Millisecond); !took || err != nil {
		t.Fatalf("Stop = %v, %v", took, err)
	}
	waitFree(t, path)
}

// The background player plays on after whatever started it has gone, as
// Boombrick quits once it has handed the music over.
func TestThePlayerOutlivesItsStarter(t *testing.T) {
	path := lockPath(t)
	cmd := exec.Command(os.Args[0])
	cmd.Env = childEnv("starter", path)
	if err := cmd.Run(); err != nil {
		t.Fatalf("the starter: %v", err)
	}
	t.Cleanup(func() { Stop(path, time.Second) })
	if _, held, _ := Holder(path); !held {
		t.Fatal("the player went with its starter")
	}
	if took, err := Stop(path, 5*time.Second); !took || err != nil {
		t.Fatalf("Stop = %v, %v", took, err)
	}
}

// Looking at a free lock must not make taking it fail.
func TestHolderDoesNotBlockAcquire(t *testing.T) {
	path := lockPath(t)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-done:
				return
			default:
				Holder(path)
			}
		}
	}()
	defer func() { close(done); <-finished }()
	for i := 0; i < 300; i++ {
		l, err := Acquire(path)
		if err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
		l.Release()
	}
}

// getsid is the session a process leads or is in; the syscall package has no
// Getsid.
func getsid(pid int) (int, error) {
	sid, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(sid), nil
}
