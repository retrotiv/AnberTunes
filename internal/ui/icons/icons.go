// Package icons rasterises the Lucide icons the design uses.
package icons

import (
	"fmt"
	"image"
	"strings"

	"github.com/ujjwal313/boombrick/internal/ui/raster"
)

// Rasterize draws an icon as a white mask of size x size px. strokeWidth is
// in grid units (Lucide's default is 2); 0 draws no stroke. filled also fills
// the outlines, as the transport controls do.
func Rasterize(name string, size int, strokeWidth float64, filled bool) (*image.NRGBA, error) {
	ds, ok := paths[name]
	if !ok {
		return nil, fmt.Errorf("icons: unknown icon %q", name)
	}
	fill, stroke := "none", "#fff"
	if filled {
		fill = "#fff"
	}
	if strokeWidth <= 0 {
		stroke = "none"
	}
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">`)
	for _, d := range ds {
		fmt.Fprintf(&b, `<path d="%s" fill="%s" stroke="%s" stroke-width="%g" stroke-linecap="round" stroke-linejoin="round"/>`,
			d, fill, stroke, strokeWidth)
	}
	b.WriteString(`</svg>`)
	return raster.SVG(b.String(), size, size)
}
