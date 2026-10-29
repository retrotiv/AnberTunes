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

	rb, _ := overlaycard.New()
	albums := []struct{ t, s string }{
		{"Hurry Up, We're Dreaming", "M83"},
		{"Random Access Memories", "Daft Punk"},
		{"In Rainbows", "Radiohead"},
		{"Discovery", "Daft Punk"},
		{"Currents", "Tame Impala"},
		{"AM", "Arctic Monkeys"},
		{"Blonde", "Frank Ocean"},
	}
	rows := make([]overlaycard.BrowseRow, len(albums))
	for i, a := range albums {
		rows[i] = overlaycard.BrowseRow{Title: a.t, Subtitle: a.s, Cover: tintCover(overlaycard.BrowseThumb, i)}
	}
	save(out+"/overlay-browse.png", rb.RenderBrowse(overlaycard.BrowseModel{
		Heading:  "Biblioteca",
		Tabs:     []string{"Álbuns", "Artistas", "Músicas", "Gêneros"},
		Tab:      0,
		Rows:     rows,
		Selected: 2,
	}))
	fmt.Println("wrote overlay-expanded.png, overlay-reduced.png and overlay-browse.png to", out)
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

// tintCover makes a small distinct cover per row for the browse preview.
func tintCover(px, seed int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	base := float64(seed*53%360) * math.Pi / 180
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			fy := float64(y) / float64(px)
			rr := 120 + 90*math.Sin(base)
			gg := 110 + 90*math.Sin(base+2) + 40*fy
			bb := 150 + 90*math.Sin(base+4)
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
