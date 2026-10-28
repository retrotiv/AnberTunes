package download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if plan := os.Getenv("BOOMBRICK_FAKE_YTDLP"); plan != "" {
		os.Exit(fakeYtDlp(plan, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeYtDlp does what plan says for each song of the batch, in order: "ok"
// downloads it, "gone" fails it by name, "403" fails it mid-download
// without naming it, "hang" starts it and waits to be killed. For a search
// it prints three results. Beyond those, "reset" gets as far as progress and
// then loses the connection, "skip" says nothing, and "long" lands the song
// after a 2 MB line.
func fakeYtDlp(plan string, args []string) int {
	arg := func(name string) string {
		i := slices.Index(args, name)
		if i < 0 || i+1 >= len(args) {
			return ""
		}
		return args[i+1]
	}
	if slices.Contains(args, "--flat-playlist") {
		fmt.Println("abcdefghijk\t231.0\tLumen Coast\tNight Drive Theory")
		fmt.Println("bcdefghijkl\tNA\tVela\tSodium Lights")
		fmt.Println("cdefghijklm\t60\tNA\tNA")
		fmt.Println("NA\t60\tVela\tNo ID")
		return 0
	}
	f, err := os.Open(arg("--batch-file"))
	if err != nil {
		return 2
	}
	defer f.Close()
	out := arg("-o")
	steps := strings.Split(plan, ",")
	code := 0
	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan(); i++ {
		id := sc.Text()[strings.LastIndexByte(sc.Text(), '=')+1:]
		step := "ok"
		if i < len(steps) {
			step = steps[i]
		}
		if step != "gone" && step != "skip" && step != "die" {
			fmt.Printf("BOOMBRICK-START %s\n", id)
		}
		switch step {
		case "ok", "long":
			if step == "long" {
				fmt.Println(strings.Repeat("x", 2<<20))
			}
			path := strings.NewReplacer("%(id)s", id, "%(format_id)s", "251", "%(ext)s", "webm").Replace(out)
			os.WriteFile(path, []byte("webm "+id), 0o644)
			fmt.Printf("BOOMBRICK-PROGRESS %s 1024 4096 NA 1000.5\n", id)
			fmt.Printf("BOOMBRICK-PROGRESS %s 4096 4096 NA NA\n", id)
			fmt.Printf("BOOMBRICK-DONE %s %s\n", id, path)
		case "gone":
			fmt.Fprintf(os.Stderr, "ERROR: [youtube] %s: Video unavailable. This video has been removed by the uploader\n", id)
			code = 1
		case "403":
			fmt.Fprintln(os.Stderr, "ERROR: unable to download video data: HTTP Error 403: Forbidden")
			code = 1
		case "403progress":
			fmt.Printf("BOOMBRICK-PROGRESS %s NA NA 9000 NA\n", id)
			fmt.Fprintln(os.Stderr, "ERROR: unable to download video data: HTTP Error 403: Forbidden")
			code = 1
		case "reset":
			fmt.Printf("BOOMBRICK-PROGRESS %s 10 4096 NA NA\n", id)
			fmt.Fprintln(os.Stderr, "ERROR: [Errno 104] Connection reset by peer")
			return 1
		case "skip":
		case "hang":
			fmt.Printf("BOOMBRICK-PROGRESS %s 10 4096 NA NA\n", id)
			// A child holding the pipe open, as QuickJS does.
			child := exec.Command("sleep", "30")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			child.Start()
			child.Wait()
		case "die":
			fmt.Fprintln(os.Stderr, "ERROR: [Errno 104] Connection reset by peer")
			return 1
		}
	}
	return code
}

func testRunner(t *testing.T, plan string) *Runner {
	t.Helper()
	dir := t.TempDir()
	return &Runner{Program: os.Args[0], Env: []string{"BOOMBRICK_FAKE_YTDLP=" + plan}, QJS: "/opt/qjs",
		Cache: filepath.Join(dir, "cache"), Parts: dir, Formats: "251/250/249"}
}

// events collects what Run reports.
type events struct {
	mu  sync.Mutex
	all []Event
}

func (e *events) add(ev Event) {
	e.mu.Lock()
	e.all = append(e.all, ev)
	e.mu.Unlock()
}

// outcomes are the terminal events, as "id ok", "id failed kind".
func (e *events) outcomes() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.all {
		switch {
		case ev.Path != "":
			out = append(out, ev.ID+" ok")
		case ev.Failure != NoFailure:
			out = append(out, fmt.Sprintf("%s failed %d", ev.ID, ev.Failure))
		}
	}
	return out
}

const (
	idA = "aaaaaaaaaaa"
	idB = "bbbbbbbbbbb"
	idC = "ccccccccccc"
	idD = "ddddddddddd"
)

func TestRunReportsEachSong(t *testing.T) {
	r := testRunner(t, "ok,gone,403progress,ok")
	var ev events
	err := r.Run(context.Background(), []string{idA, idB, idC, idD}, ev.add)
	if err == nil {
		t.Fatal("a batch with failures ran clean")
	}
	want := []string{idA + " ok", fmt.Sprintf("%s failed %d", idB, Unavailable), fmt.Sprintf("%s failed %d", idC, NeedsUpdate), idD + " ok"}
	got := ev.outcomes()
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("outcomes %q", got)
	}
	i := slices.IndexFunc(ev.all, func(e Event) bool { return e.ID == idA })
	if first := ev.all[i]; first.Done != 1024 || first.Total != 4096 || first.Speed != 1000.5 {
		t.Fatalf("A's first progress %+v", first)
	}
	if b, err := os.ReadFile(filepath.Join(r.Parts, idA+".251.webm")); err != nil || string(b) != "webm "+idA {
		t.Fatalf("the file: %q, %v", b, err)
	}
	for _, e := range ev.all {
		if e.ID == idC && e.Path == "" && e.Failure == NoFailure && e.Total != 9000 {
			t.Fatalf("an estimate stands in for an unknown total: %+v", e)
		}
	}
}

// A run that dies takes the songs it never reached with it.
func TestRunThatDiesFailsTheRest(t *testing.T) {
	r := testRunner(t, "ok,die")
	var ev events
	if err := r.Run(context.Background(), []string{idA, idB, idC}, ev.add); err == nil {
		t.Fatal("a run that died reported success")
	}
	want := []string{idA + " ok", fmt.Sprintf("%s failed %d", idB, Network), fmt.Sprintf("%s failed %d", idC, Network)}
	if got := ev.outcomes(); !slices.Equal(got, want) {
		t.Fatalf("outcomes %q", got)
	}
}

func TestCancellingLeavesSongsAsTheyWere(t *testing.T) {
	r := testRunner(t, "ok,hang")
	var ev events
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- r.Run(ctx, []string{idA, idB, idC}, ev.add) }()
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		ev.mu.Lock()
		n := len(ev.all)
		ev.mu.Unlock()
		if n >= 4 { // A's progress, progress and landing, then B's progress
			break
		}
		if time.Now().After(end) {
			t.Fatal("the batch never got to the second song")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled run reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling didn't stop yt-dlp")
	}
	if got := ev.outcomes(); !slices.Equal(got, []string{idA + " ok"}) {
		t.Fatalf("outcomes %q", got)
	}
}

