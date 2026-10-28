package platform

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSoundUsers(t *testing.T) {
	proc := t.TempDir()
	process := func(pid, cmdline string, fds ...string) {
		dir := filepath.Join(proc, pid)
		if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644)
		for i, to := range fds {
			if err := os.Symlink(to, filepath.Join(dir, "fd", string(rune('3'+i)))); err != nil {
				t.Fatal(err)
			}
		}
	}
	process("812", "bin/boombrick-spotify\x00serve\x00", "/dev/null", "/dev/snd/pcmC0D0p", "/dev/snd/controlC0")
	process("900", "./boombrick\x00", "/mnt/SDCARD/Music/a.flac")
	process("self", "", "/dev/snd/pcmC0D0p") // not a process ID
	got := SoundUsers(proc)
	if want := []string{"812 bin/boombrick-spotify serve"}; !slices.Equal(got, want) {
		t.Fatalf("SoundUsers = %q, want %q", got, want)
	}
	if got := SoundUsers(filepath.Join(proc, "missing")); got != nil {
		t.Fatalf("without a proc folder, SoundUsers = %q", got)
	}
}
