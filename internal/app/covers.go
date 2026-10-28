package app

import (
	"image"

	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/ui"
)

// Thumbs fetches pictures named by URL. *art.Web is one.
type Thumbs interface {
	Cover(url string, px int) (*image.NRGBA, bool)
	Ready() <-chan struct{}
}

// allCovers draws albums' art from the art cache and pictures named by URL
// from the web. Either may be nil.
type allCovers struct {
	albums ui.Covers
	web    Thumbs
}

func (c allCovers) Cover(cv media.Cover, px int) (*image.NRGBA, bool) {
	switch {
	case cv.URL != "":
		if c.web == nil {
			return nil, true
		}
		return c.web.Cover(cv.URL, px)
	case c.albums == nil:
		return nil, true
	}
	return c.albums.Cover(cv, px)
}

// Backdrop is the album's: pictures named by URL have none.
func (c allCovers) Backdrop(cv media.Cover) (*image.NRGBA, bool) {
	if cv.URL != "" || c.albums == nil {
		return nil, true
	}
	return c.albums.Backdrop(cv)
}
