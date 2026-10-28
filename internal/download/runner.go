package download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// Runner runs yt-dlp.
type Runner struct {
	Program string       // yt-dlp
	Env     []string     // added to yt-dlp's environment
	QJS     string       // QuickJS, yt-dlp's JavaScript runtime; "" leaves yt-dlp to find one
	Cache   string       // yt-dlp's cache, kept between runs
	Parts   string       // where files are downloaded to
	Formats string       // which formats, in order of preference
	Log     *slog.Logger // nil discards
}

// Event is something yt-dlp reported about one song: progress, the file
// landing (Path), or a failure.
type Event struct {
	ID          string
	Done, Total int64   // bytes; Total is 0 while unknown
	Speed       float64 // bytes a second, 0 while unknown
	Path        string
	Failure     Failure
	Message     string
}

// The marks that start the lines Boombrick asks yt-dlp for.
const (
	startMark    = "BOOMBRICK-START"
	progressMark = "BOOMBRICK-PROGRESS"
	doneMark     = "BOOMBRICK-DONE"
)

// videoID is what a YouTube video's ID looks like.
var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// maxLine is the longest line of yt-dlp's output that is read; a longer one
// is skipped.
const maxLine = 1 << 20

// args is the command line for a batch listed in the file batch.
func (r *Runner) args(batch string) []string {
	args := []string{"--ignore-config", "--no-playlist", "-f", r.Formats}
	if r.QJS != "" {
		args = append(args, "--js-runtimes", "quickjs:"+r.QJS)
	}
	return append(args,
		"--cache-dir", r.Cache,
		"-o", filepath.Join(r.Parts, "%(id)s.%(format_id)s.%(ext)s"),
		"--no-mtime", "--newline", "--progress", "--no-simulate",
		"--socket-timeout", "20", "--retries", "3",
		"--progress-template", "download:"+progressMark+" %(info.id)s %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s",
		"--print", "before_dl:"+startMark+" %(id)s",
		"--print", "after_move:"+doneMark+" %(id)s %(filepath)s",
		"--batch-file", batch)
}

