// Package overlaycard draws AnberTunes' now-playing player to an image, for the
// in-game overlay on the heavy emulators to texture on screen. It is pure Go
// (no SDL, no screen), so it runs in the background player and in a preview
// tool on the desktop. The expanded view mirrors the app's Now Playing screen.
package overlaycard

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"os"
	"time"

	"github.com/ujjwal313/boombrick/assets"
	"github.com/ujjwal313/boombrick/internal/art"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui/icons"
	"github.com/ujjwal313/boombrick/internal/ui/text"
)

// Now is what the player needs to draw: the current track, its progress, and
// which transport control is focused.
type Now struct {
	Title, Artist, Album, Format string
	Kbps, Rate                   int
	Elapsed, Duration            time.Duration
	Playing, Queued              bool
	CoverPath                    string // audio file; the cover is read from it or its folder
	Focus                        int    // focused control: 0 shuffle, 1 prev, 2 play, 3 next, 4 repeat
	Shuffle                      bool
	Repeat                       int // 0 off, 1 all, 2 one
}

// Mode is the view: a compact bar or the full player.
type Mode int

const (
	Reduced Mode = iota
	Expanded
)

// Renderer holds the fonts and the caches between frames.
type Renderer struct {
	eng      *text.Engine
	icons    map[string]*image.NRGBA
	coverKey string
	coverImg *image.NRGBA
	forced   image.Image // a cover set for previews, squared on demand
}

// New builds a renderer with AnberTunes' bundled fonts.
func New() (*Renderer, error) {
	eng, err := text.NewEngine(assets.Fonts())
	if err != nil {
		return nil, err
	}
	return &Renderer{eng: eng, icons: map[string]*image.NRGBA{}}, nil
}

// Render draws the now-playing card for the mode.
func (r *Renderer) Render(n Now, m Mode) *image.NRGBA {
	if m == Expanded {
		return r.expanded(n)
	}
	return r.reduced(n)
}

// ---- shared helpers ---------------------------------------------------------

func (r *Renderer) lineH(st text.Style) int { return r.eng.Shape("Ag", st).Height() }

func (r *Renderer) textW(st text.Style, s string) int { return r.eng.Shape(s, st).Width }

// drawText composites s (fitted to maxW) in st, tinted col, top-left at (x,y).
func (r *Renderer) drawText(dst *image.NRGBA, st text.Style, s string, x, y int, col color.NRGBA, maxW int) {
	if s == "" {
		return
	}
	if maxW > 0 {
		s = r.eng.Fit(s, st, maxW)
	}
	mask, _ := r.eng.Rasterize(r.eng.Shape(s, st), st)
	compositeMask(dst, mask, x, y, col)
}

// drawIcon composites an AnberTunes icon (the same SVG the app uses), tinted
// col, with its px-square top-left at (x,y).
func (r *Renderer) drawIcon(dst *image.NRGBA, name string, px int, stroke float64, filled bool, x, y int, col color.NRGBA) {
	key := fmt.Sprintf("%s|%d|%g|%t", name, px, stroke, filled)
	m := r.icons[key]
	if m == nil {
		im, err := icons.Rasterize(name, px, stroke, filled)
		if err != nil {
			return
		}
		m = im
		r.icons[key] = m
	}
	compositeMask(dst, m, x, y, col)
}

// accentOf is the screen's accent, taken from the cover like the app, or the
// brand fallback when there is none.
func (r *Renderer) accentOf(cover *image.NRGBA) theme.Accent {
	if cover != nil {
		if hue, chroma, ok := art.Colour(cover); ok {
			return theme.Derive(hue, chroma)
		}
	}
	return theme.Fallback()
}

// cover reads and squares the track's cover (embedded, or a folder image),
// cached by path. No network.
func (r *Renderer) cover(path string, px int) *image.NRGBA {
	if r.forced != nil {
		return art.Square(r.forced, px)
	}
	key := fmt.Sprintf("%s|%d", path, px)
	if key == r.coverKey {
		return r.coverImg
	}
	r.coverKey, r.coverImg = key, nil
	if path == "" {
		return nil
	}
	if data, ok := art.Embedded(path); ok {
		if im, err := art.Decode(data); err == nil {
			r.coverImg = art.Square(im, px)
		}
	}
	if r.coverImg == nil {
		if p, ok := art.FolderImage(filepath.Dir(path)); ok {
			if data, err := os.ReadFile(p); err == nil {
				if im, err := art.Decode(data); err == nil {
					r.coverImg = art.Square(im, px)
				}
			}
		}
	}
	return r.coverImg
}

