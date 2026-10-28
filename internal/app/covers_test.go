package app

import (
	"image"
	"testing"

	"github.com/ujjwal313/boombrick/internal/media"
)

type namedCovers struct{ asked []string }

func (c *namedCovers) Cover(cv media.Cover, px int) (*image.NRGBA, bool) {
	c.asked = append(c.asked, "album "+cv.Album)
	return nil, false
}

func (c *namedCovers) Backdrop(cv media.Cover) (*image.NRGBA, bool) {
	c.asked = append(c.asked, "backdrop "+cv.Album)
	return nil, false
}

type namedThumbs struct{ asked []string }

func (t *namedThumbs) Cover(url string, px int) (*image.NRGBA, bool) {
	t.asked = append(t.asked, url)
	return nil, false
}

func (t *namedThumbs) Ready() <-chan struct{} { return nil }

func TestCoversGoWhereTheyAreNamed(t *testing.T) {
	albums, web := &namedCovers{}, &namedThumbs{}
	c := allCovers{albums: albums, web: web}
	c.Cover(media.Cover{Artist: "Radiohead", Album: "Kid A"}, 73)
	c.Cover(media.Cover{Album: "OK Computer", URL: "https://example.com/ok"}, 73)
	if _, none := c.Backdrop(media.Cover{Album: "OK Computer", URL: "https://example.com/ok"}); !none {
		t.Fatal("a picture named by URL has a backdrop")
	}
	c.Backdrop(media.Cover{Artist: "Radiohead", Album: "Kid A"})
	if len(albums.asked) != 2 || albums.asked[0] != "album Kid A" || albums.asked[1] != "backdrop Kid A" {
		t.Fatalf("albums asked %q", albums.asked)
	}
	if len(web.asked) != 1 || web.asked[0] != "https://example.com/ok" {
		t.Fatalf("web asked %q", web.asked)
	}
	if _, none := (allCovers{}).Cover(media.Cover{URL: "https://example.com/x"}, 73); !none {
		t.Fatal("no web, yet a picture may come")
	}
}