func TestArguments(t *testing.T) {
	r := &Runner{Program: "yt-dlp", QJS: "/pak/bin/qjs", Cache: "/data/bin/cache", Parts: "/data/cache/downloads",
		Formats: "250/249/251"}
	args := strings.Join(r.args("/data/cache/downloads/batch.txt"), " ")
	for _, want := range []string{"-f 250/249/251", "--js-runtimes quickjs:/pak/bin/qjs", "--cache-dir /data/bin/cache",
		"-o /data/cache/downloads/%(id)s.%(format_id)s.%(ext)s", "before_dl:BOOMBRICK-START %(id)s", "--batch-file /data/cache/downloads/batch.txt", "--no-playlist",
		"--ignore-config", "--no-simulate"} {
		if !strings.Contains(args, want) {
			t.Errorf("no %q in %s", want, args)
		}
	}
	r.QJS = ""
	if args := strings.Join(r.args("b"), " "); strings.Contains(args, "--js-runtimes") {
		t.Error("a runtime named without one")
	}
}

func TestSearch(t *testing.T) {
	r := testRunner(t, "ok")
	items, err := r.Search(context.Background(), "night drive")
	if err != nil || len(items) != 3 {
		t.Fatalf("Search = %+v, %v", items, err)
	}
	if it := items[0]; it.ID != "abcdefghijk" || it.Title != "Night Drive Theory" || it.Artist != "Lumen Coast" ||
		it.Duration != 231*time.Second || it.Thumb != "https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg" {
		t.Fatalf("first %+v", it)
	}
	if items[1].Duration != 0 {
		t.Fatalf("an unknown length read as %v", items[1].Duration)
	}
	if it := items[2]; it.Artist != "" || it.Title != "" || it.Duration != 60*time.Second {
		t.Fatalf("unknown channel and title read as %+v", it)
	}
}

