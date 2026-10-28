package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The firmwares' launch scripts are tested here, beside the code that
// stages what they apply.

// launchScript is a firmware's launch script: its file in build/, and its
// name in the app's folder.
type launchScript struct{ name, src, file string }

// launchers are the firmwares' launch scripts.
var launchers = []launchScript{
	{"NextUI", filepath.Join("..", "..", "build", "pak", "launch.sh"), "launch.sh"},
	{"CrossMix", filepath.Join("..", "..", "build", "crossmix", "launch.sh"), "launch.sh"},
	{"Knulli", filepath.Join("..", "..", "build", "knulli", "launch"), "launch"},
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// installed is the app as a firmware holds it: l's script, and a program
// that says which it is, with its folders.
func installed(t *testing.T, l launchScript) string {
	t.Helper()
	return installedIn(t, t.TempDir(), l)
}

// installedIn is installed, in the folder dir.
func installedIn(t *testing.T, dir string, l launchScript) string {
	t.Helper()
	body, err := os.ReadFile(l.src)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, l.file), string(body), 0o755)
	write(t, filepath.Join(dir, "boombrick"), "#!/bin/sh\necho old > ran\n", 0o755)
	write(t, filepath.Join(dir, "bin", "qjs"), "old qjs", 0o755)
	write(t, filepath.Join(dir, "lib", "libold.so"), "old", 0o644)
	return dir
}

// launch runs the script at path as its firmware does, with stand-ins for
// the firmware's programs, and returns the folder it uses for /tmp.
func launch(t *testing.T, path string) string {
	t.Helper()
	cmd, tmp := launcher(t, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", filepath.Base(path), err, out)
	}
	return tmp
}

