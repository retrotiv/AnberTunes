package views

import (
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
)

var t0 = time.Unix(1000, 0)

func grid(n int) *Grid {
	g := &Grid{Cols: 3, CellW: 100, CellH: 100, GapX: 10, GapY: 10, Pad: 5, ViewH: 250}
	g.SetCount(n)
	return g
}

func TestGridMoves(t *testing.T) {
	g := grid(8) // rows: 0 1 2 / 3 4 5 / 6 7
	for _, c := range []struct {
		b    input.Button
		ok   bool
		want int
	}{
		{input.Left, false, 0},
		{input.Up, false, 0},
		{input.Right, true, 1},
		{input.Right, true, 2},
		{input.Right, false, 2},
		{input.Down, true, 5},
		{input.Down, true, 7}, // no cell below: the last one
		{input.Down, false, 7},
		{input.Up, true, 4},
		{input.Left, true, 3},
	} {
		if ok := g.Move(c.b, t0); ok != c.ok || g.Focus() != c.want {
			t.Fatalf("%v: moved %v to %d, want %v to %d", c.b, ok, g.Focus(), c.ok, c.want)
		}
	}
}

func TestGridScrollsTheFocusedRowIntoView(t *testing.T) {
	g := grid(30)
	g.SetFocus(9, t0) // row 3: 330 to 430, plus the padding
	if got := g.scroll.Target(); got != 3*110+100+2*5-250 {
		t.Fatalf("scroll = %v, want the row's bottom at the view's", got)
	}
	g.SetFocus(0, t0)
	if got := g.scroll.Target(); got != 0 {
		t.Fatalf("scroll back at the top = %v, want 0", got)
	}
	if !g.Animating(t0.Add(anim.Focus/2)) || g.Animating(t0.Add(anim.Focus)) {
		t.Fatal("scrolling should animate for anim.Focus")
	}
}

func TestGridDrawsOnlyVisibleCellsFocusedLast(t *testing.T) {
	g := grid(30)
	g.SetFocus(4, t0)
	var drawn []int
	g.Draw(t0.Add(anim.Focus), 0, 0, func(i int, x, y, k float32) { drawn = append(drawn, i) })
	if len(drawn) == 0 || drawn[len(drawn)-1] != 4 {
		t.Fatalf("drawn %v: the focused cell goes last", drawn)
	}
	for _, i := range drawn {
		if i > 11 { // rows 0-3 can show in a 250 px view
			t.Fatalf("drew cell %d, which is out of view", i)
		}
	}
}

func TestSetCountKeepsFocusInRange(t *testing.T) {
	g := grid(10)
	g.SetFocus(9, t0)
	g.SetCount(4)
	if g.Focus() != 3 {
		t.Fatalf("focus = %d after shrinking to 4, want 3", g.Focus())
	}
	g.SetCount(0)
	if g.Focus() != 0 || g.Move(input.Down, t0) {
		t.Fatal("an empty grid has nothing to move to")
	}
}

func TestSetCountFocusKeepsTheItemWhereItWas(t *testing.T) {
	t0 := time.Unix(0, 0)
	g := Grid{Cols: 1, CellW: 100, CellH: 80, Pad: 10, ViewH: 300}
	g.SetCount(20)
	for range 6 {
		g.Move(input.Down, t0)
	}
	now := t0.Add(time.Second)
	before := g.scroll.At(now) - float64(g.rowTop(g.Focus()))
	k := g.value(g.Focus()).At(now)

	g.SetCountFocus(25, 9) // three items arrived above the focused one
	if g.Focus() != 9 {
		t.Fatalf("focus %d, want 9", g.Focus())
	}
	if after := g.scroll.At(now) - float64(g.rowTop(9)); after != before {
		t.Fatalf("the focused item moved on screen: %v from the view's top, was %v", after, before)
	}
	if got := g.value(9).At(now); got != k {
		t.Fatalf("focus strength %v after the move, want %v", got, k)
	}
}

func TestSetCountFocusClampsToTheEnd(t *testing.T) {
	g := Grid{Cols: 3, CellW: 100, CellH: 100, ViewH: 300}
	g.SetCount(9)
	g.SetCountFocus(4, 7)
	if g.Focus() != 3 || g.Count() != 4 {
		t.Fatalf("focus %d of %d, want 3 of 4", g.Focus(), g.Count())
	}
	if g.scroll.Target() > g.maxScroll() {
		t.Fatalf("scrolled to %v, past the end at %v", g.scroll.Target(), g.maxScroll())
	}
}