// SetCover forces a cover image, squared on demand (for previews).
func (r *Renderer) SetCover(img image.Image) { r.forced = img }

func clockText(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ---- the expanded view: a faithful Now Playing ------------------------------

const (
	 expW    = 800
	expH     = 600
	expSide  = 44
	expArt   = 300
	expColGap = 44
	expGroup = 26
	expCtlRow = 108
	expKnob  = 18
)

var (
	titleStyle  = text.Style{Face: text.OutfitSemiBold, Size: 52}
	artistStyle = text.Style{Face: text.Figtree, Size: 30}
	albumStyle  = text.Style{Face: text.FigtreeItalic, Size: 24}
	timeStyle   = text.Style{Face: text.Figtree, Size: 22, Tabular: true}
	fmtStyle    = text.Style{Face: text.Figtree, Size: 20}
)

// controlOffset mirrors the app: shuffle, prev, play, next, repeat.
var controlOffset = [5]int{-170, -100, 0, 100, 170}
var controlIcons = [5]string{"shuffle", "prev", "play", "next", "repeat"}

// goldTint is the amber behind the selected browse row (brand gold at ~16%).
var goldTint = color.NRGBA{R: theme.Gold.R, G: theme.Gold.G, B: theme.Gold.B, A: 42}

func (r *Renderer) expanded(n Now) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, expW, expH))
	cover := r.cover(n.CoverPath, expArt)
	ac := r.accentOf(cover)

	// Background: a glow from behind the album art, over the dark ground.
	artCx, artCy := expSide+expArt/2, (expH-expArt)/2+expArt/2
	glowBackground(img, ac.Glow, artCx, artCy)

	// Album art, rounded, with a soft shadow.
	ax, ay := expSide, (expH-expArt)/2
	fillRoundRect(img, image.Rect(ax+6, ay+16, ax+expArt+6, ay+expArt+16), 20, color.NRGBA{A: 90})
	if cover != nil {
		compositeImage(img, cover, image.Pt(ax, ay), 16)
	} else {
		fillRoundRect(img, image.Rect(ax, ay, ax+expArt, ay+expArt), 16, theme.Raised)
		r.drawIcon(img, "music", 96, 0, true, artCx-48, artCy-48, theme.Ink4)
	}

	// Right column, vertically centred like the app.
	x := expSide + expArt + expColGap
	w := expW - expSide - x
	titleH, artistH, albumH, timeH := r.lineH(titleStyle), r.lineH(artistStyle), r.lineH(albumStyle), r.lineH(timeStyle)
	h := 30 + expGroup + titleH + artistH + albumH + expGroup + expKnob + 8 + timeH + expGroup + expCtlRow
	y := (expH - h) / 2

	r.drawText(img, fmtStyle, formatLine(n), x, y, theme.Ink3, w)
	y += 30 + expGroup
	r.drawText(img, titleStyle, orDash(n.Title), x, y, theme.Paper, w)
	y += titleH
	r.drawText(img, artistStyle, n.Artist, x, y, theme.Paper, w)
	y += artistH
	r.drawText(img, albumStyle, n.Album, x, y, theme.Ink2, w)
	y += albumH + expGroup

	r.drawProgress(img, n, x, y, w, ac)
	y += expKnob + 8 + timeH + expGroup

	r.drawControls(img, n, x, y, w, ac)

	// Hint bar: how to work the player, including the way to the library.
	r.drawHints(img, []hint{{"A", "Tocar/Pausar"}, {"Y", "Biblioteca"}, {"B", "Recolher"}})
	return img
}

func (r *Renderer) drawProgress(dst *image.NRGBA, n Now, x, y, w int, ac theme.Accent) {
	var f float64
	if n.Duration > 0 {
		f = float64(n.Elapsed) / float64(n.Duration)
		f = math.Min(math.Max(f, 0), 1)
	}
	barY := y + (expKnob-6)/2
	fillRoundRect(dst, image.Rect(x, barY, x+w, barY+6), 3, theme.PaperA(0.18))
	if f > 0 {
		fillRoundRect(dst, image.Rect(x, barY, x+int(float64(w)*f), barY+6), 3, ac.Color)
	}
	kx := x + int(float64(w)*f) - expKnob/2
	fillRoundRect(dst, image.Rect(kx, y, kx+expKnob, y+expKnob), expKnob/2, theme.Paper)
	ty := y + expKnob + 8
	r.drawText(dst, timeStyle, clockText(n.Elapsed), x, ty, theme.Ink2, w)
	rt := clockText(n.Duration)
	r.drawText(dst, timeStyle, rt, x+w-r.textW(timeStyle, rt), ty, theme.Ink2, w)
}

