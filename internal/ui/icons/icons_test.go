package icons

import (
	"image"
	"testing"
)

func coverage(img *image.NRGBA) float64 {
	var sum float64
	for i := 3; i < len(img.Pix); i += 4 {
		sum += float64(img.Pix[i]) / 255
	}
	return sum / float64(len(img.Pix)/4)
}

func TestEveryIconRasterizes(t *testing.T) {
	for name := range paths {
		img, err := Rasterize(name, 48, 2, false)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if coverage(img) == 0 {
			t.Errorf("%s rendered empty", name)
		}
	}
}

func TestFilledPlay(t *testing.T) {
	img, err := Rasterize("play", 48, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if c := coverage(img); c < 0.15 || c > 0.40 {
		t.Errorf("play coverage = %.2f", c)
	}
}

func TestUnknownIcon(t *testing.T) {
	if _, err := Rasterize("nope", 24, 2, false); err == nil {
		t.Fatal("expected an error")
	}
}
