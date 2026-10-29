// Command overlaypreview renders the in-game player card to PNG files on the
// desktop, so the design can be seen without a console. Pure Go, no SDL.
//
//	go run ./cmd/overlaypreview <out-dir>
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"time"

	"github.com/ujjwal313/boombrick/internal/overlaycard"
)

func main() {
	out := "."
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	r, err := overlaycard.New()
	if err != nil {
		panic(err)
	}
	r.SetCover(sampleCover(320))
	n := overlaycard.Now{
		Title:    "Midnight City",
		Artist:   "M83",
		Album:    "Hurry Up, We're Dreaming",
		Format:   "FLAC",
		Rate:     44100,
		Elapsed:  97 * time.Second,
		Duration: 244 * time.Second,
		Playing:  true,
		Queued:   true,
	}
	save(out+"/overlay-expanded.png", r.Render(n, overlaycard.Expanded))
	save(out+"/overlay-reduced.png", r.Render(n, overlaycard.Reduced))
	fmt.Println("wrote overlay-expanded.png and overlay-reduced.png to", out)
}

func save(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

// sampleCover makes a colourful square so the accent (taken from the art) shows.
func sampleCover(px int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			fx, fy := float64(x)/float64(px), float64(y)/float64(px)
			rr := 40 + 90*math.Sin(fx*3)
			gg := 70 + 120*fy
			bb := 180 + 60*math.Cos(fy*3)
			img.SetNRGBA(x, y, color.NRGBA{clamp8(rr), clamp8(gg), clamp8(bb), 255})
		}
	}
	return img
}

func clamp8(v float64) uint8 {
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}