func (r *Renderer) drawControls(dst *image.NRGBA, n Now, x, y, w int, ac theme.Accent) {
	cy := y + expCtlRow/2
	for i := range controlIcons {
		cx := x + w/2 + controlOffset[i]
		if i == n.Focus { // a focus ring around the selected control
			fr := 34
			if i == 2 {
				fr = 58
			}
			drawRing(dst, cx, cy, fr, 3, ac.Color)
		}
		switch i {
		case 2: // play / pause, with the accent ring
			drawRing(dst, cx, cy, expCtlRow/2-2, 2, ac.Color)
			name := "play"
			if n.Playing {
				name = "pause"
			}
			r.drawIcon(dst, name, 44, 2, true, cx-22, cy-22, theme.Paper)
		case 1, 3: // prev, next
			r.drawIcon(dst, controlIcons[i], 44, 2, true, cx-22, cy-22, theme.Paper)
		default: // shuffle, repeat — amber when on
			on := (i == 0 && n.Shuffle) || (i == 4 && n.Repeat != 0)
			col := theme.Ink3
			if on {
				col = ac.Color
			}
			r.drawIcon(dst, controlIcons[i], 34, 2, false, cx-17, cy-17, col)
			if i == 4 && n.Repeat == 2 { // repeat-one marker
				r.drawText(dst, text.Style{Face: text.FigtreeSemiBold, Size: 15}, "1", cx+12, cy-26, col, 40)
			}
		}
	}
}

func formatLine(n Now) string {
	switch {
	case n.Kbps > 0:
		return fmt.Sprintf("%d kbps", n.Kbps)
	case n.Rate == 0:
		return n.Format
	case n.Format == "":
		return fmt.Sprintf("%g kHz", float64(n.Rate)/1000)
	default:
		return fmt.Sprintf("%s · %g kHz", n.Format, float64(n.Rate)/1000)
	}
}

// ---- the browse view: pick what to play from the library --------------------

// BrowseThumb is the size a browse row's cover must already be squared to.
const BrowseThumb = 58

// BrowseRow is one line: a cover (already squared to BrowseThumb, or nil), a
// title and a subtitle (the artist, or a track count).
type BrowseRow struct {
	Title, Subtitle string
	Cover           *image.NRGBA
}

// BrowseModel is the library list to draw: a heading, the filter tabs, and the
// rows with one selected.
type BrowseModel struct {
	Heading  string
	Tabs     []string
	Tab      int
	Rows     []BrowseRow
	Selected int
}

const (
	browseTop   = 150
	browseRowH  = 78
	browseRows  = 5
	browseThumb = BrowseThumb
)

