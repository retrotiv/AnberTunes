package gfx

import (
	"image"

	"github.com/veandco/go-sdl2/sdl"
)

// Offscreen is a canvas drawn by SDL's software renderer into memory, for
// tests; it needs no window or GPU.
type Offscreen struct {
	*Canvas
	surface *sdl.Surface
	r       *sdl.Renderer
}

// NewOffscreen creates a w x h offscreen canvas.
func NewOffscreen(w, h int) (*Offscreen, error) {
	s, err := sdl.CreateRGBSurfaceWithFormat(0, int32(w), int32(h), 32, uint32(sdl.PIXELFORMAT_ABGR8888))
	if err != nil {
		return nil, err
	}
	r, err := sdl.CreateSoftwareRenderer(s)
	if err != nil {
		s.Free()
		return nil, err
	}
	return &Offscreen{Canvas: New(r), surface: s, r: r}, nil
}

// Image returns a copy of what has been drawn so far.
func (o *Offscreen) Image() *image.NRGBA {
	o.r.Present() // flushes SDL's batched draw commands into the surface
	w, h := int(o.surface.W), int(o.surface.H)
	pitch := int(o.surface.Pitch)
	pix := o.surface.Pixels()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		copy(img.Pix[y*img.Stride:y*img.Stride+w*4], pix[y*pitch:y*pitch+w*4])
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

// Close frees the renderer and its surface.
func (o *Offscreen) Close() {
	o.r.Destroy()
	o.surface.Free()
}
