// Package views lays out scrolling grids and lists of focusable cells.
package views

import (
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/focus"
)

// Grid is a grid of equal cells with one focused; a list is a grid of one
// column. It scrolls as little as possible to keep the focused row in view,
// in 120 ms. Layout fields are in design pixels.
type Grid struct {
	Cols         int
	CellW, CellH float32
	GapX, GapY   float32
	Pad          float32 // space above the first row and around a scrolled-to row, for the focus ring
	ViewH        float32 // the visible height

	count, focus int
	active       bool
	started      bool
	scroll       anim.Value
	strength     map[int]*anim.Value // focus strength of cells that have had focus
}

// Count is the number of cells.
func (g *Grid) Count() int { return g.count }

// Focus is the focused cell.
func (g *Grid) Focus() int { return g.focus }

// SetCount sets the number of cells, keeping focus where it is if that cell
// still exists. A new grid starts active, with focus on the first cell.
func (g *Grid) SetCount(n int) {
	if !g.started {
		g.started, g.active = true, true
	}
	g.count = n
	f := min(g.focus, max(n-1, 0))
	if f != g.focus {
		clear(g.strength)
		g.focus = f
	}
	if g.active {
		g.value(f).Set(1)
	}
	g.scroll.Set(min(g.scroll.Target(), g.maxScroll()))
}

// SetCountFocus sets the number of cells and moves focus to cell f at once,
// as when the list behind the grid was replaced and the focused item now sits
// elsewhere in it. The view scrolls by as much as the item moved, so it stays
// where it was on screen, and its focus strength comes with it.
func (g *Grid) SetCountFocus(n, f int) {
	f = min(max(f, 0), max(n-1, 0))
	if !g.started || f == g.focus {
		g.SetCount(n)
		return
	}
	k := g.value(g.focus)
	clear(g.strength) // every other cell's item may have moved too
	g.strength[f] = k
	shift := float64(g.rowTop(f) - g.rowTop(g.focus))
	g.count, g.focus = n, f
	g.scroll.Shift(shift)
	if t := g.scroll.Target(); t < 0 || t > g.maxScroll() {
		g.scroll.Set(min(max(t, 0), g.maxScroll()))
	}
}

// SetActive gives the grid focus or takes it away, as when focus moves to
// buttons beside it; the focused cell's ring follows.
func (g *Grid) SetActive(on bool, now time.Time) {
	g.active = on
	k := 0.0
	if on {
		k = 1
	}
	g.value(g.focus).To(k, now, anim.Focus, anim.EaseOutCubic)
}

// SetFocus moves focus to cell i and scrolls its row into view.
func (g *Grid) SetFocus(i int, now time.Time) {
	if i < 0 || i >= g.count || i == g.focus {
		return
	}
	g.value(g.focus).To(0, now, anim.Focus, anim.EaseOutCubic)
	g.focus = i
	if g.active {
		g.value(i).To(1, now, anim.Focus, anim.EaseOutCubic)
	}
	top := float64(g.rowTop(i))
	g.scroll.To(focus.Scroll(g.scroll.Target(), top, top+float64(g.CellH+2*g.Pad), float64(g.ViewH)),
		now, anim.Focus, anim.EaseOutCubic)
	g.prune(now)
}

// Move moves focus with a D-pad button. It reports false when there is
// nothing that way, so the screen can move focus out of the grid. Down from
// the second-last row of a ragged grid lands on the last cell.
func (g *Grid) Move(b input.Button, now time.Time) bool {
	f, c := g.focus, g.Cols
	switch b {
	case input.Left:
		if f%c == 0 {
			return false
		}
		f--
	case input.Right:
		if f%c == c-1 || f+1 >= g.count {
			return false
		}
		f++
	case input.Up:
		if f < c {
			return false
		}
		f -= c
	case input.Down:
		if f/c == (g.count-1)/c {
			return false
		}
		f = min(f+c, g.count-1)
	default:
		return false
	}
	if g.count == 0 {
		return false
	}
	g.SetFocus(f, now)
	return true
}

// Animating reports whether focus or scrolling is still moving.
func (g *Grid) Animating(now time.Time) bool {
	if !g.scroll.Done(now) {
		return true
	}
	for _, v := range g.strength {
		if !v.Done(now) {
			return true
		}
	}
	return false
}

// Draw calls cell for each cell in view, the focused one last so its ring and
// glow sit on top, with the cell's top-left and focus strength. The first row
// starts Pad below top; the caller clips to the view.
func (g *Grid) Draw(now time.Time, x, top float32, cell func(i int, x, y, k float32)) {
	if g.count == 0 {
		return
	}
	off := float32(g.scroll.At(now))
	rowH := g.CellH + g.GapY
	first := max(int((off-g.Pad)/rowH), 0) * g.Cols
	last := min((int((off+g.ViewH)/rowH)+1)*g.Cols, g.count)
	at := func(i int) (float32, float32) {
		return x + float32(i%g.Cols)*(g.CellW+g.GapX), top + g.Pad + g.rowTop(i) - off
	}
	for i := first; i < last; i++ {
		if i == g.focus {
			continue
		}
		cx, cy := at(i)
		cell(i, cx, cy, float32(g.value(i).At(now)))
	}
	if g.focus >= first && g.focus < last {
		cx, cy := at(g.focus)
		cell(g.focus, cx, cy, float32(g.value(g.focus).At(now)))
	}
}

func (g *Grid) rowTop(i int) float32 { return float32(i/g.Cols) * (g.CellH + g.GapY) }

// maxScroll is the furthest the grid scrolls: its last row, with padding,
// at the bottom of the view.
func (g *Grid) maxScroll() float64 {
	if g.count == 0 {
		return 0
	}
	return max(0, float64(g.rowTop(g.count-1)+g.CellH+2*g.Pad-g.ViewH))
}

func (g *Grid) value(i int) *anim.Value {
	if g.strength == nil {
		g.strength = map[int]*anim.Value{}
	}
	v, ok := g.strength[i]
	if !ok {
		v = &anim.Value{}
		g.strength[i] = v
	}
	return v
}

// prune forgets cells that have faded out, so long lists don't keep a value
// for every cell ever focused.
func (g *Grid) prune(now time.Time) {
	for i, v := range g.strength {
		if i != g.focus && v.Done(now) && v.Target() == 0 {
			delete(g.strength, i)
		}
	}
}