// RenderBrowse draws the library browser: a heading, tabs, and a scrolling list
// with the selected row lifted in amber, over the dark ground.
func (r *Renderer) RenderBrowse(m BrowseModel) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, expW, expH))
	fillRoundRect(img, img.Bounds(), 0, theme.Ground)

	// Heading.
	r.drawText(img, text.Style{Face: text.OutfitSemiBold, Size: 34}, orDash(m.Heading), expSide, 36, theme.Paper, expW-2*expSide)

	// Tabs as pills.
	tx := expSide
	for i, name := range m.Tabs {
		ts := text.Style{Face: text.FigtreeSemiBold, Size: 20}
		tw := r.textW(ts, name)
		pw := tw + 32
		on := i == m.Tab
		bg := theme.Raised
		fg := theme.Ink3
		if on {
			bg, fg = theme.Gold, theme.Ground
		}
		fillRoundRect(img, image.Rect(tx, 90, tx+pw, 90+40), 20, bg)
		r.drawText(img, ts, name, tx+16, 98, fg, pw)
		tx += pw + 12
	}

	// List, scrolled so the selected row is in view.
	first := m.Selected - 2
	if max := len(m.Rows) - browseRows; first > max {
		first = max
	}
	if first < 0 {
		first = 0
	}
	for vi := 0; vi < browseRows && first+vi < len(m.Rows); vi++ {
		i := first + vi
		row := m.Rows[i]
		ry := browseTop + vi*browseRowH
		if i == m.Selected {
			fillRoundRect(img, image.Rect(expSide-10, ry-6, expW-expSide+10, ry+browseThumb+6), 14, goldTint)
		}
		if row.Cover != nil {
			compositeImage(img, row.Cover, image.Pt(expSide, ry), 8)
		} else {
			fillRoundRect(img, image.Rect(expSide, ry, expSide+browseThumb, ry+browseThumb), 8, theme.LiftedRow)
			r.drawIcon(img, "music", 28, 0, true, expSide+browseThumb/2-14, ry+browseThumb/2-14, theme.Ink4)
		}
		tx := expSide + browseThumb + 18
		tw := expW - expSide - tx
		r.drawText(img, text.Style{Face: text.FigtreeSemiBold, Size: 25}, orDash(row.Title), tx, ry+4, theme.Paper, tw)
		r.drawText(img, text.Style{Face: text.Figtree, Size: 18}, row.Subtitle, tx, ry+34, theme.Ink3, tw)
	}

	// Hint bar.
	r.drawHints(img, []hint{{"A", "Tocar"}, {"B", "Voltar"}, {"L2/R2", "Abas"}})
	return img
}

type hint struct{ key, label string }

func (r *Renderer) drawHints(dst *image.NRGBA, hints []hint) {
	y := expH - 44
	x := expSide
	for _, h := range hints {
		ks := text.Style{Face: text.FigtreeSemiBold, Size: 16}
		kw := r.textW(ks, h.key)
		fillRoundRect(dst, image.Rect(x, y, x+kw+18, y+28), 14, theme.Raised)
		r.drawText(dst, ks, h.key, x+9, y+5, theme.Ink2, kw+18)
		x += kw + 18 + 8
		ls := text.Style{Face: text.Figtree, Size: 17}
		r.drawText(dst, ls, h.label, x, y+5, theme.Ink3, 300)
		x += r.textW(ls, h.label) + 26
	}
}

// ---- the reduced view: a compact bar -----------------------------------------

const (
	redW   = 560
	redH   = 116
	redPad = 16
	redCov = redH - 2*redPad
)

func (r *Renderer) reduced(n Now) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, redW, redH))
	cover := r.cover(n.CoverPath, redCov)
	ac := r.accentOf(cover)
	panel := theme.Raised
	panel.A = 240
	fillRoundRect(img, image.Rect(0, 0, redW, redH), 18, panel)

	cov := image.Rect(redPad, redPad, redPad+redCov, redPad+redCov)
	if cover != nil {
		compositeImage(img, cover, cov.Min, 10)
	} else {
		fillRoundRect(img, cov, 10, theme.LiftedRow)
		r.drawIcon(img, "music", 36, 0, true, cov.Min.X+redCov/2-18, redPad+redCov/2-18, theme.Ink4)
	}

	x := redPad + redCov + 16
	w := redW - x - redPad
	r.drawText(img, text.Style{Face: text.FigtreeSemiBold, Size: 24}, orDash(n.Title), x, redPad+2, theme.Paper, w)
	r.drawText(img, text.Style{Face: text.Figtree, Size: 17}, n.Artist, x, redPad+36, theme.Ink3, w)

	var f float64
	if n.Duration > 0 {
		f = math.Min(math.Max(float64(n.Elapsed)/float64(n.Duration), 0), 1)
	}
	by1 := redH - redPad
	by0 := by1 - 5
	fillRoundRect(img, image.Rect(x, by0, redW-redPad, by1), 2, theme.PaperA(0.18))
	if f > 0 {
		fillRoundRect(img, image.Rect(x, by0, x+int(float64(redW-redPad-x)*f), by1), 2, ac.Color)
	}
	// a small play/pause mark at the right
	mk := "play"
	if n.Playing {
		mk = "pause"
	}
	r.drawIcon(img, mk, 22, 2, true, redW-redPad-22, redPad+4, ac.Color)
	return img
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ---- pixel helpers ----------------------------------------------------------