func TestSearchCancelled(t *testing.T) {
	r := testRunner(t, "ok")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Search(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Search = %v", err)
	}
}

// A 403 before any progress belongs to the song being started, not to the
// last one that finished.
func TestErrorBeforeProgressIsTheSongs(t *testing.T) {
	r := testRunner(t, "ok,403,reset")
	var ev events
	if err := r.Run(context.Background(), []string{idA, idB, idC}, ev.add); err == nil {
		t.Fatal("a failed batch ran clean")
	}
	want := []string{idA + " ok", fmt.Sprintf("%s failed %d", idB, NeedsUpdate), fmt.Sprintf("%s failed %d", idC, Network)}
	if got := ev.outcomes(); !slices.Equal(got, want) {
		t.Fatalf("outcomes %q", got)
	}
	for _, e := range ev.all {
		if e.ID == idB && e.Failure != NoFailure && !strings.Contains(e.Message, "HTTP Error 403") {
			t.Fatalf("B's message %q", e.Message)
		}
	}
}

func TestCleanExitFailsWhatWasNotReached(t *testing.T) {
	r := testRunner(t, "ok,skip")
	var ev events
	if err := r.Run(context.Background(), []string{idA, idB}, ev.add); err == nil {
		t.Fatal("a batch missing a song ran clean")
	}
	want := []string{idA + " ok", fmt.Sprintf("%s failed %d", idB, Other)}
	if got := ev.outcomes(); !slices.Equal(got, want) {
		t.Fatalf("outcomes %q", got)
	}
}

func TestLongLineIsSkipped(t *testing.T) {
	r := testRunner(t, "long")
	var ev events
	if err := r.Run(context.Background(), []string{idA}, ev.add); err != nil {
		t.Fatal(err)
	}
	if got := ev.outcomes(); !slices.Equal(got, []string{idA + " ok"}) {
		t.Fatalf("outcomes %q", got)
	}
}

func TestBadIDsStayOutOfTheBatch(t *testing.T) {
	r := testRunner(t, "ok")
	var ev events
	err := r.Run(context.Background(), []string{"x\nhttps://evil", idA}, ev.add)
	if err == nil || strings.Contains(err.Error(), "without every song") {
		t.Fatalf("a batch with a bad ID: %v", err)
	}
	want := []string{"x\nhttps://evil failed " + fmt.Sprint(int(Other)), idA + " ok"}
	if got := ev.outcomes(); !slices.Equal(got, want) {
		t.Fatalf("outcomes %q", got)
	}
}

func TestBatchFileIsRemoved(t *testing.T) {
	r := testRunner(t, "ok")
	stale := filepath.Join(r.Parts, "batch-old.txt")
	os.WriteFile(stale, []byte("old"), 0o644)
	var ev events
	if err := r.Run(context.Background(), []string{idA}, ev.add); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(r.Parts, "batch-*.txt")); len(left) != 0 {
		t.Fatalf("left behind: %q", left)
	}
}
