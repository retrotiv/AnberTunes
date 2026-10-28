// Package logx provides the size-rotated log file Boombrick writes to the
// firmware's log folder.
package logx

import (
	"fmt"
	"os"
	"sync"
)

// File is an io.Writer that appends to a log file and rotates it once a write
// would take it past the size limit. Rotated copies are named path.1, path.2…
type File struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	keep    int
	f       *os.File
	size    int64
}

// Open opens or creates the log at path. keep is the number of rotated copies
// kept alongside the live file.
func Open(path string, maxSize int64, keep int) (*File, error) {
	l := &File{path: path, maxSize: maxSize, keep: keep}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *File) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, st.Size()
	return nil
}

// Write appends p to the live file, rotating first if it would exceed the size
// limit. A failed rotation keeps logging to the current file.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > l.maxSize {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate moves the live file aside and reopens it. If a rename fails the
// shifting stops early and the live file stays where it is, so the reopen
// still happens and only an error from reopening is returned.
func (l *File) rotate() error {
	l.f.Close()
	for i := l.keep; i > 0; i-- {
		src := l.path
		if i > 1 {
			src = fmt.Sprintf("%s.%d", l.path, i-1)
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, fmt.Sprintf("%s.%d", l.path, i)); err != nil {
			break
		}
	}
	return l.open()
}

// Close closes the live file.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
