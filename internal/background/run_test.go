package background

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/resume"
)

// fakePlayer plays a script of states, the last of which stays, and records
// what it is asked.
type fakePlayer struct {
	mu                    sync.Mutex
	states                []player.State
	toggles, nexts, fades int
	sleep                 player.Sleep
	resumed               []player.Sleep
}

func (f *fakePlayer) State() player.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.states[0]
	if len(f.states) > 1 {
		f.states = f.states[1:]
	}
	return s
}

func (f *fakePlayer) Toggle() { f.mu.Lock(); f.toggles++; f.mu.Unlock() }
func (f *fakePlayer) Next()   { f.mu.Lock(); f.nexts++; f.mu.Unlock() }

func (f *fakePlayer) FadeOut(time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fades++
	f.states = []player.State{{Queued: true}}
}

func (f *fakePlayer) ResumeSleep(s player.Sleep) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed, f.sleep = append(f.resumed, s), s
}

func (f *fakePlayer) Sleep() player.Sleep {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sleep
}

func (f *fakePlayer) Snapshot() (player.Snapshot, bool) {
	return player.Snapshot{Queue: []player.Track{{Path: "/m/a.mp3"}}, Order: []int{0}, Frame: 42}, true
}

// record keeps what Run saved and said.
type record struct {
	mu        sync.Mutex
	saves     int
	handovers []resume.Handover
	ready     int
}

func (r *record) count(n *int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return *n
}

func options(p *fakePlayer, r *record) Options {
	return Options{
		Player: p,
		Save: func(player.Snapshot) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.saves++
			return nil
		},
		Handover: func(h resume.Handover) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.handovers = append(r.handovers, h)
			return nil
		},
		Ready: func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.ready++
		},
		Log:   slog.New(slog.DiscardHandler),
		Every: time.Millisecond, SaveEvery: time.Hour,
	}
}

var (
	playing     = player.State{Queued: true, Playing: true, Count: 2}
	loading     = player.State{Queued: true, Playing: true, Loading: true, Count: 2}
	stalled     = player.State{Queued: true, Playing: true, Stalled: true, Count: 2}
	gap         = player.State{Queued: true, Count: 2}
	waiting     = player.State{Queued: true, Waiting: true, Count: 2}
	waitingLast = player.State{Queued: true, Waiting: true, Index: 1, Count: 2}
	stoppedOn   = player.State{Queued: true, Waiting: true, Cued: true, Count: 2}
)

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// Run plays the queue and says so once; when Boombrick takes the music back
// it fades it out and saves its place as playing, with the sleep timer.
func TestTakenBack(t *testing.T) {
	p := &fakePlayer{states: []player.State{playing}}
	r := &record{}
	o := options(p, r)
	o.Sleep = player.Sleep{Mode: player.Sleep30, At: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, o) }()
	eventually(t, "the music to play", func() bool { return r.count(&r.ready) == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p.toggles != 1 || p.fades != 1 || !slices.Equal(p.resumed, []player.Sleep{o.Sleep}) {
		t.Fatalf("toggles %d, fades %d, sleep timers %v", p.toggles, p.fades, p.resumed)
	}
	if r.saves == 0 || !slices.Equal(r.handovers, []resume.Handover{{Playing: true, Sleep: o.Sleep}}) {
		t.Fatalf("saves %d, hand-overs %+v", r.saves, r.handovers)
	}
}

// Looking a second apart, as on the device, one look that falls between two
// tracks isn't the end, but two a second apart that hear nothing are. Until
// the music plays the player is looked at more often, so that Boombrick hears
// it plays soon.
func TestLooksASecondApart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &fakePlayer{states: []player.State{loading, playing, playing, gap, playing, gap}}
		r := &record{}
		o := options(p, r)
		o.Every = time.Second
		start := time.Now()
		var readyAt time.Duration
		o.Ready = func() { readyAt = time.Since(start) }
		if err := Run(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		if readyAt != 2*readyEvery {
			t.Fatalf("said it plays %v in, want %v", readyAt, 2*readyEvery)
		}
		// Looks at 0.25 s and 0.5 s, then a second apart: a gap at 2.5 s,
		// music again at 3.5 s, and nothing from 4.5 s.
		if took := time.Since(start); took != 5500*time.Millisecond {
			t.Fatalf("stopped %v in, want 5.5s", took)
		}
		if !slices.Equal(r.handovers, []resume.Handover{{}}) {
			t.Fatalf("hand-overs %+v", r.handovers)
		}
	})
}

// Taken back within the quiet second after the music stopped by itself, as at
// the sleep timer's fade, Run hands back the queue paused.
func TestTakenBackAfterItStoppedHandsBackPaused(t *testing.T) {
	p := &fakePlayer{states: []player.State{playing, gap}}
	r := &record{}
	o := options(p, r)
	o.Every = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, o) }()
	eventually(t, "the music to play", func() bool { return r.count(&r.ready) == 1 })
	time.Sleep(100 * time.Millisecond) // a few looks at the gap, well under a second
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{}}) {
		t.Fatalf("hand-overs %+v, want the queue paused", r.handovers)
	}
}

