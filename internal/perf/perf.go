// Package perf is perf mode: the launch's stages, then every 10 s the
// process's memory, CPU and frame times, logged for comparison with the
// performance budget. Off, it costs nothing.
package perf

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/metrics"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// File is the file in the program's folder that turns perf mode on.
const File = "perf"

// Enabled reports whether perf mode is on: a file named perf in dir, the
// program's folder, or BOOMBRICK_PERF=1.
func Enabled(dir string) bool {
	if os.Getenv("BOOMBRICK_PERF") == "1" {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, File))
	return err == nil
}

// Recorder takes frame times from the UI thread and logs a summary every
// interval from a goroutine of its own. A nil *Recorder records nothing, so
// callers need not check.
type Recorder struct {
	log     *slog.Logger
	started time.Time
	stop    chan struct{}
	done    chan struct{}

	mu     sync.Mutex
	frames []frame // since the last summary
	spare  []frame // the slice before, reused for the next
}

// frame is one frame's times: drawing it, and drawing and presenting it,
// which with vsync waits for the display.
type frame struct{ draw, total time.Duration }

// Start starts recording; started is when the process began.
func Start(log *slog.Logger, started time.Time, every time.Duration) *Recorder {
	r := &Recorder{log: log, started: started, stop: make(chan struct{}), done: make(chan struct{}),
		frames: make([]frame, 0, 1024), spare: make([]frame, 0, 1024)}
	go r.run(every)
	return r
}

// Mark logs a stage of the launch and how long after the process began it
// was reached.
func (r *Recorder) Mark(stage string) {
	if r == nil {
		return
	}
	r.log.Info("perf launch", "stage", stage, "since_start", time.Since(r.started).Round(time.Millisecond))
}

// Frame records a frame: draw is the drawing, total that and presenting it.
func (r *Recorder) Frame(draw, total time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.frames = append(r.frames, frame{draw, total})
	r.mu.Unlock()
}

// Close stops the summaries.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	close(r.stop)
	<-r.done
}

func (r *Recorder) run(every time.Duration) {
	defer close(r.done)
	t := time.NewTicker(every)
	defer t.Stop()
	last := usage{at: time.Now(), cpu: cpuTime()}
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
		}
		r.mu.Lock()
		fs := r.frames
		r.frames, r.spare = r.spare[:0], nil
		r.mu.Unlock()
		now := usage{at: time.Now(), cpu: cpuTime()}
		r.summary(fs, last, now)
		last = now
		r.mu.Lock()
		r.spare = fs[:0]
		r.mu.Unlock()
	}
}

// usage is the CPU time the process had used at a moment.
type usage struct {
	at  time.Time
	cpu time.Duration
}

var memory = []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}, {Name: "/gc/cycles/total:gc-cycles"}}

// summary logs the frames since the last one and the process's state.
func (r *Recorder) summary(fs []frame, from, to usage) {
	metrics.Read(memory)
	draw, total := make([]time.Duration, len(fs)), make([]time.Duration, len(fs))
	for i, f := range fs {
		draw[i], total[i] = f.draw, f.total
	}
	dm, dp, dw := spread(draw)
	tm, tp, tw := spread(total)
	r.log.Info("perf",
		"rss_mb", mb(rss()), "rss_anon_mb", mb(rssAnon()), "heap_mb", mb(int64(memory[0].Value.Uint64())), "gcs", memory[1].Value.Uint64(),
		"cpu_pct", cpuPercent(from, to), "frames", len(fs),
		"draw_median_ms", ms(dm), "draw_p95_ms", ms(dp), "draw_worst_ms", ms(dw),
		"frame_median_ms", ms(tm), "frame_p95_ms", ms(tp), "frame_worst_ms", ms(tw))
}

// spread is the median, 95th percentile and worst of ds, which it sorts; all
// zero when there are none.
func spread(ds []time.Duration) (median, p95, worst time.Duration) {
	if len(ds) == 0 {
		return 0, 0, 0
	}
	slices.Sort(ds)
	return ds[len(ds)/2], ds[(len(ds)*95-1)/100], ds[len(ds)-1]
}

// rss is the process's resident memory, in bytes, or 0 when it can't be read.
func rss() int64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	f := bytes.Fields(b)
	if len(f) < 2 {
		return 0
	}
	pages, _ := strconv.ParseInt(string(f[1]), 10, 64)
	return pages * int64(os.Getpagesize())
}

// rssAnon is the process's resident memory apart from mapped files, in bytes
// (RssAnon in /proc/self/status), or 0 when it can't be read.
func rssAnon() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("RssAnon:")); ok {
			f := bytes.Fields(rest) // "<n> kB"
			if len(f) < 1 {
				return 0
			}
			kb, _ := strconv.ParseInt(string(f[0]), 10, 64)
			return kb << 10
		}
	}
	return 0
}

// cpuTime is the user and system CPU time the process has used.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// cpuPercent is the CPU used between from and to, in percent of one core.
func cpuPercent(from, to usage) float64 {
	wall := to.at.Sub(from.at)
	if wall <= 0 {
		return 0
	}
	return round1(100 * float64(to.cpu-from.cpu) / float64(wall))
}

func mb(n int64) float64 { return round1(float64(n) / (1 << 20)) }

func ms(d time.Duration) float64 { return round1(float64(d) / float64(time.Millisecond)) }

func round1(x float64) float64 { return float64(int64(x*10+0.5)) / 10 }
