package art

import (
	"bytes"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"

	"golang.org/x/image/tiff" // registers TIFF, as the text engine does in the app

	"github.com/ujjwal313/boombrick/internal/theme"
)

var (
	red   = color.NRGBA{220, 30, 30, 255}
	green = color.NRGBA{30, 200, 60, 255}
	blue  = color.NRGBA{30, 60, 220, 255}
)

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

// near reports whether a and b differ by at most d in each colour channel.
func near(a, b color.NRGBA, d int) bool {
	diff := func(x, y uint8) int { return int(math.Abs(float64(int(x) - int(y)))) }
	return diff(a.R, b.R) <= d && diff(a.G, b.G) <= d && diff(a.B, b.B) <= d
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSquareAveragesWhatEachPixelCovers(t *testing.T) {
	img := solid(400, 400, red)
	for y := range 400 {
		for x := 200; x < 400; x++ {
			img.SetNRGBA(x, y, green)
		}
	}
	out := Square(img, 100)
	if out.Rect.Dx() != 100 || out.Rect.Dy() != 100 {
		t.Fatalf("size %v", out.Rect)
	}
	for y := range 100 {
		for x := range 100 {
			want := red
			if x >= 50 {
				want = green
			}
			if got := out.NRGBAAt(x, y); !near(got, want, 1) {
				t.Fatalf("(%d, %d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestSquareMixesAFineChecker(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 200, 200))
	for y := range 200 {
		for x := range 200 {
			if (x+y)%2 == 0 {
				img.Pix[y*img.Stride+x] = 255
			}
		}
	}
	out := Square(img, 100)
	for i := 0; i < len(out.Pix); i += 4 {
		if v := out.Pix[i]; v < 127 || v > 128 {
			t.Fatalf("pixel %d is %d, want mid grey", i/4, v)
		}
	}
}

func TestSquareCropsToTheCentre(t *testing.T) {
	img := solid(600, 200, blue)
	for y := range 200 {
		for x := 200; x < 400; x++ {
			img.SetNRGBA(x, y, red)
		}
	}
	out := Square(img, 50)
	for y := range 50 {
		for x := range 50 {
			if got := out.NRGBAAt(x, y); !near(got, red, 1) {
				t.Fatalf("(%d, %d) = %v; the sides should be cropped", x, y, got)
			}
		}
	}
}

func TestSquareNeverScalesUp(t *testing.T) {
	if out := Square(solid(40, 60, red), 100); out.Rect.Dx() != 40 || out.Rect.Dy() != 40 {
		t.Fatalf("a 40 x 60 picture at 100 gave %v, want 40 x 40", out.Rect)
	}
}

func TestSquareReadsEveryKindOfImage(t *testing.T) {
	c := color.NRGBA{200, 100, 50, 255}
	rgba := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(rgba.Pix); i += 4 {
		rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2], rgba.Pix[i+3] = c.R, c.G, c.B, 255
	}
	gray := image.NewGray(image.Rect(0, 0, 64, 64))
	for i := range gray.Pix {
		gray.Pix[i] = 128
	}
	ycc, err := jpeg.Decode(bytes.NewReader(encodeJPEG(t, solid(64, 64, c))))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ycc.(*image.YCbCr); !ok {
		t.Fatalf("the JPEG decoded to %T, not *image.YCbCr", ycc)
	}
	paletted := image.NewPaletted(image.Rect(0, 0, 64, 64), color.Palette{c})
	for _, k := range []struct {
		name string
		img  image.Image
		want color.NRGBA
	}{
		{"NRGBA", solid(64, 64, c), c},
		{"RGBA", rgba, c},
		{"Gray", gray, color.NRGBA{128, 128, 128, 255}},
		{"YCbCr", ycc, c},
		{"Paletted", paletted, c},
	} {
		if got := Square(k.img, 16).NRGBAAt(8, 8); !near(got, k.want, 4) {
			t.Errorf("%s: %v, want %v", k.name, got, k.want)
		}
	}
}

// gradient is a picture of smooth colour over r, its chroma stored at the
// given ratio, each chroma sample from the last pixel it covers.
func gradient(r image.Rectangle, ratio image.YCbCrSubsampleRatio) *image.YCbCr {
	img := image.NewYCbCr(r, ratio)
	w, h := r.Dx(), r.Dy()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i, j := x-r.Min.X, y-r.Min.Y
			yy, cb, cr := color.RGBToYCbCr(uint8(40+170*i/w), uint8(200-150*j/h), uint8(100+60*math.Sin(float64(i+2*j)/8)))
			img.Y[img.YOffset(x, y)] = yy
			img.Cb[img.COffset(x, y)], img.Cr[img.COffset(x, y)] = cb, cr
		}
	}
	return img
}

// generic hides a picture's type, so that Square reads it pixel by pixel.
type generic struct{ image.Image }

func TestSquareYCbCrMatchesConvertingEveryPixel(t *testing.T) {
	for _, c := range []struct {
		r     image.Rectangle
		ratio image.YCbCrSubsampleRatio
		px    []int
	}{
		{image.Rect(0, 0, 300, 300), image.YCbCrSubsampleRatio420, []int{1, 37, 64, 150, 299, 300}},
		{image.Rect(1, 3, 302, 210), image.YCbCrSubsampleRatio420, []int{50, 206}},
		{image.Rect(0, 0, 240, 320), image.YCbCrSubsampleRatio422, []int{48, 100, 239}},
		{image.Rect(3, 0, 200, 200), image.YCbCrSubsampleRatio422, []int{64}},
		{image.Rect(0, 0, 257, 257), image.YCbCrSubsampleRatio444, []int{17, 128, 256}},
		{image.Rect(0, 0, 200, 200), image.YCbCrSubsampleRatio440, []int{60}},
		{image.Rect(2, 1, 203, 202), image.YCbCrSubsampleRatio411, []int{60, 199}},
		{image.Rect(0, 0, 200, 200), image.YCbCrSubsampleRatio410, []int{60}},
	} {
		img := gradient(c.r, c.ratio)
		for _, px := range c.px {
			fast, slow := Square(img, px), Square(generic{img}, px)
			worst := 0
			for i := range fast.Pix {
				worst = max(worst, int(math.Abs(float64(int(fast.Pix[i])-int(slow.Pix[i])))))
			}
			if worst > 2 || fast.Rect != slow.Rect {
				t.Errorf("%v at %v to %d px: %v, %v; channels differ by up to %d", c.ratio, c.r, px, fast.Rect, slow.Rect, worst)
			}
		}
	}
}

func BenchmarkSquare(b *testing.B) {
	ycc := gradient(image.Rect(0, 0, 3000, 3000), image.YCbCrSubsampleRatio420)
	b.Run("YCbCr", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Square(ycc, 512)
		}
	})
	nrgba := image.NewNRGBA(ycc.Rect)
	for i := range nrgba.Pix {
		nrgba.Pix[i] = uint8(i)
	}
	b.Run("NRGBA", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Square(nrgba, 512)
		}
	})
}