// Playback that stops by itself, as at the end of the queue, saves its place
// as stopped, lets CrossMix sleep again and ends Run. A gap under a second,
// between two tracks, doesn't.
func TestStoppedByItself(t *testing.T) {
	p := &fakePlayer{states: []player.State{playing, gap, gap, gap, playing, gap}}
	r := &record{}
	o := options(p, r)
	o.Every = 100 * time.Millisecond
	o.StayAwake = filepath.Join(t.TempDir(), "stay_awake")
	start := time.Now()
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < quiet {
		t.Fatalf("stopped after %v, the first gap is under a second", d)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{}}) || r.ready != 1 || r.saves == 0 {
		t.Fatalf("hand-overs %+v, ready %d, saves %d", r.handovers, r.ready, r.saves)
	}
	if _, err := os.Stat(o.StayAwake); !os.IsNotExist(err) {
		t.Fatal("stopped, the keep-awake file is still there")
	}
}

// A player that stops before the music ever played leaves the keep-awake file
// alone: Boombrick plays on.
func TestStoppedBeforeReadyKeepsCrossMixAwake(t *testing.T) {
	p := &fakePlayer{states: []player.State{gap}}
	r := &record{}
	o := options(p, r)
	o.Every = 100 * time.Millisecond
	o.StayAwake = filepath.Join(t.TempDir(), "stay_awake")
	if err := os.WriteFile(o.StayAwake, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{}}) || r.ready != 0 {
		t.Fatalf("hand-overs %+v, ready %d", r.handovers, r.ready)
	}
	if _, err := os.Stat(o.StayAwake); err != nil {
		t.Fatalf("stopped before it played, the keep-awake file is gone: %v", err)
	}
}

// A song still waiting for its download is skipped; a queue of nothing else
// ends Run.
func TestSkipsSongsNotDownloaded(t *testing.T) {
	p := &fakePlayer{states: []player.State{waiting, playing, waiting}}
	r := &record{}
	if err := Run(context.Background(), options(p, r)); err != nil {
		t.Fatal(err)
	}
	if p.nexts != 3 || !slices.Equal(r.handovers, []resume.Handover{{}}) {
		t.Fatalf("skipped %d, hand-overs %+v", p.nexts, r.handovers)
	}
}

// While the music plays its place is saved every so often, and CrossMix's
// keep-awake file is written again; taken back, the file stays, as
// Boombrick's launch.sh has just written it.
func TestSavesAndKeepsCrossMixAwake(t *testing.T) {
	p := &fakePlayer{states: []player.State{playing}}
	r := &record{}
	o := options(p, r)
	o.SaveEvery = 5 * time.Millisecond
	o.StayAwake = filepath.Join(t.TempDir(), "stay_awake")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, o) }()
	eventually(t, "three saves", func() bool { return r.count(&r.saves) >= 3 })
	// Removed, the file comes back.
	eventually(t, "the keep-awake file", func() bool { _, err := os.Stat(o.StayAwake); return err == nil })
	if err := os.Remove(o.StayAwake); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the keep-awake file again", func() bool { _, err := os.Stat(o.StayAwake); return err == nil })
	cancel()
	<-done
}

// A song waiting at the last place has nothing to skip on to: Next does
// nothing there, so Run stops at once, unless the queue repeats.
func TestWaitingAtTheLastSongStopsWithoutSkipping(t *testing.T) {
	p := &fakePlayer{states: []player.State{waitingLast}}
	r := &record{}
	if err := Run(context.Background(), options(p, r)); err != nil {
		t.Fatal(err)
	}
	if p.nexts != 0 || !slices.Equal(r.handovers, []resume.Handover{{}}) {
		t.Fatalf("skipped %d, hand-overs %+v", p.nexts, r.handovers)
	}
}

// A song waiting and stopped on, as the sleep timer leaves it, is silence,
// not a song to skip.
func TestWaitingAndCuedIsStopped(t *testing.T) {
	p := &fakePlayer{states: []player.State{stoppedOn}}
	r := &record{}
	if err := Run(context.Background(), options(p, r)); err != nil {
		t.Fatal(err)
	}
	if p.nexts != 0 || !slices.Equal(r.handovers, []resume.Handover{{}}) {
		t.Fatalf("skipped %d, hand-overs %+v", p.nexts, r.handovers)
	}
}

// A stream that is only loading hasn't played: nothing is said, and the sleep
// timer isn't resumed until it does.
func TestReadyWaitsForTheMusic(t *testing.T) {
	p := &fakePlayer{states: []player.State{loading}}
	r := &record{}
	o := options(p, r)
	o.Sleep = player.Sleep{Mode: player.Sleep30, At: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, o) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r.ready != 0 || len(p.resumed) != 0 {
		t.Fatalf("ready %d, sleep timers %v, before anything played", r.ready, p.resumed)
	}

	p = &fakePlayer{states: []player.State{loading, loading, loading, playing}}
	r = &record{}
	o = options(p, r)
	o.Sleep = player.Sleep{Mode: player.Sleep30, At: time.Now().Add(time.Hour)}
	ctx, cancel = context.WithCancel(context.Background())
	go func() { done <- Run(ctx, o) }()
	eventually(t, "the music to play", func() bool { return r.count(&r.ready) == 1 })
	cancel()
	<-done
	if r.ready != 1 || len(p.resumed) != 1 {
		t.Fatalf("ready %d, sleep timers %v", r.ready, p.resumed)
	}
}

