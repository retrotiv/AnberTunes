package text

import (
	"strings"
	"testing"

	"github.com/ujjwal313/boombrick/assets"
)

func engine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine(assets.Fonts())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestTabularFiguresShareWidth(t *testing.T) {
	e := engine(t)
	st := Style{Face: Figtree, Size: 28, Tabular: true}
	if a, b := e.Shape("1111", st).Width, e.Shape("8888", st).Width; a != b {
		t.Fatalf("tabular widths differ: 1111=%d 8888=%d", a, b)
	}
}

func TestFitAddsEllipsis(t *testing.T) {
	e := engine(t)
	long := "Paper Lanterns — The Hollow Pines — Extended Session"
	got := e.Fit(long, Body, 200)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("Fit = %q, want an ellipsis", got)
	}
	if w := e.Shape(got, Body).Width; w > 200 {
		t.Fatalf("fitted width %d > 200", w)
	}
	if got := e.Fit("Home", Body, 200); got != "Home" {
		t.Fatalf("short text changed to %q", got)
	}
}

func TestMetricsMatchShaping(t *testing.T) {
	e := engine(t)
	a, d := e.Metrics(Heading)
	l := e.Shape("Continue listening", Heading)
	if a != l.Ascent || d != l.Descent {
		t.Fatalf("Metrics = %d/%d, Shape = %d/%d", a, d, l.Ascent, l.Descent)
	}
	if a <= 0 || d <= 0 {
		t.Fatalf("implausible metrics %d/%d", a, d)
	}
}

func TestFractionalSize(t *testing.T) {
	e := engine(t)
	at := func(size float32) int { return e.Shape("Downloads", Style{Face: Figtree, Size: size}).Width }
	if w := at(27.5); w <= at(27) || w >= at(28) {
		t.Fatalf("width at 27.5 = %d, want between %d and %d", w, at(27), at(28))
	}
}

func TestRasterizeCoversText(t *testing.T) {
	e := engine(t)
	line := e.Shape("Boombrick", Title)
	img, pad := e.Rasterize(line, Title)
	if img.Bounds().Dy() != line.Height() {
		t.Fatalf("height = %d, want %d", img.Bounds().Dy(), line.Height())
	}
	right := -1
	for x := img.Bounds().Dx() - 1; x >= 0 && right < 0; x-- {
		for y := 0; y < img.Bounds().Dy(); y++ {
			if img.NRGBAAt(x, y).A > 0 {
				right = x
				break
			}
		}
	}
	ink := right - pad.X
	if ink < line.Width-int(Title.Size) || ink > line.Width+pad.X {
		t.Fatalf("ink ends at %d, advance is %d", ink, line.Width)
	}
}

// drawsAll reports whether every glyph of l is a real one, not the missing
// glyph.
func drawsAll(l Line) bool {
	for _, run := range l.runs {
		for _, g := range run.Glyphs {
			if g.GlyphID == 0 {
				return false
			}
		}
	}
	return true
}

func TestFallbackDrawsWhatTheFontsLack(t *testing.T) {
	e := engine(t)
	if l := e.Shape("Low Tide Radio · 3:42", Body); len(l.runs) != 1 || l.fallback || e.tried[notoRegular] {
		t.Fatalf("a Latin line: %d runs, fallback %v, Noto read %v", len(l.runs), l.fallback, e.tried[notoRegular])
	}
	for _, c := range []struct {
		s        string
		fallback bool
	}{
		{"Кино", true}, {"Група крові", true}, {"Σιωπή", true}, {"Mỹ Tâm", true}, {"Sơn Tùng M-TP", true}, {"Lǜ", true},
		{"Ştefan Bănică", false}, {"Björk · Sigur Rós", false}, // Figtree and Outfit have these
	} {
		for _, st := range []Style{Body, Title, {Face: FigtreeItalic, Size: 24}} {
			if l := e.Shape(c.s, st); !drawsAll(l) || l.fallback != c.fallback {
				t.Errorf("%q in face %d: every glyph drawn %v, fallback %v", c.s, st.Face, drawsAll(l), l.fallback)
			}
		}
	}
	if e.fallback[notoRegular] == nil || e.fallback[notoSemiBold] == nil {
		t.Fatal("a fallback face didn't load")
	}
}

// A letter and the marks after it are drawn by one font, so the marks sit on
// it; a line changes font only where its letters do.
func TestFallbackRunsFollowTheLetters(t *testing.T) {
	e := engine(t)
	if l := e.Shape("ộ", Body); len(l.runs) != 1 || !drawsAll(l) {
		t.Fatalf("a letter with two combining marks: %d runs, every glyph drawn %v", len(l.runs), drawsAll(l))
	}
	l := e.Shape("Hello Привет", Body)
	if len(l.runs) != 2 || l.runs[0].Face != e.faces[Figtree] || l.runs[1].Face != e.fallback[notoRegular] {
		t.Fatalf("%d runs; want Figtree, then Noto Sans", len(l.runs))
	}
	if n := len(l.runs[0].Glyphs); n != len("Hello ") {
		t.Fatalf("the first run has %d glyphs; want the space to stay with Hello", n)
	}
	// A word with one letter Figtree lacks is drawn whole by Noto Sans.
	l = e.Shape("Mỹ Tâm", Body)
	if len(l.runs) != 2 || l.runs[0].Face != e.fallback[notoRegular] || len(l.runs[0].Glyphs) != 3 ||
		l.runs[1].Face != e.faces[Figtree] {
		t.Fatalf("Mỹ Tâm: %d runs; want Mỹ and its space in Noto Sans, then Tâm in Figtree", len(l.runs))
	}
}

// The fallback font's tallest accents reach above the line box: a line it
// drew is rasterised with room for them.
func TestFallbackAccentsAreNotClipped(t *testing.T) {
	e := engine(t)
	for _, s := range []string{"Ấ", "Ǘ", "Ộ"} {
		line := e.Shape(s, Body)
		img, pad := e.Rasterize(line, Body)
		if pad.Y != Pad(Body) || img.Bounds().Dy() != line.Height()+2*pad.Y {
			t.Fatalf("%s: padding %v, height %d for a line %d high", s, pad, img.Bounds().Dy(), line.Height())
		}
		for x := range img.Bounds().Dx() {
			if img.NRGBAAt(x, 0).A > 0 || img.NRGBAAt(x, img.Bounds().Dy()-1).A > 0 {
				t.Fatalf("%s is cut off at the top or bottom", s)
			}
		}
	}
}
