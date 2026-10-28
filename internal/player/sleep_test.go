package player

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// alarms stands in for time.AfterFunc: it keeps each timer set, to be rung by
// the test.
type alarms struct {
	mu  sync.Mutex
	set []*alarm
}

type alarm struct {
	d       time.Duration
	f       func()
	stopped bool
}

func (a *alarm) Stop() bool { a.stopped = true; return true }

func (a *alarms) after(d time.Duration, f func()) stopper {
	a.mu.Lock()
	defer a.mu.Unlock()
	al := &alarm{d: d, f: f}
	a.set = append(a.set, al)
	return al
}

// last is the timer set last.
func (a *alarms) last(t *testing.T) *alarm {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.set) == 0 {
		t.Fatal("no timer was set")
	}
	return a.set[len(a.set)-1]
}

func sleepyPlayer(t *testing.T, lengths map[string]int64) (*Player, *files, *alarms) {
	t.Helper()
	p, f := newPlayer(t, lengths)
	a := &alarms{}
	p.after = a.after
	return p, f, a
}

func TestTimedSleepFadesOutAndPauses(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": long})
	p.Play(queue("a"), 0)
	settled(t, p)
	p.SetSleep(Sleep15)
	if s := p.Sleep(); s.Mode != Sleep15 || time.Until(s.At) < 14*time.Minute {
		t.Fatalf("sleep = %+v, want 15 minutes from now", s)
	}
	al := a.last(t)
	if al.d != 15*time.Minute-sleepFade {
		t.Fatalf("timer set for %v, want the fade's start, %v", al.d, 15*time.Minute-sleepFade)
	}
	al.f()
	if p.Sleep().Mode != SleepOff {
		t.Error("the timer stays set once its fade has begun")
	}
	eventually(t, "the fade to pause playback", func() bool { return !p.State().Playing })
}

func TestPlayDuringTheFadeKeepsPlaying(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": long})
	p.Play(queue("a"), 0)
	settled(t, p)
	p.SetSleep(Sleep30)
	a.last(t).f()
	p.Toggle()
	time.Sleep(time.Second) // longer than the fade takes against nullOut
	if !p.State().Playing {
		t.Fatal("playback paused though play was pressed during the fade")
	}
}

func TestSettingSleepAgainStartsItAgain(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": long})
	p.Play(queue("a"), 0)
	p.SetSleep(Sleep15)
	first := a.last(t)
	p.SetSleep(Sleep60)
	if !first.stopped {
		t.Error("the first timer still runs")
	}
	if got := a.last(t).d; got != time.Hour-sleepFade {
		t.Errorf("new timer set for %v, want %v", got, time.Hour-sleepFade)
	}
	p.SetSleep(SleepOff)
	if p.Sleep().Mode != SleepOff || !a.last(t).stopped {
		t.Error("SleepOff left a timer running")
	}
}

func TestSleepAtTheEndOfTheTrack(t *testing.T) {
	p, f, _ := sleepyPlayer(t, map[string]int64{"a": short, "b": short})
	p.SetSleep(SleepTrack)
	p.Play(queue("a", "b"), 0)
	eventually(t, "a stop at the end of a", func() bool {
		s := p.State()
		return s.Track.Path == "b" && !s.Playing
	})
	time.Sleep(100 * time.Millisecond)
	if s := p.State(); s.Playing || s.Track.Path != "b" {
		t.Fatalf("state after the stop = %+v, want b cued", s)
	}
	if p.Sleep().Mode != SleepOff {
		t.Error("the timer stays set after it stopped playback")
	}
	if got := f.log(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("opened %v, want a, then b cued", got)
	}
}

func TestSleepAtTheEndOfTheAlbum(t *testing.T) {
	p, f, _ := sleepyPlayer(t, map[string]int64{"a1": short, "a2": short, "b1": short})
	p.SetSleep(SleepAlbum)
	p.Play([]Track{{Path: "a1", Album: "A"}, {Path: "a2", Album: "A"}, {Path: "b1", Album: "B"}}, 0)
	eventually(t, "a stop at the end of album A", func() bool {
		s := p.State()
		return s.Track.Path == "b1" && !s.Playing
	})
	settled(t, p)
	if got := f.log(); !slices.Equal(got, []string{"a1", "a2", "b1"}) {
		t.Errorf("opened %v, want a1, a2, then b1 cued", got)
	}
}