func TestDecode(t *testing.T) {
	img, err := Decode(encodePNG(t, solid(30, 20, red)))
	if err != nil || img.Bounds().Dx() != 30 {
		t.Fatalf("Decode(PNG) = %v, %v", img, err)
	}
	if _, err := Decode([]byte("not a picture")); err == nil {
		t.Fatal("Decode of text succeeded")
	}
	if img, err := Decode(encodeJPEG(t, solid(40, 30, red))); err != nil || img.Bounds().Dy() != 30 {
		t.Fatalf("Decode(JPEG) = %v, %v", img, err)
	}
	var tif bytes.Buffer
	if err := tiff.Encode(&tif, solid(4, 4, red), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(tif.Bytes()); !errors.Is(err, image.ErrFormat) {
		t.Fatalf("Decode(TIFF) error %v, want image.ErrFormat", err)
	}
}

// jpegHead builds the start of a JPEG: SOI, the given segments, then a frame
// header of kind sof for a w x h picture whose components have the sampling
// factors in hv (0x22 for a 2 x 2 component).
func jpegHead(w, h int, sof byte, hv []byte, before ...[]byte) []byte {
	b := []byte{0xFF, 0xD8}
	for _, s := range before {
		b = append(b, s...)
	}
	n := len(hv)
	b = append(b, 0xFF, sof, 0, byte(8+3*n), 8, byte(h>>8), byte(h), byte(w>>8), byte(w), byte(n))
	for i, v := range hv {
		b = append(b, byte(i+1), v, 0)
	}
	return append(b, 0xFF, 0xDA, 0, 2)
}

// app is an APPn segment holding n bytes of data.
func app(marker byte, n int) []byte {
	return append([]byte{0xFF, marker, byte((n + 2) >> 8), byte(n + 2)}, make([]byte, n)...)
}

var (
	ycc420 = []byte{0x22, 0x11, 0x11}
	ycc444 = []byte{0x11, 0x11, 0x11}

	// A baseline frame header, 13 bytes, to hide inside another segment.
	fakeSOF = []byte{0xFF, 0xC0, 0x00, 0x0B, 8, 0x00, 0x10, 0x00, 0x10, 1, 1, 0x11, 0}
)

// pngHead is a PNG signature and IHDR for a w x h picture of the given bit
// depth, and nothing after.
func pngHead(w, h int, depth byte) []byte {
	ihdr := []byte{'I', 'H', 'D', 'R', byte(w >> 24), byte(w >> 16), byte(w >> 8), byte(w), byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h), depth, 6, 0, 0, 0}
	b := append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0, 13)
	b = append(b, ihdr...)
	crc := crc32.ChecksumIEEE(ihdr)
	return append(b, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

func TestPNGCost(t *testing.T) {
	if got := pngCost(encodePNG(t, image.NewNRGBA64(image.Rect(0, 0, 4, 4)))); got != 8 {
		t.Errorf("a 16-bit PNG costs %v a pixel, want 8", got)
	}
	if got := pngCost(encodePNG(t, solid(4, 4, red))); got != 4 {
		t.Errorf("an 8-bit PNG costs %v a pixel, want 4", got)
	}
	if got := pngCost(nil); got != 4 {
		t.Errorf("a short file costs %v a pixel, want 4", got)
	}
}

func TestDecodeRefusesBigPNG(t *testing.T) {
	for _, c := range []struct {
		side  int
		depth byte
		big   bool
	}{
		{3547, 8, false},
		{3548, 8, true},
		{2508, 16, false},
		{2509, 16, true},
	} {
		_, err := Decode(pngHead(c.side, c.side, c.depth)) // no pixels follow, so one that fits fails some other way
		if got := errors.Is(err, ErrTooBig); got != c.big {
			t.Errorf("%d px at %d bits: error %v, ErrTooBig is %v", c.side, c.depth, err, got)
		}
	}
}

func TestJPEGCost(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
		want float64
		ok   bool
	}{
		{"baseline 4:2:0", jpegHead(100, 100, 0xC0, ycc420), 1.5, true},
		{"baseline 4:4:4", jpegHead(100, 100, 0xC0, ycc444), 3, true},
		{"progressive 4:2:0", jpegHead(100, 100, 0xC2, ycc420), 7.5, true},
		{"progressive 4:4:4", jpegHead(100, 100, 0xC2, ycc444), 15, true},
		{"grey", jpegHead(100, 100, 0xC0, []byte{0x11}), 1, true},
		{"after an APP segment", jpegHead(100, 100, 0xC2, ycc420, app(0xE0, 14)), 7.5, true},
		{"after fill bytes", jpegHead(100, 100, 0xC2, ycc420, []byte{0xFF, 0xFF, 0xFF}), 7.5, true},
		{"after restart markers", jpegHead(100, 100, 0xC2, ycc420, []byte{0xFF, 0xD0, 0xFF, 0xD7}), 7.5, true},
		{"after FF 00", jpegHead(100, 100, 0xC2, ycc420, []byte{0xFF, 0x00}), 7.5, true},
		{"after stray bytes", jpegHead(100, 100, 0xC2, ycc420, []byte{0x12, 0x34}), 7.5, true},
		// Go reads TEM and a second SOI as segments with a length, so a frame
		// header inside one isn't the frame header.
		{"past a fake SOF inside TEM", jpegHead(100, 100, 0xC2, ycc420, append([]byte{0xFF, 0x01, 0x00, 0x0F}, fakeSOF...)), 7.5, true},
		{"past a fake SOF inside a second SOI", jpegHead(100, 100, 0xC2, ycc420, append([]byte{0xFF, 0xD8, 0x00, 0x0F}, fakeSOF...)), 7.5, true},
		{"after over 64 KB of ICC profile", jpegHead(100, 100, 0xC2, ycc420, app(0xE2, 65000), app(0xE2, 65000)), 7.5, true},
		{"after several large segments", jpegHead(100, 100, 0xC0, ycc444, app(0xE1, 60000), app(0xED, 60000), app(0xE2, 60000)), 3, true},
		{"no frame header", []byte{0xFF, 0xD8, 0xFF, 0xDA, 0, 2}, 0, false},
		{"not a JPEG", []byte("hello"), 0, false},
		{"empty", nil, 0, false},
	} {
		got, ok := jpegCost(c.data)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: jpegCost = %v, %v; want %v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestJPEGCostOfATruncatedHeader(t *testing.T) {
	full := jpegHead(100, 100, 0xC2, ycc420, app(0xE0, 14), []byte{0xFF, 0xFF, 0xFF, 0x01})
	for n := range len(full) + 1 {
		jpegCost(full[:n]) // must not panic
	}
	if _, ok := jpegCost(full[:len(full)-8]); ok {
		t.Error("a header cut inside the frame header was read")
	}
}

func TestTooBig(t *testing.T) {
	for _, c := range []struct {
		name     string
		side     int
		perPixel float64
		big      bool
	}{
		{"baseline 4:2:0", 5792, 1.5, false},
		{"baseline 4:2:0", 5793, 1.5, true},
		{"baseline 4:4:4", 4096, 3, false},
		{"baseline 4:4:4", 4097, 3, true},
		{"progressive 4:2:0", 2590, 7.5, false},
		{"progressive 4:2:0", 2591, 7.5, true},
		{"progressive 4:4:4", 1831, 15, false},
		{"progressive 4:4:4", 1832, 15, true},
		{"other", 3547, 4, false},
		{"other", 3548, 4, true},
		{"huge", 1 << 20, 4, true},
	} {
		if got := tooBig(c.side, c.side, c.perPixel); got != c.big {
			t.Errorf("%s: tooBig(%d, %g) = %v", c.name, c.side, c.perPixel, got)
		}
	}
	if !tooBig(0, 10, 4) || !tooBig(10, 0, 4) {
		t.Error("an empty picture was accepted")
	}
}

func TestDecodeRefusesWhatWontFit(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
		big  bool
	}{
		{"baseline 4:2:0 in budget", jpegHead(5792, 5792, 0xC0, ycc420), false},
		{"baseline 4:2:0 over", jpegHead(5793, 5793, 0xC0, ycc420), true},
		{"progressive 4:2:0 over", jpegHead(2591, 2591, 0xC2, ycc420), true},
		{"progressive behind a big APP", jpegHead(2591, 2591, 0xC2, ycc420, app(0xE2, 65000), app(0xE2, 65000)), true},
		{"progressive 4:4:4 in budget", jpegHead(1831, 1831, 0xC2, ycc444), false},
		{"progressive 4:4:4 over", jpegHead(1832, 1832, 0xC2, ycc444), true},
	} {
		_, err := Decode(c.data) // the header has no scan, so a picture that fits fails some other way
		if got := errors.Is(err, ErrTooBig); got != c.big {
			t.Errorf("%s: Decode error %v, ErrTooBig is %v", c.name, err, got)
		}
	}
}

func TestColour(t *testing.T) {
	hue, chroma, ok := Colour(solid(100, 100, red))
	want := theme.OklabOf(red).OKLCH()
	if !ok || math.Abs(hue-want.H) > 2 || math.Abs(chroma-want.C) > 0.01 {
		t.Fatalf("Colour(red) = %v, %v, %v; want about %v, %v", hue, chroma, ok, want.H, want.C)
	}
	if _, _, ok := Colour(solid(100, 100, color.NRGBA{120, 120, 120, 255})); ok {
		t.Fatal("grey art has a colour")
	}
}

func TestBackdrop(t *testing.T) {
	img := solid(200, 200, red)
	for y := range 200 {
		for x := 100; x < 200; x++ {
			img.SetNRGBA(x, y, blue)
		}
	}
	b := Backdrop(img)
	if b.Rect.Dx() != backdropPx || b.Rect.Dy() != backdropPx {
		t.Fatalf("backdrop is %v", b.Rect)
	}
	for x := 1; x < backdropPx; x++ {
		p, q := b.NRGBAAt(x-1, 32), b.NRGBAAt(x, 32)
		if int(q.R)-int(p.R) < -40 || int(q.B)-int(p.B) > 40 {
			t.Fatalf("a hard step between x %d and %d: %v %v", x-1, x, p, q)
		}
	}
}

func TestSaturate(t *testing.T) {
	g := solid(1, 1, color.NRGBA{100, 100, 100, 255})
	saturate(g, 1.2)
	if c := g.NRGBAAt(0, 0); !near(c, color.NRGBA{100, 100, 100, 255}, 1) {
		t.Fatalf("grey became %v", c)
	}
	c := solid(1, 1, color.NRGBA{200, 100, 100, 255})
	saturate(c, 1.2)
	if got := c.NRGBAAt(0, 0); got.R <= 200 || got.G >= 100 {
		t.Fatalf("(200, 100, 100) became %v; want redder", got)
	}
}
