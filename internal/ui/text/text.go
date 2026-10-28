// Package text shapes and rasterises strings with Boombrick's bundled fonts.
package text

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"io/fs"
	"math"
	"strings"
	"unicode"

	"github.com/go-text/render"
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/ujjwal313/boombrick/internal/ui/raster"
)

// Face selects one of the bundled font files: Outfit for headings, Figtree
// for everything else.
type Face uint8

const (
	Outfit Face = iota
	OutfitSemiBold
	Figtree
	FigtreeSemiBold
	FigtreeItalic
	faceCount
)

var files = [faceCount]string{
	Outfit:          "Outfit-Regular.ttf",
	OutfitSemiBold:  "Outfit-SemiBold.ttf",
	Figtree:         "Figtree-Regular.ttf",
	FigtreeSemiBold: "Figtree-SemiBold.ttf",
	FigtreeItalic:   "Figtree-Italic.ttf",
}

// Outfit and Figtree have Western European Latin only. Noto Sans draws what
// they lack: Cyrillic, Greek, Vietnamese and the rest of European Latin, in
// the weight nearest the face's, upright for the italic.
const (
	notoRegular = iota
	notoSemiBold
	fallbackCount
)

var fallbackFiles = [fallbackCount]string{
	notoRegular:  "NotoSans-Regular.ttf",
	notoSemiBold: "NotoSans-SemiBold.ttf",
}

var fallbackOf = [faceCount]int{
	Outfit:          notoRegular,
	OutfitSemiBold:  notoSemiBold,
	Figtree:         notoRegular,
	FigtreeSemiBold: notoSemiBold,
	FigtreeItalic:   notoRegular,
}

// Style is a face at a pixel size, which may be fractional. Tabular turns on
// the font's tabular figures.
type Style struct {
	Face    Face
	Size    float32
	Tabular bool
}

// Tnum returns st with tabular figures.
func (st Style) Tnum() Style {
	st.Tabular = true
	return st
}

// Named styles. Headings are semibold; display sizes and big numbers are
// regular.
var (
	Display    = Style{Face: Outfit, Size: 56}
	Title      = Style{Face: OutfitSemiBold, Size: 44}
	Heading    = Style{Face: OutfitSemiBold, Size: 34}
	Tab        = Style{Face: OutfitSemiBold, Size: 30}
	Body       = Style{Face: Figtree, Size: 28}
	Meta       = Style{Face: Figtree, Size: 22}
	Chrome     = Style{Face: Figtree, Size: 22}
	ChromeTnum = Style{Face: Figtree, Size: 22, Tabular: true}
	Badge      = Style{Face: Figtree, Size: 20}
)

const ellipsis = "…"

var (
	tnum    = []shaping.FontFeature{{Tag: opentype.MustNewTag("tnum"), Value: 1}}
	english = language.NewLanguage("en")
)

// Engine owns the parsed fonts. It is not safe for concurrent use; the UI
// thread owns it.
type Engine struct {
	fonts    fs.FS
	faces    [faceCount]*font.Face
	fallback [fallbackCount]*font.Face // parsed the first time a line needs one
	tried    [fallbackCount]bool
	shaper   shaping.HarfbuzzShaper
	bounds   map[Style]shaping.Bounds
	spans    []span
}

