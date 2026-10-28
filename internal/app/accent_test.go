package app

import (
	"image"
	"image/color"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui/anim"
	"github.com/ujjwal313/boombrick/internal/ui/uitest"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

func TestAccentFade(t *testing.T) {
	t0 := time.Unix(100, 0)
	gold, red := theme.Fallback(), theme.Derive(25, 0.12)
	f := newAccentFade(gold)
	if acc, fade := f.at(t0); acc != gold || fade.On || f.animating(t0) {
		t.Fatalf("settled: %+v, %+v", acc, fade)
	}
	f.set(red, t0)
	if _, fade := f.at(t0); !fade.On || fade.T != 0 || fade.From != gold || fade.To != red {
		t.Fatalf("at the start: %+v", fade)
	}
	acc, fade := f.at(t0.Add(anim.Accent / 2))
	if fade.T != 0.5 || acc != gold.Lerp(red, 0.5) || !f.animating(t0.Add(anim.Accent/2)) {
		t.Fatalf("halfway: %+v, %+v", acc, fade)
	}
	if acc, fade := f.at(t0.Add(anim.Accent)); acc != red || fade.On || f.animating(t0.Add(anim.Accent)) {
		t.Fatalf("at the end: %+v, %+v", acc, fade)
	}
}

func TestAccentFadeChangeOfMind(t *testing.T) {
	t0 := time.Unix(100, 0)
	gold, red, blue, green := theme.Fallback(), theme.Derive(25, 0.12), theme.Derive(265, 0.12), theme.Derive(150, 0.12)
	f := newAccentFade(gold)
	f.set(red, t0)
	f.set(blue, t0.Add(anim.Accent/4)) // nearer gold: start from gold
	if _, fade := f.at(t0.Add(anim.Accent / 4)); fade.From != gold || fade.To != blue {
		t.Fatalf("early change of mind fades %+v", fade)
	}
	later := t0.Add(anim.Accent/4 + anim.Accent*3/4)
	f.set(green, later) // nearer blue: start from blue
	if _, fade := f.at(later); fade.From != blue || fade.To != green {
		t.Fatalf("late change of mind fades %+v", fade)
	}
}

type fakeColours struct {
	hue, chroma float64
	colour, ok  bool
	asked       media.Cover
}

func (f *fakeColours) Colour(c media.Cover) (float64, float64, bool, bool) {
	f.asked = c
	return f.hue, f.chroma, f.colour, f.ok
}

// colourArt is an art cache whose every album has the colour given, and no
// pictures.
type colourArt struct{ fakeColours }

func (*colourArt) Cover(media.Cover, int) (*image.NRGBA, bool) { return nil, true }
func (*colourArt) Backdrop(media.Cover) (*image.NRGBA, bool)   { return nil, true }
func (*colourArt) Ready() <-chan struct{}                      { return nil }

// A new album colour's backgrounds are rendered off the UI thread, and the
// fade to it starts once they are in the context, which draws them as they
// came rather than rendering them again.
func TestAccentFadesOnceItsBackgroundsAreRendered(t *testing.T) {
	u, off := uitest.NewContext(t, theme.DeviceW, theme.DeviceH)
	gold, red := theme.Fallback(), theme.Derive(25, 0.12)
	marked := color.NRGBA{R: 200, B: 200, A: 255}
	release, woken := make(chan struct{}), make(chan struct{}, 1)
	render := func(acc theme.Accent) []ground {
		<-release
		gs := renderGrounds(acc)
		for _, g := range gs {
			for i := 0; i < len(g.img.Pix); i += 4 {
				g.img.Pix[i], g.img.Pix[i+1], g.img.Pix[i+2] = marked.R, marked.G, marked.B
			}
		}
		return gs
	}
	a := &App{ui: u, opts: Options{Covers: &colourArt{fakeColours{hue: 25, chroma: 0.12, colour: true, ok: true}}},
		fade: newAccentFade(gold)}
	a.grounds = newGrounds(gold, render, func() { woken <- struct{}{} })
	st := player.State{Queued: true, Track: player.Track{Path: "/m/1.flac", AlbumArtist: "Odalys", Album: "Copper Season"}}

	a.followAccent(st, true, t0)
	if a.fade.target() != gold || a.fade.animating(t0) {
		t.Fatal("the fade started before the new accent's backgrounds were rendered")
	}
	close(release)
	<-woken
	a.followAccent(st, false, t0)
	if a.fade.target() != red || !a.fade.animating(t0) {
		t.Fatalf("once the backgrounds are in, the fade heads for %+v, animating %v", a.fade.target(), a.fade.animating(t0))
	}
	for _, kind := range []widgets.Background{widgets.TabGround, widgets.NowPlayingGround} {
		u.Accent, u.Fade = a.fade.at(t0.Add(anim.Accent))
		widgets.DrawBackground(u, kind)
		if c := off.Image().NRGBAAt(10, 10); c != marked {
			t.Errorf("ground %d: drew %v, not the one rendered for it", kind, c)
		}
	}
}

// With the screen off a new album's accent isn't worked out, nor its
// backgrounds rendered; they are once the screen is back on.
func TestAccentWaitsForTheScreen(t *testing.T) {
	gold := theme.Fallback()
	art := &colourArt{fakeColours{hue: 25, chroma: 0.12, colour: true, ok: true}}
	woken := make(chan struct{}, 1)
	a := &App{opts: Options{Covers: art}, fade: newAccentFade(gold), dark: true}
	a.grounds = newGrounds(gold, func(theme.Accent) []ground { return nil }, func() { woken <- struct{}{} })
	st := player.State{Queued: true, Track: player.Track{Path: "/m/1.flac", AlbumArtist: "Odalys", Album: "Copper Season"}}

	a.followAccent(st, true, t0)
	a.followAccent(st, false, t0)
	if art.asked != (media.Cover{}) || a.grounds.running {
		t.Fatal("the accent was worked out with the screen off")
	}
	a.dark = false
	a.followAccent(st, false, t0)
	if !a.grounds.running {
		t.Fatal("the screen came on, and the new accent's backgrounds weren't asked for")
	}
	<-woken
	a.followAccent(st, false, t0)
	if a.fade.target() != theme.Derive(25, 0.12) {
		t.Fatalf("the fade heads for %+v", a.fade.target())
	}
}

func TestGroundsFollowTheLatestAccent(t *testing.T) {
	gold, red, blue := theme.Fallback(), theme.Derive(25, 0.12), theme.Derive(265, 0.12)
	gate, woken := make(chan struct{}), make(chan struct{}, 1)
	var mu sync.Mutex
	var rendered []theme.Accent
	g := newGrounds(gold, func(acc theme.Accent) []ground {
		<-gate
		mu.Lock()
		rendered = append(rendered, acc)
		mu.Unlock()
		return nil
	}, func() { woken <- struct{}{} })
	// finish lets the render under way end and takes what it made.
	finish := func() (theme.Accent, bool) {
		gate <- struct{}{}
		<-woken
		acc, _, ok := g.take()
		return acc, ok
	}

	g.ask(red)
	g.ask(blue) // a change of mind while red's are rendered
	if _, ok := finish(); ok {
		t.Fatal("red's backgrounds were handed over after blue was asked for")
	}
	if acc, ok := finish(); !ok || acc != blue {
		t.Fatalf("handed over %+v, %v; want blue's", acc, ok)
	}
	g.ask(red)
	g.ask(blue) // back to the accent already in place
	if _, ok := finish(); ok {
		t.Fatal("red's backgrounds were handed over after going back to blue")
	}
	if g.running {
		t.Fatal("blue's backgrounds are rendered again")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(rendered, []theme.Accent{red, blue, red}) {
		t.Fatalf("rendered %v", rendered)
	}
}

func TestAccentFor(t *testing.T) {
	playing := player.State{Queued: true, Track: player.Track{Path: "/m/1.flac", AlbumArtist: "Odalys", Album: "Copper Season"}}
	if acc, ok := accentFor(nil, playing); !ok || acc != theme.Fallback() {
		t.Error("without art the accent isn't gold")
	}
	if acc, ok := accentFor(&fakeColours{}, player.State{}); !ok || acc != theme.Fallback() {
		t.Error("with nothing queued the accent isn't gold")
	}
	f := &fakeColours{}
	if _, ok := accentFor(f, playing); ok {
		t.Error("an album whose colour isn't known yet gave an accent")
	}
	if f.asked != (media.Cover{Artist: "Odalys", Album: "Copper Season", Path: "/m/1.flac"}) {
		t.Errorf("asked for %+v", f.asked)
	}
	if acc, ok := accentFor(&fakeColours{ok: true}, playing); !ok || acc != theme.Fallback() {
		t.Error("greyscale art isn't gold")
	}
	if acc, ok := accentFor(&fakeColours{hue: 265, chroma: 0.1, colour: true, ok: true}, playing); !ok || acc != theme.Derive(265, 0.1) {
		t.Errorf("coloured art gave %+v", acc)
	}
}
