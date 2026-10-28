package widgets

import (
	"time"
	"unicode/utf8"

	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// The keyboard fits the 414 px between the tabs and the hint bar: keys are
// 44 px with 8 between them, the field 46 high and the text Figtree 24.
const (
	KeyboardW = 7*keySize + 6*keyGap

	keySize   = 44
	keyGap    = 8
	keyRadius = 9
	fieldH    = 46
	fieldGap  = 12
	// The bottom row is split 2:1:2 between Space, Backspace and Search.
	bottomPart = (KeyboardW - 2*keyGap) / 5
)

var (
	keyText   = text.Style{Face: text.Figtree, Size: 24}
	fieldText = text.Style{Face: text.Figtree, Size: 24}
)

// keyRows are the character keys.
var keyRows = [6]string{"abcdefg", "hijklmn", "opqrstu", "vwxyz01", "2345678", "9'-&.,!"}

// Moving between the last row of characters and the bottom row lands on the
// key nearest the middle of the one left.
var (
	downTo = [7]int{0, 0, 0, 1, 2, 2, 2}
	upFrom = [3]int{1, 3, 5}
)

const keyCount = 6*7 + 3

// KeyAction is what A does on the focused key.
type KeyAction uint8

const (
	TypeKey KeyAction = iota
	SpaceKey
	BackspaceKey
	SearchKey
)

// Keyboard is the on-screen keyboard and its focus. Rows 0 to 5 are the
// characters; row 6 is Space, Backspace and Search.
type Keyboard struct {
	row, col int
	active   bool
	k        [keyCount]anim.Value // focus strength, by key
	query    string               // the query last drawn, and the end of it that fits
	shown    string
}

// NewKeyboard returns a keyboard with focus on "a".
func NewKeyboard() Keyboard {
	kb := Keyboard{active: true}
	kb.k[0].Set(1)
	return kb
}

func (kb *Keyboard) index() int {
	if kb.row == 6 {
		return 6*7 + kb.col
	}
	return kb.row*7 + kb.col
}

// Move moves focus with a D-pad button, reporting false at an edge.
func (kb *Keyboard) Move(b input.Button, now time.Time) bool {
	r, c := kb.row, kb.col
	switch b {
	case input.Left:
		if c == 0 {
			return false
		}
		c--
	case input.Right:
		if r < 6 && c == 6 || r == 6 && c == 2 {
			return false
		}
		c++
	case input.Up:
		switch r {
		case 0:
			return false
		case 6:
			r, c = 5, upFrom[c]
		default:
			r--
		}
	case input.Down:
		switch r {
		case 6:
			return false
		case 5:
			r, c = 6, downTo[c]
		default:
			r++
		}
	default:
		return false
	}
	kb.k[kb.index()].To(0, now, anim.Focus, anim.EaseOutCubic)
	kb.row, kb.col = r, c
	if kb.active {
		kb.k[kb.index()].To(1, now, anim.Focus, anim.EaseOutCubic)
	}
	return true
}

// Press is what A does on the focused key, and the character it types.
func (kb *Keyboard) Press() (KeyAction, rune) {
	if kb.row == 6 {
		return [3]KeyAction{SpaceKey, BackspaceKey, SearchKey}[kb.col], 0
	}
	return TypeKey, rune(keyRows[kb.row][kb.col])
}

// SetActive gives the keyboard focus or takes it away, as when focus moves
// to the results beside it.
func (kb *Keyboard) SetActive(on bool, now time.Time) {
	kb.active = on
	k := 0.0
	if on {
		k = 1
	}
	kb.k[kb.index()].To(k, now, anim.Focus, anim.EaseOutCubic)
}

// Animating reports whether focus is moving.
func (kb *Keyboard) Animating(now time.Time) bool {
	for i := range kb.k {
		if !kb.k[i].Done(now) {
			return true
		}
	}
	return false
}

// Draw draws the search field holding query and, under it, the keys, from
// (x, y).
func (kb *Keyboard) Draw(u *ui.Context, x, y float32, query string) {
	now := u.Now
	field := gfx.Rect{X: x, Y: y, W: KeyboardW, H: fieldH}
	u.Shape(raster.Shape{W: KeyboardW, H: fieldH, Radius: fieldH / 2, Kind: raster.Ring, RingOffset: -1.5, RingWidth: 1.5},
		field, fade(u.Accent.Color, 0.7))
	u.Icon("search", 22, 2, false, x+16, y+(fieldH-22)/2, theme.Ink2)
	tx := x + 16 + 22 + 10
	shown := kb.tail(u, query, x+KeyboardW-20-tx-4)
	tw := u.Text(shown, fieldText, tx, u.CenterY(fieldText, y, fieldH), theme.Paper)
	u.Fill(gfx.Rect{X: tx + tw + 2, Y: y + (fieldH-24)/2, W: 2, H: 24}, u.Accent.Color)

	top := y + fieldH + fieldGap
	for r, keys := range keyRows {
		for c := range len(keys) {
			box := gfx.Rect{X: x + float32(c)*(keySize+keyGap), Y: top + float32(r)*(keySize+keyGap), W: keySize, H: keySize}
			key(u, box, float32(kb.k[r*7+c].At(now)), false)
			s := keys[c : c+1]
			u.Text(s, keyText, box.X+(keySize-u.TextWidth(s, keyText))/2, u.CenterY(keyText, box.Y, keySize), theme.Paper)
		}
	}
	bx, by := x, top+6*(keySize+keyGap)
	for i, w := range [3]float32{2 * bottomPart, bottomPart, 2 * bottomPart} {
		box := gfx.Rect{X: bx, Y: by, W: w, H: keySize}
		key(u, box, float32(kb.k[6*7+i].At(now)), i == 2)
		iy := by + (keySize-24)/2
		switch i {
		case 0:
			u.Icon("space", 24, 2, false, bx+(w-24)/2, iy, theme.Paper)
		case 1:
			u.Icon("backspace", 24, 2, false, bx+(w-24)/2, iy, theme.Paper)
		case 2:
			lw := 22 + 8 + u.TextWidth("Search", keyText)
			lx := bx + (w-lw)/2
			u.Icon("search", 22, 2, false, lx, by+(keySize-22)/2, u.Accent.Color)
			u.Text("Search", keyText, lx+30, u.CenterY(keyText, by, keySize), u.Accent.Color)
		}
		bx += w + keyGap
	}
}

// key draws a key's face: a 1.5 px border of paper at 20 %, or of the accent
// when accent is set; focus k tints it and brings in a 2 px accent border.
// What focus has faded out entirely isn't drawn.
func key(u *ui.Context, box gfx.Rect, k float32, accent bool) {
	shape := raster.Shape{W: int(box.W), H: int(box.H), Radius: keyRadius, Kind: raster.Fill}
	if c := fade(u.Accent.Color, 0.14*k); c.A > 0 {
		u.Shape(shape, box, c)
	}
	rest := theme.PaperA(0.20)
	if accent {
		rest = u.Accent.Color
	}
	shape.Kind, shape.RingOffset, shape.RingWidth = raster.Ring, -1.5, 1.5
	if c := fade(rest, 1-k); c.A > 0 {
		u.Shape(shape, box, c)
	}
	shape.RingOffset, shape.RingWidth = -2, 2
	if c := fade(u.Accent.Color, k); c.A > 0 {
		u.Shape(shape, box, c)
	}
}

// tail is the end of query that fits in w, after an ellipsis when cut, so the
// caret stays in view as the query grows. The answer is kept until the query
// changes.
func (kb *Keyboard) tail(u *ui.Context, query string, w float32) string {
	if query == kb.query && kb.shown != "" {
		return kb.shown
	}
	kb.query, kb.shown = query, query
	for s := query; s != "" && u.TextWidth(kb.shown, fieldText) > w; {
		_, n := utf8.DecodeRuneInString(s)
		s = s[n:]
		kb.shown = "…" + s
	}
	return kb.shown
}