// NewEngine loads the bundled fonts from fonts.
func NewEngine(fonts fs.FS) (*Engine, error) {
	e := &Engine{fonts: fonts, bounds: map[Style]shaping.Bounds{}}
	for i, name := range files {
		face, err := parse(fonts, name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		e.faces[i] = face
	}
	return e, nil
}

// fallbackFor is the face that draws what f lacks, or nil if its file won't
// parse. A library in Western European languages never loads one.
func (e *Engine) fallbackFor(f Face) *font.Face {
	i := fallbackOf[f]
	if !e.tried[i] {
		e.tried[i] = true
		e.fallback[i], _ = parse(e.fonts, fallbackFiles[i]) // nil: its letters draw as missing
	}
	return e.fallback[i]
}

// parse parses the font file name in fonts. An embedded file is read in
// place, sparing a copy of the whole file on the heap; the parsed font keeps
// its own copies of the tables it needs.
func parse(fonts fs.FS, name string) (*font.Face, error) {
	f, err := fonts.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if r, ok := f.(font.Resource); ok {
		return font.ParseTTF(r)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return font.ParseTTF(bytes.NewReader(data))
}

// Line is a shaped line of text. Its line box is its style's, whichever
// fonts drew it.
type Line struct {
	runs     []shaping.Output // one for each font, in order
	fallback bool             // some of it is drawn by the fallback font
	Width    int              // advance in pixels
	Ascent   int
	Descent  int
}

// Height is the line box: ascent plus descent.
func (l Line) Height() int { return l.Ascent + l.Descent }

// Shape lays out s on one line.
func (e *Engine) Shape(s string, st Style) Line {
	runes := []rune(s)
	size := fixed.Int26_6(math.Round(float64(st.Size) * 64))
	in := shaping.Input{
		Text:      runes,
		Direction: di.DirectionLTR,
		// The shaper rounds its size up to a whole pixel; the output is
		// scaled back to size below.
		Size:     fixed.I(size.Ceil()),
		Script:   language.Latin,
		Language: english,
	}
	if st.Tabular {
		in.FontFeatures = tnum
	}
	b := e.lineBounds(st)
	l := Line{Ascent: b.Ascent.Ceil(), Descent: (-b.Descent).Ceil()}
	var advance fixed.Int26_6
	for _, sp := range e.split(runes, st.Face) {
		in.RunStart, in.RunEnd, in.Face = sp.start, sp.end, sp.face
		out := e.shaper.Shape(in)
		if in.Size != size {
			rescale(&out, float64(size)/float64(in.Size))
			out.Size = size
		}
		l.runs = append(l.runs, out)
		l.fallback = l.fallback || sp.face != e.faces[st.Face]
		advance += out.Advance
	}
	l.Width = advance.Ceil()
	return l
}

// span is a run of runes drawn by one face.
type span struct {
	start, end int
	face       *font.Face
}

// split divides runes into runs by the face that draws them: f's own, or its
// fallback for a word with a letter f lacks, so that a word is drawn in one
// design. Spaces and other invisible characters stay in the run they are in.
// A word neither face draws whole is split letter by letter, each letter
// with the marks that follow it so an accent is placed by the font that drew
// its letter; what neither face has is left to f, which draws it as missing.
// The spans are e's until the next call.
func (e *Engine) split(runes []rune, f Face) []span {
	own := e.faces[f]
	e.spans = e.spans[:0]
	add := func(start, end int, face *font.Face) {
		if n := len(e.spans); n > 0 && e.spans[n-1].face == face {
			e.spans[n-1].end = end
			return
		}
		e.spans = append(e.spans, span{start, end, face})
	}
	if covers(own, runes) {
		add(0, len(runes), own)
		return e.spans
	}
	fb := e.fallbackFor(f)
	for i := 0; i < len(runes); {
		if invisible(runes[i]) {
			face := own
			if n := len(e.spans); n > 0 {
				face = e.spans[n-1].face
			}
			add(i, i+1, face)
			i++
			continue
		}
		j := i + 1
		for j < len(runes) && !invisible(runes[j]) {
			j++
		}
		switch word := runes[i:j]; {
		case covers(own, word):
			add(i, j, own)
		case fb != nil && covers(fb, word):
			add(i, j, fb)
		default:
			for k := i; k < j; {
				m := k + 1
				for m < j && joins(runes[m]) {
					m++
				}
				face := own
				if cluster := runes[k:m]; !covers(own, cluster) && fb != nil && covers(fb, cluster) {
					face = fb
				}
				add(k, m, face)
				k = m
			}
		}
		i = j
	}
	return e.spans
}

// covers reports whether face has a glyph for each of runes that needs one.
func covers(face *font.Face, runes []rune) bool {
	for _, r := range runes {
		if _, ok := face.NominalGlyph(r); !ok && !invisible(r) {
			return false
		}
	}
	return true
}

// joins reports whether r belongs with the character before it: a combining
// mark, a joiner or a variation selector.
func joins(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) || r == '‍' || r >= '︀' && r <= '️'
}

// invisible reports whether r draws nothing: a space or a format character.
func invisible(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) }

