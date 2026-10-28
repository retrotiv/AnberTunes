// Command appicon draws the icon CrossMix's Apps menu shows for Boombrick:
// the logo in gold on a rounded square of the app's ground colour.
//
//	go run ./scripts/appicon icon.png
package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"

	"github.com/ujjwal313/boombrick/assets"
	"github.com/ujjwal313/boombrick/internal/theme"
	"github.com/ujjwal313/boombrick/internal/ui/raster"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: appicon out.png")
		os.Exit(2)
	}
	if err := write(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "appicon:", err)
		os.Exit(1)
	}
}

func write(path string) error {
	img, err := Icon(256)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Icon draws the icon size pixels square. The logo, 150 x 127 in its SVG,
// takes three quarters of the width.
func Icon(size int) (*image.NRGBA, error) {
	icon := image.NewNRGBA(image.Rect(0, 0, size, size))
	ground := raster.Shape{W: size, H: size, Radius: float64(size) * 0.22, Kind: raster.Fill}.Mask()
	draw.DrawMask(icon, icon.Bounds(), image.NewUniform(theme.Ground), image.Point{}, ground, image.Point{}, draw.Over)
	w := size * 3 / 4
	h := w * 127 / 150
	logo, err := raster.SVG(assets.Logo(), w, h)
	if err != nil {
		return nil, err
	}
	at := image.Pt((size-w)/2, (size-h)/2)
	draw.DrawMask(icon, image.Rectangle{Min: at, Max: at.Add(image.Pt(w, h))}, image.NewUniform(theme.Gold), image.Point{}, logo, image.Point{}, draw.Over)
	return icon, nil
}