// glowBackground fills the canvas with the dark ground and a radial glow of col
// centred on (cx,cy), like the Now Playing background.
func glowBackground(dst *image.NRGBA, glow color.NRGBA, cx, cy int) {
	b := dst.Bounds()
	maxd := 460.0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			d := math.Hypot(float64(x-cx), float64(y-cy))
			t := 1 - d/maxd
			if t < 0 {
				t = 0
			}
			t = t * t * 0.9
			o := dst.PixOffset(x, y)
			dst.Pix[o] = uint8(float64(theme.Ground.R)*(1-t) + float64(glow.R)*t)
			dst.Pix[o+1] = uint8(float64(theme.Ground.G)*(1-t) + float64(glow.G)*t)
			dst.Pix[o+2] = uint8(float64(theme.Ground.B)*(1-t) + float64(glow.B)*t)
			dst.Pix[o+3] = 255
		}
	}
}

// compositeMask draws a white-with-alpha mask tinted col at (x,y).
func compositeMask(dst, mask *image.NRGBA, x, y int, col color.NRGBA) {
	b := mask.Bounds()
	for my := b.Min.Y; my < b.Max.Y; my++ {
		for mx := b.Min.X; mx < b.Max.X; mx++ {
			a := mask.Pix[mask.PixOffset(mx, my)+3]
			if a == 0 {
				continue
			}
			px, py := x+(mx-b.Min.X), y+(my-b.Min.Y)
			if px < 0 || py < 0 || px >= dst.Rect.Dx() || py >= dst.Rect.Dy() {
				continue
			}
			src := col
			src.A = uint8(uint16(col.A) * uint16(a) / 255)
			overPixel(dst, px, py, src)
		}
	}
}

func compositeImage(dst, src *image.NRGBA, p image.Point, radius int) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	rect := image.Rect(p.X, p.Y, p.X+w, p.Y+h)
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			if !insideRound(p.X+xx, p.Y+yy, rect, radius) {
				continue
			}
			o := src.PixOffset(b.Min.X+xx, b.Min.Y+yy)
			overPixel(dst, p.X+xx, p.Y+yy, color.NRGBA{src.Pix[o], src.Pix[o+1], src.Pix[o+2], src.Pix[o+3]})
		}
	}
}

func drawRing(dst *image.NRGBA, cx, cy, radius, width int, col color.NRGBA) {
	r2o, r2i := float64(radius*radius), float64((radius-width)*(radius-width))
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			d2 := float64((x-cx)*(x-cx) + (y-cy)*(y-cy))
			if d2 <= r2o && d2 >= r2i {
				if x >= 0 && y >= 0 && x < dst.Rect.Dx() && y < dst.Rect.Dy() {
					overPixel(dst, x, y, col)
				}
			}
		}
	}
}

func fillRoundRect(dst *image.NRGBA, r image.Rectangle, radius int, col color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if insideRound(x, y, r, radius) {
				overPixel(dst, x, y, col)
			}
		}
	}
}

func insideRound(x, y int, r image.Rectangle, radius int) bool {
	if radius <= 0 {
		return true
	}
	left, right := r.Min.X+radius, r.Max.X-1-radius
	top, bottom := r.Min.Y+radius, r.Max.Y-1-radius
	var dx, dy int
	switch {
	case x < left:
		dx = left - x
	case x > right:
		dx = x - right
	}
	switch {
	case y < top:
		dy = top - y
	case y > bottom:
		dy = y - bottom
	}
	if dx == 0 || dy == 0 {
		return true
	}
	return dx*dx+dy*dy <= radius*radius
}

func overPixel(dst *image.NRGBA, x, y int, src color.NRGBA) {
	o := dst.PixOffset(x, y)
	sa := int(src.A)
	if sa == 0 {
		return
	}
	if sa == 255 {
		dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = src.R, src.G, src.B, 255
		return
	}
	da := int(dst.Pix[o+3])
	oa := sa + da*(255-sa)/255
	if oa == 0 {
		dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = 0, 0, 0, 0
		return
	}
	blend := func(s, d uint8) uint8 { return uint8((int(s)*sa + int(d)*da*(255-sa)/255) / oa) }
	dst.Pix[o] = blend(src.R, dst.Pix[o])
	dst.Pix[o+1] = blend(src.G, dst.Pix[o+1])
	dst.Pix[o+2] = blend(src.B, dst.Pix[o+2])
	dst.Pix[o+3] = uint8(oa)
}

// Dimensions reports a mode's pixel size, for the preview and the overlay.
func Dimensions(m Mode) (int, int) {
	if m == Expanded {
		return expW, expH
	}
	return redW, redH
}
