// Package remux rewrites the Opus audio YouTube serves in WebM as an Ogg
// Opus file, without decoding it, tagging it on the way.
package remux

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

// Tags are written into the file's OpusTags header as Vorbis comments.
type Tags struct {
	Title, Artist, Album, AlbumArtist string
	Track, Year                       int
	Lyrics                            string
	Extra                             [][2]string // more comments, such as BOOMBRICK_YOUTUBE_ID
	Picture                           *Picture    // the front cover, or nil
}

// Picture is an image to embed as the front cover.
type Picture struct {
	MIME          string
	Width, Height int
	Data          []byte
}

// ErrNotOpus is a file with no Opus track.
var ErrNotOpus = errors.New("no Opus track")

// serial is the Ogg stream's serial number; a file holds one stream.
const serial = 0x426f6f6d

// maxPadding keeps a file's DiscardPadding, in nanoseconds, from overflowing
// when it is turned into samples: it is far longer than any packet.
const maxPadding = 1 << 40

// remux reads WebM from r and writes it to w as Ogg Opus tagged with t.
func remux(r io.Reader, w io.Writer, t Tags) error {
	in := newWebM(r)
	head, err := in.head()
	if err != nil {
		return err
	}
	out := &ogg{w: w, serial: serial}
	if err := out.headers(head, opusTags(t)); err != nil {
		return err
	}
	var granule int64
	for {
		p, err := in.packet()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		granule += int64(samples(p))
		if err := out.packet(p, granule); err != nil {
			return err
		}
	}
	// Padding is given in nanoseconds; Opus counts 48 kHz samples. A padding
	// that is negative or longer than the last page is bounded by finish.
	pad := min(max(in.discard, 0), maxPadding)
	return out.finish(granule - (pad*48+500_000)/1_000_000)
}

// File remuxes the WebM file src into the Ogg Opus file dst, tagged with
// t. dst is written beside itself, synced and renamed into place when
// complete.
func File(src, dst string, t Tags) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 64<<10)
	if err = remux(bufio.NewReaderSize(in, 64<<10), w, t); err != nil {
		err = fmt.Errorf("remux: %w", err)
	} else {
		err = w.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(part, dst)
	}
	if err != nil {
		os.Remove(part)
	}
	return err
}
