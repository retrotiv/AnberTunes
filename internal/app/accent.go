package app

import (
	"image"
	"time"

	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/screens"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

// accentFade eases the accent from one album's colours to the next over
// anim.Accent.
type accentFade struct {
	from, to theme.Accent
	start    time.Time // zero once settled
}

func newAccentFade(a theme.Accent) accentFade { return accentFade{from: a, to: a} }

// target is the accent the fade is heading for.
func (f *accentFade) target() theme.Accent { return f.to }

// set starts easing to to at now. A change of mind part-way starts from
// whichever accent was nearer rather than from a blend, whose backgrounds
// would have to be rendered afresh.
func (f *accentFade) set(to theme.Accent, now time.Time) {
	if to == f.to {
		return
	}
	if f.progress(now) >= 0.5 {
		f.from = f.to
	}
	f.to, f.start = to, now
}

func (f *accentFade) progress(now time.Time) float64 {
	if f.start.IsZero() {
		return 1
	}
	return min(max(float64(now.Sub(f.start))/float64(anim.Accent), 0), 1)
}

// at is the accent to draw with at now, and the backgrounds' fade.
func (f *accentFade) at(now time.Time) (theme.Accent, ui.Fade) {
	p := f.progress(now)
	if p >= 1 {
		f.start = time.Time{}
		return f.to, ui.Fade{}
	}
	e := anim.EaseInOutCubic(p)
	return f.from.Lerp(f.to, e), ui.Fade{From: f.from, To: f.to, T: float32(e), On: true}
}

func (f *accentFade) animating(now time.Time) bool { return f.progress(now) < 1 }

// ground is a background rendered ahead, with the key the context keeps it
// under.
type ground struct {
	key string
	img *image.NRGBA
}

// renderGrounds renders the backgrounds that take the accent: the tabs' and
// Now Playing's, which Lyrics shows too until its backdrop is ready.
func renderGrounds(acc theme.Accent) []ground {
	return []ground{
		{widgets.GroundKey(widgets.TabGround, acc), widgets.GroundImage(widgets.TabGround, acc)},
		{widgets.GroundKey(widgets.NowPlayingGround, acc), widgets.GroundImage(widgets.NowPlayingGround, acc)},
	}
}

// grounds renders a new accent's backgrounds on a goroutine, so that the fade
// to it starts with them ready rather than rendering them on the UI thread in
// its first frame. One accent is rendered at a time: if another is asked for
// meanwhile, what was rendered is dropped and the latest rendered next. It is
// used from the UI thread.
type grounds struct {
	render  func(theme.Accent) []ground
	wake    func()
	results chan renderedGrounds
	running bool
	want    theme.Accent // the accent asked for last
	have    theme.Accent // the accent whose backgrounds were handed over last
}

type renderedGrounds struct {
	acc theme.Accent
	gs  []ground
}

// newGrounds returns grounds starting from have, whose backgrounds are drawn
// as they are first needed. wake is called when a render is done.
func newGrounds(have theme.Accent, render func(theme.Accent) []ground, wake func()) *grounds {
	return &grounds{render: render, wake: wake, results: make(chan renderedGrounds, 1), want: have, have: have}
}

// ask asks for acc's backgrounds, unless they were the last handed over.
func (g *grounds) ask(acc theme.Accent) {
	g.want = acc
	if acc != g.have && !g.running {
		g.start(acc)
	}
}

func (g *grounds) start(acc theme.Accent) {
	g.running = true
	go func() {
		g.results <- renderedGrounds{acc, g.render(acc)}
		g.wake()
	}()
}

// take returns the backgrounds of the accent asked for last, once they are
// rendered.
func (g *grounds) take() (theme.Accent, []ground, bool) {
	select {
	case r := <-g.results:
		g.running = false
		if r.acc == g.want {
			g.have = r.acc
			return r.acc, r.gs, true
		}
		if g.want != g.have {
			g.start(g.want)
		}
	default:
	}
	return theme.Accent{}, nil, false
}

// colours is what the accent needs from the art cache.
type colours interface {
	Colour(c media.Cover) (hue, chroma float64, colour, ok bool)
}

// accentFor is the accent for what st is playing: its album's colour, or
// gold when nothing is queued or the art is greyscale or missing. ok is false
// while the album's colour isn't known yet; asking starts finding out.
func accentFor(c colours, st player.State) (theme.Accent, bool) {
	if !st.Queued || c == nil {
		return theme.Fallback(), true
	}
	hue, chroma, colour, ok := c.Colour(screens.TrackArt(st.Track).Cover)
	switch {
	case !ok:
		return theme.Accent{}, false
	case !colour:
		return theme.Fallback(), true
	}
	return theme.Derive(hue, chroma), true
}
