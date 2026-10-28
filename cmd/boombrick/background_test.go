package main

import (
	"errors"
	"syscall"
	"testing"
)

// A descriptor without close-on-exec is one a C library opened: closeInherited
// closes it, and leaves the ones Go opened.
func TestCloseInherited(t *testing.T) {
	fd, err := syscall.Open("/dev/null", syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	var pipe [2]int
	if err := syscall.Pipe2(pipe[:], syscall.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(pipe[0])
	defer syscall.Close(pipe[1])

	closeInherited()

	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0); !errors.Is(errno, syscall.EBADF) {
		t.Fatalf("the inherited descriptor is still open: %v", errno)
	}
	if _, err := syscall.Write(pipe[1], []byte("x")); err != nil {
		t.Fatalf("the pipe was closed: %v", err)
	}
	b := make([]byte, 1)
	if n, err := syscall.Read(pipe[0], b); n != 1 || err != nil {
		t.Fatalf("read = %d, %v", n, err)
	}
}
