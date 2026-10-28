package settings

import (
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

// confirm is a dialog with two pills, the first to go ahead and the second
// to leave things as they are: Clear caches? and Update ready.
type confirm struct {
	dialog    widgets.Dialog
	pill      int
	focus     [2]anim.Value
	k         []float32
	shown     anim.Value
	closing   bool
	confirmed bool
	hintBar   [2][]widgets.Hint
}

// newConfirm opens d, whose two pills are A's actions; B takes the second.
func newConfirm(d widgets.Dialog, now time.Time) *confirm {
	c := &confirm{dialog: d, k: make([]float32, 2)}
	yes, no := d.Pills[0].Label, d.Pills[1].Label
	c.hintBar = [2][]widgets.Hint{
		{{Key: "A", Label: yes}, {Key: "B", Label: no}},
		{{Key: "A", Label: no}, {Key: "B", Label: no}},
	}
	c.focus[0].Set(1)
	c.shown.To(1, now, anim.Dialog, anim.EaseOutCubic)
	return c
}

// handle moves between the pills and presses one with A, reporting whether
// the frame changed.
func (c *confirm) handle(ev input.Event, now time.Time) bool {
	if c.closing || ev.Kind == input.Release {
		return false
	}
	switch ev.Button {
	case input.Left, input.Right:
		next := 1 - c.pill
		if ev.Button == input.Left && c.pill == 0 || ev.Button == input.Right && c.pill == 1 {
			return false
		}
		c.focus[c.pill].To(0, now, anim.Focus, anim.EaseOutCubic)
		c.pill = next
		c.focus[next].To(1, now, anim.Focus, anim.EaseOutCubic)
		return true
	case input.A:
		if ev.Kind == input.Press {
			c.confirmed = c.pill == 0
			c.close(now)
			return true
		}
	}
	return false
}

func (c *confirm) close(now time.Time) {
	c.closing = true
	c.shown.To(0, now, anim.Dialog, anim.EaseOutCubic)
}

func (c *confirm) gone(now time.Time) bool { return c.closing && c.shown.Done(now) }

func (c *confirm) animating(now time.Time) bool {
	return !c.shown.Done(now) || !c.focus[0].Done(now) || !c.focus[1].Done(now) || c.closing
}

func (c *confirm) draw(u *ui.Context) {
	for i := range c.focus {
		c.k[i] = float32(c.focus[i].At(u.Now))
	}
	widgets.DrawDialog(u, &c.dialog, float32(c.shown.At(u.Now)), c.k)
}

func (c *confirm) hints() []widgets.Hint { return c.hintBar[c.pill] }