// The fade's time comes while playback waits for a download: the song lands
// paused rather than playing on with the timer spent.
func TestSleepWhileWaitingForADownload(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": short, "b": long})
	p.Play([]Track{{Path: "a", Title: "a"}, {VideoID: "v", Title: "b"}}, 0)
	eventually(t, "waiting on b", func() bool { return p.State().Waiting })
	p.SetSleep(Sleep15)
	a.last(t).f()
	p.Resolve("v", "b")
	time.Sleep(time.Second)
	if s := p.State(); s.Playing || s.Track.Path != "b" {
		t.Fatalf("after the timer's time: %+v, want b cued", s)
	}
}

// A track that ends during the fade without a gapless hand-over (here the
// next file won't open) stops playback there, the next one cued.
func TestTheFadeStopsAtATrackChange(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": 200000, "c": long})
	p.Play(queue("a", "x", "c"), 0) // x won't open
	settled(t, p)
	p.SetSleep(Sleep15)
	a.last(t).f()
	eventually(t, "a stop after a", func() bool { s := p.State(); return s.Track.Path != "a" && !s.Playing })
	time.Sleep(time.Second)
	if s := p.State(); s.Playing {
		t.Fatalf("%s plays at full volume after the sleep timer's fade", s.Track.Path)
	}
}

// End of album with repeat all: going round to the first track is the
// album's end.
func TestEndOfAlbumWithRepeatAll(t *testing.T) {
	p, f, _ := sleepyPlayer(t, map[string]int64{"a1": short, "a2": short})
	p.CycleRepeat() // all
	p.SetSleep(SleepAlbum)
	p.Play([]Track{{Path: "a1", Album: "A"}, {Path: "a2", Album: "A"}}, 0)
	// Opened: a1, a2 to follow it, a1 cued at the stop, and a2 to follow
	// that.
	eventually(t, "a stop at the album's end", func() bool {
		s := p.State()
		return s.Track.Path == "a1" && !s.Playing && len(f.log()) >= 4
	})
	n := len(f.log())
	time.Sleep(200 * time.Millisecond)
	if p.State().Playing || len(f.log()) != n || p.Sleep().Mode != SleepOff {
		t.Fatalf("after the stop: playing %v, opened %v, timer %v", p.State().Playing, f.log(), p.Sleep().Mode)
	}
}

// A track no longer the last before the timer stops, as when one of its album
// is added after it, loses the fade at its end.
func TestTheEndFadeGoesWhenATrackFollows(t *testing.T) {
	p, _, _ := sleepyPlayer(t, map[string]int64{"a1": long, "a2": long})
	p.SetSleep(SleepAlbum)
	p.Play([]Track{{Path: "a1", Album: "A"}}, 0)
	settled(t, p)
	if !p.endFade {
		t.Fatal("no fade at the end of the album's last track")
	}
	p.Enqueue([]Track{{Path: "a2", Album: "A"}})
	if p.endFade {
		t.Fatal("a1 still fades out with a2 to follow")
	}
}

// Play pressed when a timed fade has already paused the engine, just before
// the fade's time is up, still keeps the music.
func TestPlayAtTheVeryEndOfTheFade(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": long})
	p.Play(queue("a"), 0)
	settled(t, p)
	p.SetSleep(Sleep30)
	a.last(t).f()
	eventually(t, "the fade to pause playback", func() bool { return !p.State().Playing })
	p.Toggle()
	if !p.State().Playing {
		t.Fatal("play at the end of the fade didn't play")
	}
}

// Play during the fade at the end of the track an end-of-track timer stops
// at keeps playing, with the timer off.
func TestPlayDuringTheEndFadeKeepsPlaying(t *testing.T) {
	p, _, _ := sleepyPlayer(t, map[string]int64{"a": 4 * 48000, "b": long})
	p.SetSleep(SleepTrack)
	p.Play(queue("a", "b"), 0)
	settled(t, p)
	p.mu.Lock()
	p.engine.SeekFrame(p.curFrames - 48000) // a second from its end, behind the player's back
	p.mu.Unlock()
	eventually(t, "the seek", func() bool { return p.State().Elapsed >= 2*time.Second })
	p.Toggle()
	if p.Sleep().Mode != SleepOff {
		t.Fatal("the timer stays set")
	}
	eventually(t, "b to play on", func() bool { s := p.State(); return s.Track.Path == "b" && s.Playing })
}

