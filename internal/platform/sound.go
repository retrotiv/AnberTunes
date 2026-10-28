package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SoundUsers lists the programs with the sound card open, as "pid command
// line", from proc (/proc). Programs it may not look into are left out.
func SoundUsers(proc string) []string {
	dirs, err := os.ReadDir(proc)
	if err != nil {
		return nil
	}
	var users []string
	for _, d := range dirs {
		pid := d.Name()
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		fds, _ := os.ReadDir(filepath.Join(proc, pid, "fd"))
		for _, fd := range fds {
			to, err := os.Readlink(filepath.Join(proc, pid, "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(to, "/dev/snd/") {
				continue
			}
			cmd, _ := os.ReadFile(filepath.Join(proc, pid, "cmdline"))
			users = append(users, pid+" "+strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " ")))
			break
		}
	}
	return users
}
