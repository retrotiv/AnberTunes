package spotify

import (
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

// Expired is the "Spotify session expired" dialog, over whatever is shown.
// Playback has already moved on past the Spotify songs; the dialog offers to
// sign in again.
type Expired struct {
	signIn  func()
	dialog  widgets.Dialog
	open    bool
	pill    int
	focus   [2]anim.Value
	k       []float32
	shown   anim.Value
	hintBar [2][]widgets.Hint
}

// NewExpired makes the dialog; Sign in again calls signIn.
func NewExpired(signIn func()) *Expired {
	e := &Expired{signIn: signIn, k: make([]float32, 2)}
	e.dialog = widgets.Dialog{Icon: "user", Circle: true, Title: "Spotify session expired",
		Body:  "Sign in again to keep streaming. Downloads and local music are not affected.",
		Pills: []widgets.DialogPill{{Label: "Sign in again"}, {Label: "Not now"}}}
	e.hintBar = [2][]widgets.Hint{
		{{Key: "A", Label: "Sign in again"}, {Key: "B", Label: "Not now"}},
		{{Key: "A", Label: "Not now"}, {Key: "B", Label: "Not now"}},
	}
	return e
}

// Open shows the dialog, focus on Sign in again.
func (e *Expired) Open(now time.Time) {
	e.open, e.pill = true, 0
	e.focus[0].Set(1)
	e.focus[1].Set(0)
	e.shown.To(1, now, anim.Dialog, anim.EaseOutCubic)
}

func (e *Expired) close(now time.Time) {
	e.open = false
	e.shown.To(0, now, anim.Dialog, anim.EaseOutCubic)
}

// IsOpen reports whether the dialog takes input.
func (e *Expired) IsOpen() bool { return e.open }

// Visible reports whether the dialog is drawn: open, or fading out.
func (e *Expired) Visible(now time.Time) bool { return e.open || e.shown.At(now) > 0 }

// Handle moves between the pills, presses one with A, and closes with B.
func (e *Expired) Handle(ev input.Event, now time.Time) bool {
	if !e.open || ev.Kind == input.Release {
		return false
	}
	switch ev.Button {
	case input.Left, input.Right:
		next := 0
		if ev.Button == input.Right {
			next = 1
		}
		if next == e.pill {
			return false
		}
		e.focus[e.pill].To(0, now, anim.Focus, anim.EaseOutCubic)
		e.pill = next
		e.focus[next].To(1, now, anim.Focus, anim.EaseOutCubic)
		return true
	case input.A:
		if ev.Kind != input.Press {
			return false
		}
		e.close(now)
		if e.pill == 0 {
			e.signIn()
		}
		return true
	case input.B:
		if ev.Kind != input.Press {
			return false
		}
		e.close(now)
		return true
	}
	return false
}

// Animating reports whether the dialog or its focus is moving.
func (e *Expired) Animating(now time.Time) bool {
	return !e.shown.Done(now) || !e.focus[0].Done(now) || !e.focus[1].Done(now)
}

// Draw draws the dialog over the frame, with its hints.
func (e *Expired) Draw(u *ui.Context) {
	shown := float32(e.shown.At(u.Now))
	if shown <= 0 {
		return
	}
	for i := range e.focus {
		e.k[i] = float32(e.focus[i].At(u.Now))
	}
	widgets.DrawDialog(u, &e.dialog, shown, e.k)
	widgets.HintBar(u, e.hintBar[e.pill])
}
