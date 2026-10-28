// Package raster draws masks, gradients, placeholder art and SVGs into images
// on the CPU. Results are uploaded once and reused as textures.
package raster

import (
	"image"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// WhiteMask turns a premultiplied image into white pixels that carry the
// source's alpha, ready to be tinted with texture colour modulation.
func WhiteMask(src *image.RGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2] = 255, 255, 255
		dst.Pix[i+3] = src.Pix[i+3]
	}
	return dst
}

// SVG rasterises an SVG document at w x h pixels as a white mask. Colours in
// the document are ignored; only coverage is kept.
func SVG(doc string, w, h int) (*image.NRGBA, error) {
	icon, err := oksvg.ReadIconStream(strings.NewReader(doc), oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, scanner), 1)
	return WhiteMask(rgba), nil
}