func TestTurningSleepOffPrimesTheNextTrack(t *testing.T) {
	p, f, _ := sleepyPlayer(t, map[string]int64{"a": long, "b": short})
	p.SetSleep(SleepTrack)
	p.Play(queue("a", "b"), 0)
	time.Sleep(50 * time.Millisecond)
	if got := f.log(); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("opened %v while the timer waits for a's end, want only a", got)
	}
	p.SetSleep(SleepOff)
	eventually(t, "b to be primed", func() bool { return slices.Contains(f.log(), "b") })
}

// A sleep timer carried over from another process stops playback when it
// would have there.
func TestResumeSleepKeepsItsTime(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": long})
	p.Play(queue("a"), 0)
	settled(t, p)
	at := time.Now().Add(20 * time.Minute)
	p.ResumeSleep(Sleep{Mode: Sleep30, At: at})
	if s := p.Sleep(); s.Mode != Sleep30 || s.At.Sub(at).Abs() > time.Second {
		t.Fatalf("sleep = %+v, want 30 min ending at %v", s, at)
	}
	al := a.last(t)
	if want := time.Until(at) - sleepFade; al.d < want || al.d > want+time.Second {
		t.Fatalf("timer set for %v, want about %v", al.d, want)
	}
	al.f()
	eventually(t, "the fade to pause playback", func() bool { return !p.State().Playing })
}

// The carried end is kept as the clock's now plus the time left, so a wall
// clock step afterwards doesn't move it.
func TestResumeSleepStoresNowPlusTimeLeft(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": long})
	p.Play(queue("a"), 0)
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	p.now = func() time.Time { return now }
	left := 20 * time.Minute
	p.ResumeSleep(Sleep{Mode: Sleep30, At: now.Add(left)})
	if s := p.Sleep(); s.Mode != Sleep30 || !s.At.Equal(now.Add(left)) {
		t.Fatalf("sleep = %+v, want it to end at %v", s, now.Add(left))
	}
	if d := a.last(t).d; d != left-sleepFade {
		t.Fatalf("timer set for %v, want %v", d, left-sleepFade)
	}
}

// A time already past fades at once; the end of an album carries over as it
// was set.
func TestResumeSleepPastAndAtTheEnd(t *testing.T) {
	p, _, a := sleepyPlayer(t, map[string]int64{"a": long})
	p.Play(queue("a"), 0)
	p.ResumeSleep(Sleep{Mode: Sleep15, At: time.Now().Add(-time.Minute)})
	if d := a.last(t).d; d != 0 {
		t.Fatalf("a time already past set a timer for %v, want 0", d)
	}
	p.ResumeSleep(Sleep{Mode: SleepAlbum})
	if s := p.Sleep(); s.Mode != SleepAlbum || !s.At.IsZero() {
		t.Fatalf("sleep = %+v, want the end of the album", s)
	}
}

// FadeOut fades the music out and pauses it, keeping the queue; with nothing
// playing it does nothing.
func TestFadeOutPauses(t *testing.T) {
	p, _, _ := sleepyPlayer(t, map[string]int64{"a": long})
	p.FadeOut(10 * time.Millisecond) // nothing queued
	p.Play(queue("a"), 0)
	settled(t, p)
	eventually(t, "playing", func() bool { return p.State().Playing })
	p.FadeOut(10 * time.Millisecond)
	eventually(t, "the fade to pause playback", func() bool { return !p.State().Playing })
	if _, ok := p.Snapshot(); !ok {
		t.Fatal("the queue went with the fade")
	}
}

// The end-of-track timer stops before a song still downloading: the song waits
// and is stopped on.
func TestSleepAtTheEndOfTheTrackBeforeAPendingSong(t *testing.T) {
	p, _, _ := sleepyPlayer(t, map[string]int64{"a": short})
	p.SetSleep(SleepTrack)
	p.Play([]Track{{Path: "a"}, {VideoID: "v"}}, 0)
	eventually(t, "the stop before v", func() bool {
		s := p.State()
		return s.Index == 1 && s.Waiting && s.Cued
	})
}