// command makes yt-dlp's command in a process group of its own, so that
// cancelling ctx kills it and the QuickJS it started.
func (r *Runner) command(ctx context.Context, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.Program, args...)
	cmd.Env = append(os.Environ(), r.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// errorLine is an error naming its song: "ERROR: [youtube] <id>: <message>".
var errorLine = regexp.MustCompile(`^ERROR: \[[^\]]+\] ([A-Za-z0-9_-]{11}): (.*)$`)

// Run downloads ids in order with one yt-dlp, calling event as yt-dlp
// reports. An id that isn't a YouTube video ID fails at once, as Other. When
// yt-dlp exits on its own every id has had an outcome by the time Run
// returns: a landed file or a failure, those it never reached failing with
// its last error, or as Other when it gave none. Cancelling ctx kills it and
// leaves the songs without an outcome as they were. The error is yt-dlp's
// exit or ctx's; with yt-dlp exiting 0 it is still an error when a song had
// no outcome of yt-dlp's or an id was invalid.
func (r *Runner) Run(ctx context.Context, ids []string, event func(Event)) error {
	if err := os.MkdirAll(r.Parts, 0o755); err != nil {
		return err
	}
	// Only one yt-dlp runs at a time, so any batch file still here is stale.
	old, _ := filepath.Glob(filepath.Join(r.Parts, "batch-*.txt"))
	for _, name := range old {
		os.Remove(name)
	}
	settled := map[string]bool{}
	inBatch := map[string]bool{}
	var list strings.Builder
	var invalid, unfinished bool
	for _, id := range ids {
		if !videoID.MatchString(id) {
			settled[id] = true
			invalid = true
			event(Event{ID: id, Failure: Other, Message: "not a YouTube video ID"})
			continue
		}
		inBatch[id] = true
		fmt.Fprintf(&list, "https://music.youtube.com/watch?v=%s\n", id)
	}
	if len(inBatch) == 0 {
		if invalid {
			return errors.New("no YouTube video IDs to download")
		}
		return nil
	}
	batch, err := os.CreateTemp(r.Parts, "batch-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(batch.Name())
	if _, err := batch.WriteString(list.String()); err != nil {
		batch.Close()
		return err
	}
	if err := batch.Close(); err != nil {
		return err
	}
	cmd := r.command(ctx, r.args(batch.Name()))
	// yt-dlp's output and errors share one pipe, so an error comes after the
	// progress lines written before it, and is told to the right song.
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	err = cmd.Start()
	pw.Close()
	if err != nil {
		pr.Close()
		return err
	}
	var current, lastErr string
	report := func(e Event) {
		if !inBatch[e.ID] || settled[e.ID] {
			return
		}
		if e.Path != "" || e.Failure != NoFailure {
			settled[e.ID] = true
		} else {
			current = e.ID
		}
		event(e)
	}
	rerr := lines(pr, func(line string) {
		if id, ok := strings.CutPrefix(line, startMark+" "); ok {
			if inBatch[id] {
				current = id
			}
			return
		}
		if e, ok := parseLine(line); ok {
			report(e)
			return
		}
		if !strings.HasPrefix(line, "ERROR:") {
			return
		}
		msg := strings.TrimSpace(strings.TrimPrefix(line, "ERROR:"))
		if m := errorLine.FindStringSubmatch(line); m != nil {
			report(Event{ID: m[1], Failure: Classify(line), Message: m[2]})
			return
		}
		lastErr = msg
		if current != "" {
			report(Event{ID: current, Failure: Classify(line), Message: msg})
		}
	})
	if rerr != nil {
		// Let yt-dlp finish writing rather than break its pipe.
		if r.Log != nil {
			r.Log.Warn("read yt-dlp's output", "err", rerr)
		}
		io.Copy(io.Discard, pr)
	}
	pr.Close()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Songs yt-dlp never reached fail as the run did, however it ended.
	for _, id := range ids {
		if settled[id] {
			continue
		}
		unfinished = true
		if lastErr == "" {
			report(Event{ID: id, Failure: Other, Message: "yt-dlp finished without this song"})
		} else {
			report(Event{ID: id, Failure: Classify(lastErr), Message: lastErr})
		}
	}
	switch {
	case err != nil && lastErr != "":
		return fmt.Errorf("yt-dlp: %w: %s", err, lastErr)
	case err != nil:
		return err
	case unfinished:
		return errors.New("yt-dlp finished without every song")
	case invalid:
		return errors.New("a song's ID isn't a YouTube video ID")
	}
	return nil
}

// lines calls f with each line read from r, skipping any longer than
// maxLine. The error is a failure to read.
func lines(r io.Reader, f func(string)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var line []byte
	var long bool
	for {
		chunk, more, err := br.ReadLine()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}
		if !long {
			if len(line)+len(chunk) > maxLine {
				long, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		if more {
			continue
		}
		if !long {
			f(string(line))
		}
		line, long = line[:0], false
	}
}

// parseLine reads one of the lines Boombrick asked for.
func parseLine(line string) (Event, bool) {
	switch {
	case strings.HasPrefix(line, progressMark+" "):
		f := strings.Fields(line)
		if len(f) != 6 {
			return Event{}, false
		}
		e := Event{ID: f[1], Done: number(f[2])}
		e.Total = number(f[3])
		if e.Total == 0 {
			e.Total = number(f[4])
		}
		e.Speed, _ = strconv.ParseFloat(f[5], 64) // NA reads as 0
		return e, true
	case strings.HasPrefix(line, doneMark+" "):
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 || f[2] == "" {
			return Event{}, false
		}
		return Event{ID: f[1], Path: f[2]}, true
	}
	return Event{}, false
}

// number reads one of yt-dlp's byte counts, which may be fractional, or NA
// for unknown, which reads as 0.
func number(s string) int64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

// Search asks yt-dlp to search YouTube for query, for when YouTube Music's
// own search can't be read. The results are songs: videos with their
// channel as the artist.
func (r *Runner) Search(ctx context.Context, query string) ([]ytmusic.Item, error) {
	args := []string{"--ignore-config", "--flat-playlist", "--no-warnings",
		"--print", "%(id)s\t%(duration)s\t%(channel)s\t%(title)s", "ytsearch10:" + query}
	out, err := r.command(ctx, args).Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("yt-dlp search: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("yt-dlp search: %w", err)
	}
	var items []ytmusic.Item
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.SplitN(line, "\t", 4)
		if len(f) != 4 {
			continue
		}
		for i := range f {
			if f[i] == "NA" { // yt-dlp's word for unknown
				f[i] = ""
			}
		}
		if !videoID.MatchString(f[0]) {
			continue
		}
		secs, _ := strconv.ParseFloat(f[1], 64) // unknown reads as 0
		items = append(items, ytmusic.Item{Kind: ytmusic.SongItem, ID: f[0], Title: f[3], Artist: f[2],
			Duration: time.Duration(secs) * time.Second, Thumb: "https://i.ytimg.com/vi/" + f[0] + "/hqdefault.jpg"})
	}
	return items, nil
}
