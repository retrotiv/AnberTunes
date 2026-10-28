package remux

import (
	"encoding/binary"
	"errors"
	"io"
)

// pageBody is where an audio page is closed: about 0.4 s at 160 kbps.
const pageBody = 8 << 10

// Page flags.
const (
	continued = 0x01
	first     = 0x02
	last      = 0x04
)

var errPacket = errors.New("packet too large for a page")

// ogg writes one logical Ogg stream (RFC 3533) holding Opus (RFC 7845).
type ogg struct {
	w       io.Writer
	serial  uint32
	seq     uint32
	segs    []byte // the open page's lacing values
	body    []byte // and its data
	granule int64  // the samples decoded by the end of the open page's last packet
	base    int64  // and by the end of the page before it
	head    []byte
}

// headers writes OpusHead alone on the first page and OpusTags on the next
// pages, each ending its page, with granule 0.
func (o *ogg) headers(head, tags []byte) error {
	if err := o.whole(head, first); err != nil {
		return err
	}
	return o.whole(tags, 0)
}

// whole writes packet p on as many pages as it needs.
func (o *ogg) whole(p []byte, flags byte) error {
	segs := lacing(nil, len(p))
	for len(segs) > 0 {
		n := min(len(segs), 255)
		size := 0
		for _, s := range segs[:n] {
			size += int(s)
		}
		// A page where no packet ends has granule -1 (RFC 3533).
		granule := int64(0)
		if n < len(segs) {
			granule = -1
		}
		if err := o.page(flags, granule, segs[:n], p[:size]); err != nil {
			return err
		}
		segs, p = segs[n:], p[size:]
		flags = continued
	}
	return nil
}

// packet adds an audio packet ending at granule, first closing the open page
// if the packet won't fit. A page is closed only to make room, so the last
// page always holds a packet.
func (o *ogg) packet(p []byte, granule int64) error {
	n := len(p)/255 + 1
	if n > 255 {
		return errPacket
	}
	if len(o.segs) > 0 && (len(o.segs)+n > 255 || len(o.body)+len(p) > pageBody) {
		if err := o.flush(0); err != nil {
			return err
		}
	}
	o.segs = lacing(o.segs, len(p))
	o.body = append(o.body, p...)
	o.granule = granule
	return nil
}

// finish writes the open page as the last, ending at granule end, which may
// trim the stream's last samples; no further than this page's own.
func (o *ogg) finish(end int64) error {
	o.granule = min(max(end, o.base), o.granule)
	return o.flush(last)
}

func (o *ogg) flush(flags byte) error {
	err := o.page(flags, o.granule, o.segs, o.body)
	o.base = o.granule
	o.segs, o.body = o.segs[:0], o.body[:0]
	return err
}

// page writes one page.
func (o *ogg) page(flags byte, granule int64, segs, body []byte) error {
	h := append(o.head[:0], "OggS"...)
	h = append(h, 0, flags)
	h = binary.LittleEndian.AppendUint64(h, uint64(granule))
	h = binary.LittleEndian.AppendUint32(h, o.serial)
	h = binary.LittleEndian.AppendUint32(h, o.seq)
	h = append(h, 0, 0, 0, 0, byte(len(segs))) // the CRC, filled in below
	h = append(h, segs...)
	binary.LittleEndian.PutUint32(h[22:], crc(crc(0, h), body))
	o.head = h
	o.seq++
	if _, err := o.w.Write(h); err != nil {
		return err
	}
	_, err := o.w.Write(body)
	return err
}

// lacing appends the lacing values of a packet of n bytes.
func lacing(dst []byte, n int) []byte {
	for ; n >= 255; n -= 255 {
		dst = append(dst, 255)
	}
	return append(dst, byte(n))
}

// crcTable is Ogg's CRC: polynomial 0x04c11db7, not reflected.
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

func crc(c uint32, p []byte) uint32 {
	for _, b := range p {
		c = c<<8 ^ crcTable[byte(c>>24)^b]
	}
	return c
}