// launcher is the command launch runs, and the folder it uses for /tmp and,
// as Knulli's, /var/run. The stand-in poweroff, given by path as busybox's
// sh would run its own before one on PATH, leaves powered-off there.
func launcher(t *testing.T, path string) (*exec.Cmd, string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	stubs, tmp := t.TempDir(), t.TempDir()
	write(t, filepath.Join(stubs, "syncsettings.elf"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(stubs, "poweroff"), "#!/bin/sh\ntouch \"$BOOMBRICK_TMP/powered-off\"\n", 0o755)
	cmd := exec.Command(sh, path)
	cmd.Env = append(os.Environ(), "LOGS_PATH="+t.TempDir(), "BOOMBRICK_TMP="+tmp, "BOOMBRICK_RUN="+tmp,
		"BOOMBRICK_POWEROFF="+filepath.Join(stubs, "poweroff"),
		"PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cmd, tmp
}

func TestLaunchAppliesAStagedUpdate(t *testing.T) {
	for _, l := range launchers {
		t.Run(l.name, func(t *testing.T) {
			dir := installed(t, l)
			staged := filepath.Join(dir, ".update", "pak")
			write(t, filepath.Join(staged, l.file), read(t, filepath.Join(dir, l.file)), 0o755)
			write(t, filepath.Join(staged, "boombrick"), "#!/bin/sh\necho new > ran\n", 0o755)
			write(t, filepath.Join(staged, "bin", "qjs"), "new qjs", 0o755)
			write(t, filepath.Join(staged, "lib", "libnew.so"), "new", 0o644)
			write(t, filepath.Join(dir, ".update", "ready"), "0.5.0\n", 0o644)
			launch(t, filepath.Join(dir, l.file))
			if got := read(t, filepath.Join(dir, "ran")); got != "new\n" {
				t.Fatalf("the program that ran said %q", got)
			}
			if read(t, filepath.Join(dir, "bin", "qjs")) != "new qjs" || read(t, filepath.Join(dir, "lib", "libnew.so")) != "new" {
				t.Fatal("the new files weren't moved in")
			}
			if read(t, filepath.Join(dir, "lib", "libold.so")) != "old" {
				t.Fatal("a file the update doesn't have went")
			}
			if _, err := os.Stat(filepath.Join(dir, ".update")); !os.IsNotExist(err) {
				t.Fatal(".update is still there")
			}
		})
	}
}

func TestLaunchFinishesAnUpdateCutShort(t *testing.T) {
	for _, l := range launchers {
		t.Run(l.name, func(t *testing.T) {
			dir := installed(t, l)
			// The program was moved in before the power went; qjs wasn't.
			write(t, filepath.Join(dir, "boombrick"), "#!/bin/sh\necho new > ran\n", 0o755)
			write(t, filepath.Join(dir, ".update", "pak", "bin", "qjs"), "new qjs", 0o755)
			write(t, filepath.Join(dir, ".update", "ready"), "0.5.0\n", 0o644)
			launch(t, filepath.Join(dir, l.file))
			if read(t, filepath.Join(dir, "ran")) != "new\n" || read(t, filepath.Join(dir, "bin", "qjs")) != "new qjs" {
				t.Fatal("the update wasn't finished")
			}
			if _, err := os.Stat(filepath.Join(dir, ".update")); !os.IsNotExist(err) {
				t.Fatal(".update is still there")
			}
		})
	}
}

func TestLaunchDeletesAnIncompleteStage(t *testing.T) {
	for _, l := range launchers {
		t.Run(l.name, func(t *testing.T) {
			dir := installed(t, l)
			write(t, filepath.Join(dir, ".update", "pak", "boombrick"), "#!/bin/sh\necho new > ran\n", 0o755)
			launch(t, filepath.Join(dir, l.file))
			if got := read(t, filepath.Join(dir, "ran")); got != "old\n" {
				t.Fatalf("the program that ran said %q", got)
			}
			if _, err := os.Stat(filepath.Join(dir, ".update")); !os.IsNotExist(err) {
				t.Fatal("the incomplete stage is still there")
			}
		})
	}
}

// CrossMix's launcher names the firmware, keeps the Brick awake while
// Boombrick runs, and keeps the early output in the app's logs folder.
func TestCrossMixLaunch(t *testing.T) {
	dir := installed(t, launchers[1])
	write(t, filepath.Join(dir, "boombrick"), `#!/bin/sh
echo "$BOOMBRICK_PLATFORM" > ran
[ -f "$BOOMBRICK_TMP/stay_awake" ] && echo awake >> ran
echo starting
`, 0o755)
	tmp := launch(t, filepath.Join(dir, "launch.sh"))
	if got := read(t, filepath.Join(dir, "ran")); got != "crossmix\nawake\n" {
		t.Fatalf("the program saw %q", got)
	}
	if _, err := os.Stat(filepath.Join(tmp, "stay_awake")); !os.IsNotExist(err) {
		t.Fatal("stay_awake outlived Boombrick")
	}
	if got := read(t, filepath.Join(dir, "logs", "Boombrick.crash.txt")); got != "starting\n" {
		t.Fatalf("the early output is %q", got)
	}
	if _, err := os.Stat(filepath.Join(tmp, "powered-off")); !os.IsNotExist(err) {
		t.Fatal("powered off though Boombrick didn't ask")
	}
}

// Boombrick on CrossMix asks to be powered off with a flag; the launcher
// powers off once it has exited.
func TestCrossMixPowersOffWhenAsked(t *testing.T) {
	dir := installed(t, launchers[1])
	write(t, filepath.Join(dir, "boombrick"), "#!/bin/sh\ntouch \"$BOOMBRICK_TMP/boombrick-poweroff\"\n", 0o755)
	tmp := launch(t, filepath.Join(dir, "launch.sh"))
	if _, err := os.Stat(filepath.Join(tmp, "powered-off")); err != nil {
		t.Fatal("the launcher didn't power off")
	}
	if _, err := os.Stat(filepath.Join(tmp, "boombrick-poweroff")); !os.IsNotExist(err) {
		t.Fatal("the flag is still there")
	}
}

// A flag left by a launcher that was killed before it read it doesn't power
// off the Brick at the next ordinary quit.
func TestCrossMixIgnoresAnOldFlag(t *testing.T) {
	dir := installed(t, launchers[1])
	cmd, tmp := launcher(t, filepath.Join(dir, "launch.sh"))
	write(t, filepath.Join(tmp, "boombrick-poweroff"), "", 0o644)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("launch.sh: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(tmp, "powered-off")); !os.IsNotExist(err) {
		t.Fatal("powered off for an old flag")
	}
}

// A SIGTERM to the launchers of CrossMix and Knulli, which outlive
// Boombrick, goes on to Boombrick, so it quits as it does when NextUI's
// launcher, replaced by it, is sent one: saving the queue. Knulli sends one
// to a port it closes. The keep-awake file goes with Boombrick.
func TestLaunchPassesOnSIGTERM(t *testing.T) {
	for _, c := range []struct {
		l     launchScript
		awake string // the keep-awake file, under the launcher's /tmp
	}{
		{launchers[1], "stay_awake"},
		{launchers[2], filepath.Join("battery-saver", "boombrick.pause")},
	} {
		t.Run(c.l.name, func(t *testing.T) {
			dir := installed(t, c.l)
			write(t, filepath.Join(dir, "boombrick"), `#!/bin/sh
trap 'echo stopped > ran; exit 0' TERM
touch running
while :; do sleep 0.05; done
`, 0o755)
			cmd, tmp := launcher(t, filepath.Join(dir, c.l.file))
			if err := os.Mkdir(filepath.Join(tmp, "battery-saver"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
				if _, err := os.Stat(filepath.Join(dir, "running")); err == nil {
					break
				}
			}
			if _, err := os.Stat(filepath.Join(tmp, c.awake)); err != nil {
				t.Fatalf("no keep-awake file while Boombrick runs: %v", err)
			}
			cmd.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				cmd.Process.Kill()
				t.Fatal("the launcher didn't end")
			}
			if got := read(t, filepath.Join(dir, "ran")); got != "stopped\n" {
				t.Fatalf("Boombrick saw %q, want the SIGTERM", got)
			}
			if _, err := os.Stat(filepath.Join(tmp, c.awake)); !os.IsNotExist(err) {
				t.Fatal("the keep-awake file outlived Boombrick")
			}
		})
	}
}

// Knulli lists Boombrick.sh under Ports and runs it to start the app, in
// the boombrick folder beside it. The launcher names the firmware, holds
// off Knulli's battery saver while Boombrick runs, and keeps the early
// output in the app's logs folder.
func TestKnulliLaunch(t *testing.T) {
	ports := t.TempDir()
	stub, err := os.ReadFile(filepath.Join("..", "..", "build", "knulli", "Boombrick.sh"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(ports, "Boombrick.sh"), string(stub), 0o755)
	dir := installedIn(t, filepath.Join(ports, "boombrick"), launchers[2])
	write(t, filepath.Join(dir, "boombrick"), `#!/bin/sh
echo "$BOOMBRICK_PLATFORM" > ran
[ -f "$BOOMBRICK_RUN/battery-saver/boombrick.pause" ] && echo paused >> ran
echo starting
`, 0o755)
	cmd, run := launcher(t, filepath.Join(ports, "Boombrick.sh"))
	if err := os.Mkdir(filepath.Join(run, "battery-saver"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Boombrick.sh: %v\n%s", err, out)
	}
	if got := read(t, filepath.Join(dir, "ran")); got != "knulli\npaused\n" {
		t.Fatalf("the program saw %q", got)
	}
	if _, err := os.Stat(filepath.Join(run, "battery-saver", "boombrick.pause")); !os.IsNotExist(err) {
		t.Fatal("the pause file outlived Boombrick")
	}
	if got := read(t, filepath.Join(dir, "logs", "Boombrick.crash.txt")); got != "starting\n" {
		t.Fatalf("the early output is %q", got)
	}
}

// Without Knulli's battery saver there is nothing to hold off, and its
// folder isn't made.
func TestKnulliLaunchWithoutTheBatterySaver(t *testing.T) {
	dir := installed(t, launchers[2])
	run := launch(t, filepath.Join(dir, "launch"))
	if got := read(t, filepath.Join(dir, "ran")); got != "old\n" {
		t.Fatalf("the program that ran said %q", got)
	}
	if _, err := os.Stat(filepath.Join(run, "battery-saver")); !os.IsNotExist(err) {
		t.Fatal("the battery saver's folder was made")
	}
}
