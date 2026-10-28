package helper

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// Ogg pages, as RFC 3533 frames them: a 27-byte header, a table of segment
// lengths, then the segments, which join into packets. A packet goes on while
// its segments are 255 bytes long, across pages if need be.
const (
	pageHeader    = 27
	flagContinued = 0x01 // the page's first packet began on the page before
	flagLast      = 0x04 // the stream's last page
	// syncWindow is how much is searched at once for a page to start at.
	syncWindow = 64 << 10
)

var (
	capture   = []byte("OggS")
	errNotOgg = errors.New("not an Ogg page")
)

// page is an Ogg page as read: its lacing and body are the reader's, good
// until the next page is read.
type page struct {
	flags   byte
	granule int64 // the frame position at the end of its last whole packet; -1 for none
	lacing  []byte
	body    []byte
}

// pages reads the pages of an Ogg stream in r, which ends at end.
type pages struct {
	r      io.ReaderAt
	end    int64
	off    int64 // where the next page starts
	br     *bufio.Reader
	header [pageHeader]byte
	lacing [255]byte
	body   []byte
}

func newPages(r io.ReaderAt, end int64) *pages {
	return &pages{r: r, end: end, br: bufio.NewReaderSize(nil, 16<<10)}
}

// at makes the page at off the next one read.
func (p *pages) at(off int64) {
	p.off = off
	p.br.Reset(io.NewSectionReader(p.r, off, max(p.end-off, 0)))
}

// next reads the next page, checking its CRC. At the end of the stream it
// returns io.EOF.
func (p *pages) next() (page, error) {
	if _, err := io.ReadFull(p.br, p.header[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			err = io.EOF
		}
		return page{}, err
	}
	h := p.header[:]
	if !bytes.Equal(h[:4], capture) || h[4] != 0 {
		return page{}, errNotOgg
	}
	lacing := p.lacing[:h[26]]
	if _, err := io.ReadFull(p.br, lacing); err != nil {
		return page{}, errNotOgg
	}
	size := 0
	for _, n := range lacing {
		size += int(n)
	}
	if cap(p.body) < size {
		p.body = make([]byte, size)
	}
	body := p.body[:size]
	if _, err := io.ReadFull(p.br, body); err != nil {
		return page{}, errNotOgg
	}
	want := binary.LittleEndian.Uint32(h[22:26])
	sum := crc(crc(crc(crc(0, h[:22]), []byte{0, 0, 0, 0}), h[26:]), lacing)
	if crc(sum, body) != want {
		return page{}, errNotOgg
	}
	p.off += int64(pageHeader + len(lacing) + size)
	return page{flags: h[5], granule: int64(binary.LittleEndian.Uint64(h[6:14])), lacing: lacing, body: body}, nil
}

// sync makes the next page the first whole one at or after off: its capture
// pattern found, and its CRC right. It returns io.EOF when there is none.
func (p *pages) sync(off int64) error {
	buf := make([]byte, syncWindow)
	for off < p.end {
		n, err := p.r.ReadAt(buf, off)
		if n == 0 {
			if err == nil || errors.Is(err, io.EOF) {
				return io.EOF
			}
			return err
		}
		for i := 0; ; i++ {
			j := bytes.Index(buf[i:n], capture)
			if j < 0 {
				break
			}
			i += j
			p.at(off + int64(i))
			if _, err := p.next(); err == nil {
				p.at(off + int64(i))
				return nil
			}
		}
		if n < len(buf) {
			return io.EOF
		}
		off += int64(n - len(capture) + 1)
	}
	return io.EOF
}

// crcTable is Ogg's CRC-32: polynomial 0x04c11db7, unreflected, starting
// from 0.
var crcTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for range 8 {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()

func crc(c uint32, b []byte) uint32 {
	for _, x := range b {
		c = c<<8 ^ crcTable[byte(c>>24)^x]
	}
	return c
}
