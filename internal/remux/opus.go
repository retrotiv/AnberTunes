package remux

import (
	"encoding/base64"
	"encoding/binary"
	"strconv"
)

// samples is how many 48 kHz samples an Opus packet decodes to, from its
// TOC byte (RFC 6716, section 3.1): the frame length its configuration
// gives, times the frames its code says it holds.
func samples(p []byte) int {
	if len(p) == 0 {
		return 0
	}
	config := p[0] >> 3
	var frame int
	switch {
	case config < 12: // SILK: 10, 20, 40, 60 ms
		frame = [4]int{480, 960, 1920, 2880}[config&3]
	case config < 16: // hybrid: 10, 20 ms
		frame = [2]int{480, 960}[config&1]
	default: // CELT: 2.5, 5, 10, 20 ms
		frame = [4]int{120, 240, 480, 960}[config&3]
	}
	switch p[0] & 3 {
	case 0:
		return frame
	case 1, 2:
		return 2 * frame
	}
	if len(p) < 2 {
		return 0
	}
	// A packet holds at most 120 ms (RFC 6716, section 3.2.5).
	if n := int(p[1]&0x3f) * frame; n <= 5760 {
		return n
	}
	return 0
}

const vendor = "Boombrick"

// opusTags is the OpusTags header holding t (RFC 7845, section 5.2).
func opusTags(t Tags) []byte {
	var comments []string
	add := func(k, v string) {
		if v != "" {
			comments = append(comments, k+"="+v)
		}
	}
	add("TITLE", t.Title)
	add("ARTIST", t.Artist)
	add("ALBUM", t.Album)
	add("ALBUMARTIST", t.AlbumArtist)
	if t.Track > 0 {
		add("TRACKNUMBER", strconv.Itoa(t.Track))
	}
	if t.Year > 0 {
		add("DATE", strconv.Itoa(t.Year))
	}
	add("LYRICS", t.Lyrics)
	for _, kv := range t.Extra {
		add(kv[0], kv[1])
	}
	if t.Picture != nil {
		add("METADATA_BLOCK_PICTURE", base64.StdEncoding.EncodeToString(pictureBlock(t.Picture)))
	}
	b := []byte("OpusTags")
	b = lengthed(b, vendor)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(comments)))
	for _, c := range comments {
		b = lengthed(b, c)
	}
	return b
}

// lengthed appends s after its length, little-endian, as Vorbis comments
// are stored.
func lengthed(b []byte, s string) []byte {
	return append(binary.LittleEndian.AppendUint32(b, uint32(len(s))), s...)
}

// pictureBlock is p as a FLAC picture block, as METADATA_BLOCK_PICTURE
// holds it: big-endian, type 3 for the front cover, 24-bit colour.
func pictureBlock(p *Picture) []byte {
	be := binary.BigEndian
	b := be.AppendUint32(nil, 3)
	b = append(be.AppendUint32(b, uint32(len(p.MIME))), p.MIME...)
	b = be.AppendUint32(b, 0) // no description
	b = be.AppendUint32(b, uint32(p.Width))
	b = be.AppendUint32(b, uint32(p.Height))
	b = be.AppendUint32(b, 24)
	b = be.AppendUint32(b, 0) // not indexed
	return append(be.AppendUint32(b, uint32(len(p.Data))), p.Data...)
}