// A stream stalled for StallLimit counts as stopped; one that picks up again
// starts the count over.
func TestStalledForTooLongIsStopped(t *testing.T) {
	p := &fakePlayer{states: []player.State{playing, stalled}}
	r := &record{}
	o := options(p, r)
	o.StallLimit = 20 * time.Millisecond
	o.StayAwake = filepath.Join(t.TempDir(), "stay_awake")
	if err := os.WriteFile(o.StayAwake, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{}}) {
		t.Fatalf("hand-overs %+v", r.handovers)
	}
	if _, err := os.Stat(o.StayAwake); !os.IsNotExist(err) {
		t.Fatal("stopped, the keep-awake file is still there")
	}

	// Five stalled looks, then playing for good: under the limit of eight.
	p = &fakePlayer{states: []player.State{stalled, stalled, stalled, stalled, stalled, playing, stalled, stalled, stalled, stalled, stalled, playing}}
	r = &record{}
	o = options(p, r)
	o.StallLimit = 8 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, o) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(r.handovers) != 1 || !r.handovers[0].Playing {
		t.Fatalf("hand-overs %+v, want taken back playing", r.handovers)
	}
}

// Taken back before the music ever played, the sleep timer handed over is
// handed back: the player has not been given it yet.
func TestTakenBackWhileLoadingKeepsTheSleepTimer(t *testing.T) {
	p := &fakePlayer{states: []player.State{loading}}
	r := &record{}
	o := options(p, r)
	o.Sleep = player.Sleep{Mode: player.Sleep30, At: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, o) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{Playing: true, Sleep: o.Sleep}}) {
		t.Fatalf("hand-overs %+v", r.handovers)
	}
}

// A stalled stream that is paused, as by the sleep timer's fade, is silence.
func TestPausedAndStalledIsStopped(t *testing.T) {
	p := &fakePlayer{states: []player.State{{Queued: true, Stalled: true, Count: 2}}}
	r := &record{}
	if err := Run(context.Background(), options(p, r)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{}}) {
		t.Fatalf("hand-overs %+v", r.handovers)
	}
}

// A stream that neither plays nor fails within StallLimit counts as stopped.
func TestLoadingForTooLongIsStopped(t *testing.T) {
	p := &fakePlayer{states: []player.State{playing, loading}}
	r := &record{}
	o := options(p, r)
	o.StallLimit = 20 * time.Millisecond
	o.StayAwake = filepath.Join(t.TempDir(), "stay_awake")
	if err := os.WriteFile(o.StayAwake, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{}}) || r.saves == 0 {
		t.Fatalf("hand-overs %+v, saves %d", r.handovers, r.saves)
	}
	if _, err := os.Stat(o.StayAwake); !os.IsNotExist(err) {
		t.Fatal("stopped, the keep-awake file is still there")
	}
}

// Repeating all, a song waiting at the last place is skipped to the first.
func TestWaitingAtTheLastSongRepeatingAllSkips(t *testing.T) {
	last := waitingLast
	last.Repeat = player.RepeatAll
	p := &fakePlayer{states: []player.State{last, playing}}
	r := &record{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, options(p, r)) }()
	eventually(t, "the music to play", func() bool { return r.count(&r.ready) == 1 })
	cancel()
	<-done
	if p.nexts != 1 {
		t.Fatalf("skipped %d, want 1", p.nexts)
	}
}

// A stream cued while it loads, paused or stopped on, is silence, not a
// stream to wait for.
func TestCuedAndLoadingIsStopped(t *testing.T) {
	p := &fakePlayer{states: []player.State{{Queued: true, Loading: true, Cued: true, Count: 2}}}
	r := &record{}
	if err := Run(context.Background(), options(p, r)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.handovers, []resume.Handover{{}}) {
		t.Fatalf("hand-overs %+v", r.handovers)
	}
}

// When the sound card fails, as when something else holds it, the log says
// so, and who holds it, before Run stops.
func TestAFailedSoundCardIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		busy := player.State{Queued: true, Count: 2, Err: errors.New("Device or resource busy")}
		p := &fakePlayer{states: []player.State{busy}}
		r := &record{}
		o := options(p, r)
		var log bytes.Buffer
		o.Log = slog.New(slog.NewTextHandler(&log, nil))
		o.SoundUsers = func() []string { return []string{"812 boombrick-spotify serve"} }
		if err := Run(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		if s := log.String(); !strings.Contains(s, "Device or resource busy") || !strings.Contains(s, "812 boombrick-spotify serve") {
			t.Fatalf("the log says %q", s)
		}
	})
}
