package raster

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/ujjwal313/boombrick/internal/theme"
)

func alpha(img *image.NRGBA, x, y int) uint8 { return img.NRGBAAt(x, y).A }

func lum(c color.NRGBA) int { return int(c.R) + int(c.G) + int(c.B) }

func TestWhiteMask(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{128, 0, 0, 128})
	if got := WhiteMask(src).NRGBAAt(0, 0); got != (color.NRGBA{255, 255, 255, 128}) {
		t.Fatalf("WhiteMask = %v", got)
	}
}

func TestSVGCoverage(t *testing.T) {
	doc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">` +
		`<rect x="0" y="0" width="5" height="10" fill="#fff"/></svg>`
	img, err := SVG(doc, 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	if a := alpha(img, 2, 10); a != 255 {
		t.Errorf("inside alpha = %d", a)
	}
	if a := alpha(img, 17, 10); a != 0 {
		t.Errorf("outside alpha = %d", a)
	}
}

func TestFillMask(t *testing.T) {
	m := Shape{W: 100, H: 50, Radius: 10, Kind: Fill}.Mask()
	if m.Bounds() != image.Rect(0, 0, 100, 50) {
		t.Fatalf("bounds = %v", m.Bounds())
	}
	if a := alpha(m, 50, 25); a != 255 {
		t.Errorf("centre alpha = %d", a)
	}
	if a := alpha(m, 0, 0); a != 0 {
		t.Errorf("corner alpha = %d, want 0 outside the radius", a)
	}
	if a := alpha(m, 50, 0); a != 255 {
		t.Errorf("straight edge alpha = %d", a)
	}
}

func TestRingLeavesAGap(t *testing.T) {
	s := Shape{W: 100, H: 100, Radius: 12, Kind: Ring, RingOffset: 3, RingWidth: 4}
	m := s.Mask()
	o := s.Outset()
	if o != 8 {
		t.Fatalf("Outset = %d, want 8", o)
	}
	cx := o + 50
	if a := alpha(m, cx, o+50); a != 0 {
		t.Errorf("inside the element alpha = %d", a)
	}
	if a := alpha(m, cx, o-2); a != 0 {
		t.Errorf("gap alpha = %d", a)
	}
	if a := alpha(m, cx, o-5); a != 255 {
		t.Errorf("ring alpha = %d", a)
	}
}

func TestGlowFallsOff(t *testing.T) {
	s := Shape{W: 100, H: 100, Radius: 12, Kind: Glow, GlowSpread: 8, GlowBlur: 44}
	m := s.Mask()
	o := s.Outset()
	cx := o + 50
	near, mid, far := alpha(m, cx, o-1), alpha(m, cx, o-20), alpha(m, cx, o-50)
	if !(near > mid && mid > far) {
		t.Errorf("glow should fade: %d, %d, %d", near, mid, far)
	}
	if a := alpha(m, cx, o-8); a < 115 || a > 145 {
		t.Errorf("alpha at the spread distance = %d, want about half", a)
	}
}

func TestGlowIsClearInsideTheBox(t *testing.T) {
	s := Shape{W: 100, H: 100, Radius: 12, Kind: Glow, GlowSpread: 8, GlowBlur: 44}
	m := s.Mask()
	o := s.Outset()
	if a := alpha(m, o+50, o+50); a != 0 {
		t.Errorf("alpha at the centre = %d, want 0", a)
	}
	if a := alpha(m, o+50, o+10); a != 0 {
		t.Errorf("alpha well inside the edge = %d, want 0", a)
	}
}

func TestRadialGradient(t *testing.T) {
	red := theme.OklabOf(theme.Hex("#ff0000"))
	ground := theme.OklabOf(theme.Ground)
	img := RadialGradient(100, 100, 0, 0, 130, 100, []Stop{{0, red}, {0.62, ground}})
	if got := img.NRGBAAt(0, 0); lum(got) < 200 || got.R < 230 {
		t.Errorf("origin = %v, want close to red", got)
	}
	if d := lum(img.NRGBAAt(99, 99)) - lum(theme.Ground); d < -3 || d > 3 {
		t.Errorf("far corner = %v, want ground", img.NRGBAAt(99, 99))
	}
}

func TestPlaceholderVariants(t *testing.T) {
	for v := 0; v < 4; v++ {
		img := Placeholder(200, 12, 35, v, nil, 0, 0)
		if img.Bounds() != image.Rect(0, 0, 200, 200) {
			t.Fatalf("variant %d bounds = %v", v, img.Bounds())
		}
		if a := alpha(img, 0, 0); a != 0 {
			t.Errorf("variant %d corner alpha = %d", v, a)
		}
		if a := alpha(img, 100, 100); a != 255 {
			t.Errorf("variant %d centre alpha = %d", v, a)
		}
	}
}

func TestPlaceholderStripes(t *testing.T) {
	img := Placeholder(200, 0, 35, 1, nil, 0, 0)
	dark := img.NRGBAAt(100, 189)  // 5 % up from the bottom
	light := img.NRGBAAt(100, 173) // 13 % up
	if lum(light) <= lum(dark) {
		t.Errorf("stripe %v should be lighter than %v", light, dark)
	}
}

func TestShadowFillsInside(t *testing.T) {
	s := Shape{W: 100, H: 100, Radius: 16, Kind: Shadow, GlowBlur: 20}
	m := s.Mask()
	o := s.Outset()
	if a := alpha(m, o+50, o+50); a < 250 {
		t.Errorf("shadow centre alpha = %d, want nearly opaque", a)
	}
	if near, far := alpha(m, o+50, o-2), alpha(m, o+50, o-20); !(near > far) {
		t.Errorf("shadow should fade outside: %d, %d", near, far)
	}
}

func TestSectorSweepsClockwiseFromTheTop(t *testing.T) {
	m := Shape{W: 40, H: 40, Radius: 20, Kind: Sector, Sweep: 0.25}.Mask()
	if a := alpha(m, 30, 10); a != 255 {
		t.Errorf("top right = %d", a)
	}
	for _, p := range [][2]int{{10, 10}, {30, 30}, {10, 30}} {
		if a := alpha(m, p[0], p[1]); a != 0 {
			t.Errorf("%v = %d", p, a)
		}
	}
	full := Shape{W: 40, H: 40, Radius: 20, Kind: Sector, Sweep: 1}.Mask()
	disc := Shape{W: 40, H: 40, Radius: 20, Kind: Fill}.Mask()
	if !bytes.Equal(full.Pix, disc.Pix) {
		t.Error("a full sweep isn't the disc")
	}
	if a := alpha(Shape{W: 40, H: 40, Radius: 20, Kind: Sector}.Mask(), 30, 30); a != 0 {
		t.Errorf("no sweep covers %d", a)
	}
}
