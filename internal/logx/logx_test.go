package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatesWhenFull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Boombrick.txt")
	l, err := Open(path, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	line := []byte(strings.Repeat("a", 59) + "\n")
	for i := 0; i < 3; i++ {
		if _, err := l.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	assertSize(t, path, 60)
	assertSize(t, path+".1", 60)
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatalf("%s.2 should not exist with keep=1 (err=%v)", path, err)
	}
}

func TestAppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Boombrick.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path, 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("world\n")); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\nworld\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteSurvivesFailedRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Boombrick.txt")
	l, err := Open(path, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	line := []byte(strings.Repeat("a", 59) + "\n")
	if _, err := l.Write(line); err != nil {
		t.Fatal(err)
	}
	// A directory at the rotation target makes the rename fail.
	if err := os.Mkdir(path+".1", 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := l.Write(line); err != nil {
		t.Fatalf("write after failed rotation: %v", err)
	}
	assertSize(t, path, 120)
	if _, err := l.Write(line); err != nil {
		t.Fatalf("later write: %v", err)
	}
	assertSize(t, path, 180)
}

func assertSize(t *testing.T, path string, want int64) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != want {
		t.Fatalf("%s is %d bytes, want %d", path, st.Size(), want)
	}
}
