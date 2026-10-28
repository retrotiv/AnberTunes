package downloads

import (
	"time"

	"github.com/ujjwal313/boombrick/internal/download"
	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

// What a pill of the failure dialog does.
const (
	updateRetry = iota
	retryIt
	removeIt
)

// bodies explain each failure.
var bodies = map[download.Failure]string{
	download.NeedsUpdate: "The downloader couldn’t read this track. It is usually fixed by updating the downloader.",
	download.Network:     "The connection dropped while this track was downloading. Check the Wi-Fi, then try again.",
	download.Unavailable: "YouTube Music won’t give this track. It may have been removed, made private or blocked where you are.",
	download.Other:       "Something went wrong saving this track. Updating the downloader often helps.",
}

// failure is the "Download failed" dialog over the tab.
type failure struct {
	entry   download.Entry
	dialog  widgets.Dialog
	actions []int
	pill    int
	focus   []anim.Value
	shown   anim.Value
	closing bool
	art     widgets.ArtSpec
	k       []float32        // the pills' focus strengths, for drawing
	hintBar [][]widgets.Hint // the hints with each pill focused
}

func newFailure(e download.Entry, now time.Time) *failure {
	f := &failure{entry: e, art: art(e.Song)}
	f.dialog = widgets.Dialog{Icon: "alert", IconColor: theme.GoldSoft, Title: "Download failed", Art: &f.art,
		ItemTitle: e.Song.Title, ItemDetail: e.Song.Artist + " · YouTube Music", Body: bodies[e.Failure]}
	if e.Failure == download.NeedsUpdate || e.Failure == download.Other {
		f.dialog.Pills = append(f.dialog.Pills, widgets.DialogPill{Icon: "refresh", Label: "Update and retry"})
		f.actions = append(f.actions, updateRetry)
	}
	f.dialog.Pills = append(f.dialog.Pills, widgets.DialogPill{Label: "Retry"}, widgets.DialogPill{Label: "Remove"})
	f.actions = append(f.actions, retryIt, removeIt)
	f.focus = make([]anim.Value, len(f.actions))
	f.k = make([]float32, len(f.actions))
	for i, a := range f.actions {
		h := []widgets.Hint{{Key: "A", Label: f.dialog.Pills[i].Label}}
		if a != retryIt {
			h = append(h, widgets.Hint{Key: "X", Label: "Retry"})
		}
		f.hintBar = append(f.hintBar, append(h, widgets.Hint{Key: "B", Label: "Close"}))
	}
	f.focus[0].Set(1)
	f.shown.To(1, now, anim.Dialog, anim.EaseOutCubic)
	return f
}

// handle moves between the pills, presses one with A, retries with X and
// closes with B.
func (f *failure) handle(d *Downloads, ev input.Event, now time.Time) bool {
	if f.closing {
		return false
	}
	switch ev.Button {
	case input.Left, input.Right:
		next := f.pill - 1
		if ev.Button == input.Right {
			next = f.pill + 1
		}
		if next < 0 || next >= len(f.actions) {
			return false
		}
		f.focus[f.pill].To(0, now, anim.Focus, anim.EaseOutCubic)
		f.pill = next
		f.focus[next].To(1, now, anim.Focus, anim.EaseOutCubic)
		return true
	}
	if ev.Kind != input.Press {
		return false
	}
	switch ev.Button {
	case input.A:
		f.do(d, f.actions[f.pill], now)
	case input.X:
		f.do(d, retryIt, now)
	case input.B:
		f.close(now)
	default:
		return false
	}
	return true
}

func (f *failure) do(d *Downloads, action int, now time.Time) {
	id := f.entry.Song.VideoID
	switch action {
	case updateRetry:
		d.src.Retry(id, true)
	case retryIt:
		d.src.Retry(id, false)
	case removeIt:
		d.remove(f.entry)
	}
	f.close(now)
}

func (f *failure) close(now time.Time) {
	f.closing = true
	f.shown.To(0, now, anim.Dialog, anim.EaseOutCubic)
}

// gone reports whether the dialog has closed and faded out.
func (f *failure) gone(now time.Time) bool { return f.closing && f.shown.Done(now) }

func (f *failure) animating(now time.Time) bool {
	if !f.shown.Done(now) {
		return true
	}
	for i := range f.focus {
		if !f.focus[i].Done(now) {
			return true
		}
	}
	return f.closing // one more frame clears it
}

func (f *failure) draw(u *ui.Context) {
	for i := range f.focus {
		f.k[i] = float32(f.focus[i].At(u.Now))
	}
	widgets.DrawDialog(u, &f.dialog, float32(f.shown.At(u.Now)), f.k)
}

func (f *failure) hints() []widgets.Hint { return f.hintBar[f.pill] }
