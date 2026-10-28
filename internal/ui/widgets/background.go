package widgets

import (
	"fmt"
	"image"
	"image/color"

	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/gfx"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
)

// Background selects a screen's ground.
type Background uint8

const (
	// TabGround glows from the top-left corner.
	TabGround Background = iota
	// NowPlayingGround glows from behind the album art.
	NowPlayingGround
	// FlatGround is plain ground, used by Settings.
	FlatGround
	// StateGround glows from the top centre in the fallback colour, for
	// sign-in, offline and empty-library screens.
	StateGround
)

// DrawBackground fills the screen with kind, coloured by u.Accent, or
// cross-fading between the two accents of u.Fade. Each gradient is rendered
// once per accent and cached, unless it was rendered ahead and put in the
// context (see GroundImage); the state ground is one texture whatever the
// accent.
func DrawBackground(u *ui.Context, kind Background) {
	if kind == FlatGround {
		// Filled rather than cleared, so that it fades with the screen.
		u.Fill(gfx.Rect{W: theme.ScreenW, H: theme.ScreenH}, theme.Ground)
		return
	}
	if f := u.Fade; f.On && kind != StateGround {
		drawGround(u, kind, f.From, 1)
		drawGround(u, kind, f.To, f.T)
		return
	}
	drawGround(u, kind, u.Accent, 1)
}

// drawGround draws kind's gradient for acc at alpha a.
func drawGround(u *ui.Context, kind Background, acc theme.Accent, a float32) {
	u.Image(groundKey(kind, acc), func() *image.NRGBA { return GroundImage(kind, acc) },
		gfx.Rect{X: 0, Y: 0, W: theme.ScreenW, H: theme.ScreenH}, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(255*a + 0.5)})
}

// groundKeys holds the keys groundKey made last for each kind that takes the
// accent, two apiece for the two accents of a cross-fade. Only the UI thread
// uses it.
var groundKeys [StateGround][2]struct {
	acc theme.Accent
	key string
}

// groundKey is GroundKey, remembered so that a frame drawing the grounds of
// the last frame formats nothing: the accent seldom changes.
func groundKey(kind Background, acc theme.Accent) string {
	if kind >= StateGround {
		return GroundKey(kind, acc)
	}
	m := &groundKeys[kind]
	for i := range m {
		if m[i].key != "" && m[i].acc == acc {
			return m[i].key
		}
	}
	m[1] = m[0]
	m[0].acc, m[0].key = acc, GroundKey(kind, acc)
	return m[0].key
}

// GroundKey is the key DrawBackground keeps kind's gradient for acc under in
// the context's images.
func GroundKey(kind Background, acc theme.Accent) string {
	if kind == StateGround {
		return ui.GroundPrefix + "state" // drawn in the fallback colour whatever the accent
	}
	return fmt.Sprintf("%s%d:%v:%.1f", ui.GroundPrefix, kind, acc.Glow, acc.Hue)
}

// groundDown is how much smaller than the screen a gradient is rendered. The
// gradients are smooth enough that drawn scaled up, with linear filtering,
// they look the same, at a sixteenth of the memory and time.
const groundDown = 4

// GroundImage renders kind's gradient for acc at a quarter of the device's
// width and height, for DrawBackground to scale up; FlatGround has none. A
// gradient still takes milliseconds to render on the device, long for the UI
// thread when the album colour changes, so it may be rendered on another
// goroutine and handed to the context's PutImage under GroundKey.
func GroundImage(kind Background, acc theme.Accent) *image.NRGBA {
	const w, h = theme.DeviceW / groundDown, theme.DeviceH / groundDown
	ground := theme.OklabOf(theme.Ground)
	switch kind {
	case NowPlayingGround:
		return raster.RadialGradient(w, h, 0.22*w, 0.45*h, 0.9*w, 1.2*h, []raster.Stop{
			{Pos: 0, Color: theme.OKLCH{L: 0.36, C: 0.09, H: acc.Hue}.Oklab()},
			{Pos: 0.45, Color: theme.OKLCH{L: 0.24, C: 0.05, H: acc.Hue}.Oklab()},
			{Pos: 0.85, Color: ground},
		})
	case StateGround:
		return raster.RadialGradient(w, h, 0.5*w, 0, 1.3*w, h, []raster.Stop{
			{Pos: 0, Color: theme.OklabOf(theme.FallbackGlow)},
			{Pos: 0.62, Color: ground},
		})
	case TabGround:
		return raster.RadialGradient(w, h, 0, 0, 1.3*w, h, []raster.Stop{
			{Pos: 0, Color: theme.OklabOf(acc.Glow)},
			{Pos: 0.62, Color: ground},
		})
	}
	return nil
}