// lineBounds is the line box of st in its own face.
func (e *Engine) lineBounds(st Style) shaping.Bounds {
	st.Tabular = false
	if b, ok := e.bounds[st]; ok {
		return b
	}
	size := fixed.Int26_6(math.Round(float64(st.Size) * 64))
	space := []rune{' '}
	in := shaping.Input{Text: space, RunEnd: 1, Direction: di.DirectionLTR, Face: e.faces[st.Face],
		Size: fixed.I(size.Ceil()), Script: language.Latin, Language: english}
	out := e.shaper.Shape(in)
	if in.Size != size {
		rescale(&out, float64(size)/float64(in.Size))
	}
	e.bounds[st] = out.LineBounds
	return out.LineBounds
}

// rescale multiplies every metric of out by f.
func rescale(out *shaping.Output, f float64) {
	sc := func(v fixed.Int26_6) fixed.Int26_6 { return fixed.Int26_6(math.Round(float64(v) * f)) }
	for i := range out.Glyphs {
		g := &out.Glyphs[i]
		g.Width, g.Height = sc(g.Width), sc(g.Height)
		g.XBearing, g.YBearing = sc(g.XBearing), sc(g.YBearing)
		g.Advance = sc(g.Advance)
		g.XOffset, g.YOffset = sc(g.XOffset), sc(g.YOffset)
	}
	out.LineBounds = shaping.Bounds{Ascent: sc(out.LineBounds.Ascent), Descent: sc(out.LineBounds.Descent), Gap: sc(out.LineBounds.Gap)}
	out.RecalculateAll()
}

// Metrics returns the ascent and descent of st's line box.
func (e *Engine) Metrics(st Style) (ascent, descent int) {
	b := e.lineBounds(st)
	return b.Ascent.Ceil(), (-b.Descent).Ceil()
}

// Fit returns s if it fits in maxWidth, otherwise the longest prefix that fits
// with an ellipsis appended.
func (e *Engine) Fit(s string, st Style, maxWidth int) string {
	if e.Shape(s, st).Width <= maxWidth {
		return s
	}
	runes := []rune(s)
	cut := func(n int) string { return strings.TrimRight(string(runes[:n]), " ") + ellipsis }
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if e.Shape(cut(mid), st).Width <= maxWidth {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return cut(lo)
}

// Pad is the padding Rasterize adds on each side so italic overhangs, and the
// tallest accents of the fallback font, aren't clipped.
func Pad(st Style) int { return int(st.Size)/8 + 1 }

// Rasterize draws l as a white mask Pad(st) px wider than its advance on each
// side, and as much taller at the top and bottom when the fallback font drew
// some of it. It returns the mask and that padding; draw the mask pad.X px
// to the left of the text position and pad.Y px above it.
func (e *Engine) Rasterize(l Line, st Style) (*image.NRGBA, image.Point) {
	pad := image.Point{X: Pad(st)}
	if l.fallback {
		pad.Y = pad.X
	}
	h := max(l.Height(), 1)
	dst := image.NewRGBA(image.Rect(0, 0, l.Width+2*pad.X, h+2*pad.Y))
	r := render.Renderer{FontSize: st.Size, PixScale: 1, Color: color.White}
	var x fixed.Int26_6
	for _, run := range l.runs {
		r.DrawShapedRunAt(run, dst, pad.X+x.Round(), pad.Y+l.Ascent)
		x += run.Advance
	}
	return raster.WhiteMask(dst), pad
}
