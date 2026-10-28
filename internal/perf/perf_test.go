package perf

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BOOMBRICK_PERF", "")
	if Enabled(dir) {
		t.Fatal("on with no file")
	}
	os.WriteFile(filepath.Join(dir, File), nil, 0o644)
	if !Enabled(dir) {
		t.Fatal("off with the file")
	}
	t.Setenv("BOOMBRICK_PERF", "1")
	if !Enabled(t.TempDir()) {
		t.Fatal("off with BOOMBRICK_PERF=1")
	}
}

func TestSpread(t *testing.T) {
	var ds []time.Duration
	for i := 100; i >= 1; i-- {
		ds = append(ds, time.Duration(i)*time.Millisecond)
	}
	m, p, w := spread(ds)
	if m != 51*time.Millisecond || p != 95*time.Millisecond || w != 100*time.Millisecond {
		t.Fatalf("spread = %v, %v, %v", m, p, w)
	}
	if m, p, w := spread(nil); m != 0 || p != 0 || w != 0 {
		t.Fatal("frames from none")
	}
}

// records keeps the log's records.
type records struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (r *records) Enabled(context.Context, slog.Level) bool { return true }
func (r *records) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *records) WithGroup(string) slog.Handler            { return r }
func (r *records) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec)
	return nil
}

// find returns the attributes of the first record of msg that ok accepts.
func (r *records) find(msg string, ok func(map[string]slog.Value) bool) map[string]slog.Value {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.recs {
		attrs := map[string]slog.Value{}
		rec.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value; return true })
		if rec.Message == msg && ok(attrs) {
			return attrs
		}
	}
	return nil
}

func anything(map[string]slog.Value) bool { return true }

func TestRecorder(t *testing.T) {
	h := &records{}
	r := Start(slog.New(h), time.Now().Add(-300*time.Millisecond), 20*time.Millisecond)
	r.Mark("window")
	for range 10 {
		r.Frame(4*time.Millisecond, 16*time.Millisecond)
	}
	// A summary can come before the frames; the one after them counts.
	framed := func(a map[string]slog.Value) bool { return a["frames"].Int64() > 0 }
	var attrs map[string]slog.Value
	for deadline := time.Now().Add(3 * time.Second); attrs == nil && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		attrs = h.find("perf", framed)
	}
	r.Close()
	if attrs == nil || attrs["frames"].Int64() != 10 {
		t.Fatalf("summary %v", attrs)
	}
	if attrs["draw_median_ms"].Float64() != 4 || attrs["frame_worst_ms"].Float64() != 16 {
		t.Fatalf("frame times %v", attrs)
	}
	if attrs["rss_mb"].Float64() <= 0 || attrs["rss_anon_mb"].Float64() <= 0 || attrs["heap_mb"].Float64() <= 0 {
		t.Fatalf("memory %v", attrs)
	}
	launch := h.find("perf launch", anything)
	if launch == nil || launch["stage"].String() != "window" || launch["since_start"].Duration() < 300*time.Millisecond {
		t.Fatalf("launch mark %v", launch)
	}
}

func TestANilRecorderDoesNothing(t *testing.T) {
	var r *Recorder
	r.Mark("window")
	r.Frame(time.Millisecond, time.Millisecond)
	r.Close()
}

func TestFrameDoesNotAllocate(t *testing.T) {
	r := Start(slog.New(&records{}), time.Now(), time.Hour)
	defer r.Close()
	if n := testing.AllocsPerRun(500, func() { r.Frame(time.Millisecond, time.Millisecond) }); n != 0 {
		t.Fatalf("Frame allocates %v times", n)
	}
}
