package widgets

import (
	"strings"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/uitest"
)

// keyName is what A does on kb's focused key, as text.
func keyName(kb *Keyboard) string {
	act, r := kb.Press()
	switch act {
	case SpaceKey:
		return "space"
	case BackspaceKey:
		return "backspace"
	case SearchKey:
		return "search"
	}
	return string(r)
}

func TestKeyboardMoves(t *testing.T) {
	kb := NewKeyboard()
	now := time.Unix(0, 0)
	steps := []struct {
		b     input.Button
		moved bool
		key   string
	}{
		{input.Up, false, "a"}, {input.Left, false, "a"},
		{input.Right, true, "b"}, {input.Down, true, "i"}, {input.Down, true, "p"}, {input.Down, true, "w"},
		{input.Down, true, "3"}, {input.Down, true, "'"},
		{input.Down, true, "space"}, {input.Right, true, "backspace"}, {input.Right, true, "search"},
		{input.Right, false, "search"}, {input.Down, false, "search"},
		{input.Up, true, ","}, {input.Right, true, "!"}, {input.Right, false, "!"},
		{input.Down, true, "search"}, {input.Left, true, "backspace"}, {input.Up, true, "&"},
		{input.A, false, "&"},
	}
	for i, s := range steps {
		if moved := kb.Move(s.b, now); moved != s.moved || keyName(&kb) != s.key {
			t.Fatalf("step %d, %v: moved %v on %q, want %v on %q", i, s.b, moved, keyName(&kb), s.moved, s.key)
		}
	}
}

func TestKeyboardShowsTheEndOfALongQuery(t *testing.T) {
	u, _ := uitest.NewContext(t, 64, 64)
	kb := NewKeyboard()
	q := strings.Repeat("night drive ", 6) + "theory"
	got := kb.tail(u, q, 200)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "theory") || u.TextWidth(got, fieldText) > 200 {
		t.Fatalf("tail = %q", got)
	}
	if kb.tail(u, "night", 200) != "night" {
		t.Fatal("a short query is shown whole")
	}
}

func TestKeyboard(t *testing.T) {
	u, off := uitest.NewContext(t, 480, 560)
	u.Accent = theme.Derive(265, 0.12)
	kb := NewKeyboard()
	t0 := time.Unix(0, 0)
	kb.Move(input.Right, t0)
	kb.Move(input.Down, t0) // "i"
	u.Now = t0.Add(anim.Focus)
	DrawBackground(u, FlatGround)
	kb.Draw(u, 8, 8, "night dri")
	if kb.Animating(u.Now) {
		t.Fatal("focus still moving after the focus time")
	}
	uitest.Golden(t, "keyboard", off.Image())
}
