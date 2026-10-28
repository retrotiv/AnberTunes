package decode

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

var (
	errBadMP4  = errors.New("damaged MP4 file")
	errNotAAC  = errors.New("no AAC track in the MP4 file")
	maxPayload = int64(32 << 20) // boxes read whole: sample tables, configs
)

// Limits on what a file may claim. Real timescales and durations stay far
// inside them, and within them the products below can't overflow.
const (
	maxScale  = 1000000
	maxTicks  = 1 << 40
	maxFrames = 1 << 22 // AAC frames in a track: over a day of audio
)

// mp4Track is the first AAC track of an MP4 file.
type mp4Track struct {
	config  []byte  // the AudioSpecificConfig
	offsets []int64 // where each AAC frame starts in the file
	sizes   []uint32
	// unit is the track's timescale. delay and length place the music within
	// the decoded samples: the encoder's priming, then the music's length (-1
	// if unknown). They count in 1/unit seconds.
	delay, length, unit int64
	// The first edit: where the music starts in the track (-1 if no edit)
	// and how long it lasts in the movie's timescale.
	editStart, editDur int64
	mediaDur           int64 // the whole track's length, in 1/unit seconds
}

// box is one MP4 box: its type and where its payload lies.
type box struct {
	typ        string
	start, end int64
}

// eachBox calls fn for each box between start and end.
func eachBox(r io.ReaderAt, start, end int64, fn func(box) error) error {
	var h [16]byte
	for start+8 <= end {
		if err := readAt(r, h[:8], start); err != nil {
			return err
		}
		size := int64(be{p: h[:4]}.peek())
		b := box{typ: string(h[4:8]), start: start + 8}
		switch size {
		case 0: // runs to the end
			size = end - start
		case 1: // a 64-bit size follows
			if start+16 > end {
				return errBadMP4
			}
			if err := readAt(r, h[8:16], start+8); err != nil {
				return err
			}
			size = int64(be{p: h[8:16]}.peek())
			b.start += 8
		}
		if size < b.start-start {
			return errBadMP4
		}
		b.end = start + min(size, end-start) // a cut-off file's last box
		if err := fn(b); err != nil {
			return err
		}
		start = b.end
	}
	return nil
}

// readAt fills p from off. A file that ends first is damaged.
func readAt(r io.ReaderAt, p []byte, off int64) error {
	if n, err := r.ReadAt(p, off); n < len(p) {
		if errors.Is(err, io.EOF) {
			return errBadMP4
		}
		return err
	}
	return nil
}

func payload(r io.ReaderAt, b box) ([]byte, error) {
	if b.end-b.start > maxPayload {
		return nil, errBadMP4
	}
	p := make([]byte, b.end-b.start)
	if err := readAt(r, p, b.start); err != nil {
		return nil, err
	}
	return p, nil
}

// children maps the types listed to the first such box in b.
type children map[string]box

func find(r io.ReaderAt, b box, types ...string) (children, error) {
	found := children{}
	err := eachBox(r, b.start, b.end, func(c box) error {
		if _, dup := found[c.typ]; !dup && slices.Contains(types, c.typ) {
			found[c.typ] = c
		}
		return nil
	})
	return found, err
}

// find looks inside the child of that type, if there is one.
func (c children) find(r io.ReaderAt, typ string, types ...string) (children, error) {
	b, ok := c[typ]
	if !ok {
		return children{}, nil
	}
	return find(r, b, types...)
}

// payload reads the child of that type, or returns nil if there is none.
func (c children) payload(r io.ReaderAt, typ string) ([]byte, error) {
	b, ok := c[typ]
	if !ok {
		return nil, nil
	}
	return payload(r, b)
}

// be reads big-endian fields. Reading past the end marks it bad and yields
// zeros, so parsers check bad once at the end.
type be struct {
	p   []byte
	bad bool
}

func (b *be) bytes(n int) []byte {
	if b.bad || n < 0 || n > len(b.p) {
		b.bad = true
		return nil
	}
	v := b.p[:n:n]
	b.p = b.p[n:]
	return v
}

// uint reads an n-byte unsigned integer.
func (b *be) uint(n int) uint64 {
	var v uint64
	for _, c := range b.bytes(n) {
		v = v<<8 | uint64(c)
	}
	return v
}

// peek reads all of p as one integer.
func (b be) peek() uint64 { return b.uint(len(b.p)) }

// readMP4 finds the first AAC track in an MP4 file and how much of it is music.
func readMP4(r io.ReaderAt, size int64) (*mp4Track, error) {
	var t *mp4Track
	var movieScale int64
	var smpb string
	var moov bool
	err := eachBox(r, 0, size, func(b box) error {
		if b.typ != "moov" {
			return nil
		}
		moov = true
		return eachBox(r, b.start, b.end, func(b box) error {
			var err error
			switch b.typ {
			case "mvhd":
				var p []byte
				if p, err = payload(r, b); err == nil {
					movieScale, _ = timescale(p)
				}
			case "trak":
				if t == nil {
					t, err = readTrak(r, b, size)
				}
			case "udta":
				smpb, err = findSMPB(r, b)
			}
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	if !moov { // a file cut off before its index
		return nil, errBadMP4
	}
	if t == nil {
		return nil, errNotAAC
	}
	if movieScale > maxScale || t.editDur > maxTicks || t.editStart > maxTicks {
		return nil, errBadMP4
	}
	if d, n, ok := parseSMPB(smpb); ok {
		t.delay, t.length = d, n
	} else if t.editStart >= 0 {
		t.delay = t.editStart
		if t.editDur > 0 && movieScale > 0 {
			t.length = t.editDur * t.unit / movieScale
			// The edit counts in the movie's ticks, which round the length.
			// The track's own duration less the priming is exact, if the
			// edit agrees with it to within a tick.
			exact := t.mediaDur - t.editStart
			if exact > 0 && max(exact*movieScale-t.editDur*t.unit, t.editDur*t.unit-exact*movieScale) <= t.unit {
				t.length = exact
			}
		}
	}
	return t, nil
}

// readTrak reads a track, returning nil if it isn't AAC audio. The boxes it
// wants sit at fixed paths, so a file can't make it recurse.
func readTrak(r io.ReaderAt, trak box, size int64) (*mp4Track, error) {
	t := &mp4Track{length: -1, editStart: -1}
	top, err := find(r, trak, "edts", "mdia")
	if err != nil {
		return nil, err
	}
	mdia, err := top.find(r, "mdia", "mdhd", "hdlr", "minf")
	if err != nil {
		return nil, err
	}
	hdlr, err := mdia.payload(r, "hdlr")
	if err != nil {
		return nil, err
	}
	if len(hdlr) < 12 || string(hdlr[8:12]) != "soun" {
		return nil, nil
	}
	mdhd, err := mdia.payload(r, "mdhd")
	if err != nil {
		return nil, err
	}
	t.unit, t.mediaDur = timescale(mdhd)
	if t.unit <= 0 || t.unit > maxScale || t.mediaDur > maxTicks {
		return nil, errBadMP4
	}
	edts, err := top.find(r, "edts", "elst")
	if err != nil {
		return nil, err
	}
	elst, err := edts.payload(r, "elst")
	if err != nil {
		return nil, err
	}
	t.editDur, t.editStart = firstEdit(elst)

	minf, err := mdia.find(r, "minf", "stbl")
	if err != nil {
		return nil, err
	}
	stbl, err := minf.find(r, "stbl", "stsd", "stsz", "stsc", "stco", "co64")
	if err != nil {
		return nil, err
	}
	stsd, err := stbl.payload(r, "stsd")
	if err != nil {
		return nil, err
	}
	if t.config = audioConfig(stsd); t.config == nil {
		return nil, nil
	}
	stsz, err := stbl.payload(r, "stsz")
	if err != nil {
		return nil, err
	}
	stsc, err := stbl.payload(r, "stsc")
	if err != nil {
		return nil, err
	}
	stco, err := stbl.payload(r, "stco")
	if err != nil {
		return nil, err
	}
	co64, err := stbl.payload(r, "co64")
	if err != nil {
		return nil, err
	}
	t.sizes = sampleSizes(stsz, size)
	chunks := chunkOffsets(stco, 4)
	if co64 != nil {
		chunks = chunkOffsets(co64, 8)
	}
	t.offsets = frameOffsets(chunks, stsc, t.sizes)
	if len(t.sizes) == 0 || len(t.offsets) != len(t.sizes) {
		return nil, errBadMP4
	}
	return t, nil
}

// timescale reads the timescale and duration from an mvhd or mdhd payload.
func timescale(p []byte) (scale, dur int64) {
	b := be{p: p}
	w := 4
	if b.uint(1) == 1 { // version 1: 64-bit times
		w = 8
	}
	b.bytes(3 + 2*w)
	scale = int64(b.uint(4))
	return scale, int64(min(b.uint(w), 1<<62))
}

// firstEdit returns the duration, in the movie's timescale, and the start, in
// the track's, of an elst payload's first non-empty edit, or -1s.
func firstEdit(p []byte) (dur, start int64) {
	b := be{p: p}
	w := 4
	if b.uint(1) == 1 {
		w = 8
	}
	b.bytes(3)
	for n := b.uint(4); n > 0 && !b.bad; n-- {
		d, s := int64(min(b.uint(w), 1<<62)), b.uint(w)
		b.bytes(4) // rate
		media := int64(s)
		if w == 4 {
			media = int64(int32(s))
		}
		if media >= 0 && !b.bad {
			return d, media
		}
	}
	return -1, -1
}

// audioConfig finds the AudioSpecificConfig in an stsd payload's mp4a entry,
// or nil if the track isn't AAC.
func audioConfig(p []byte) []byte {
	b := be{p: p}
	b.bytes(8) // version, flags, entry count
	size, typ := int(b.uint(4)), string(b.bytes(4))
	entry := be{p: b.bytes(size - 8)}
	if b.bad || typ != "mp4a" {
		return nil
	}
	entry.bytes(8) // reserved, data reference index
	version := entry.uint(2)
	entry.bytes(6 + 12) // revision, vendor; channels, sample size, compression, packet size, rate
	switch version {    // QuickTime sound descriptions add fields
	case 1:
		entry.bytes(16)
	case 2:
		entry.bytes(36)
	}
	for !entry.bad && len(entry.p) >= 8 {
		size, typ := int(entry.uint(4)), string(entry.bytes(4))
		body := entry.bytes(size - 8)
		if typ == "esds" {
			return esdsConfig(body)
		}
	}
	return nil
}

// esdsConfig digs the AudioSpecificConfig out of an esds payload's
// descriptors, if the stream is AAC.
func esdsConfig(p []byte) []byte {
	b := be{p: p}
	b.bytes(4) // version, flags
	tag, es := descriptor(&b)
	if tag != 3 {
		return nil
	}
	es.bytes(2) // ES_ID
	flags := es.uint(1)
	if flags&0x80 != 0 {
		es.bytes(2)
	}
	if flags&0x40 != 0 {
		es.bytes(int(es.uint(1)))
	}
	if flags&0x20 != 0 {
		es.bytes(2)
	}
	tag, dc := descriptor(&es)
	if tag != 4 {
		return nil
	}
	switch dc.uint(1) { // object type
	case 0x40, 0x67: // MPEG-4 audio, MPEG-2 AAC LC; faad2 isn't built for Main or SSR
	default:
		return nil
	}
	dc.bytes(12) // stream type, buffer size, bitrates
	tag, ds := descriptor(&dc)
	if tag != 5 || ds.bad || len(ds.p) == 0 {
		return nil
	}
	return ds.p
}

// descriptor reads an MPEG-4 descriptor's tag and body.
func descriptor(b *be) (byte, be) {
	tag := byte(b.uint(1))
	n := 0
	for i := 0; i < 4; i++ {
		c := b.uint(1)
		n = n<<7 | int(c&0x7f)
		if c&0x80 == 0 {
			break
		}
	}
	body := b.bytes(n)
	return tag, be{p: body, bad: b.bad}
}

// sampleSizes reads an stsz payload. A table of one size is only a few bytes
// whatever its count, so the count must fit the file, whose frames it claims.
func sampleSizes(p []byte, fileSize int64) []uint32 {
	b := be{p: p}
	b.bytes(4)
	fixed, n := uint32(b.uint(4)), int(b.uint(4))
	if b.bad || n > maxFrames || fixed == 0 && n > len(b.p)/4 ||
		fixed != 0 && (int64(n) > fileSize || int64(n)*int64(fixed) > fileSize) {
		return nil
	}
	sizes := make([]uint32, n)
	for i := range sizes {
		sizes[i] = fixed
		if fixed == 0 {
			sizes[i] = uint32(b.uint(4))
		}
	}
	return sizes
}

func chunkOffsets(p []byte, w int) []int64 {
	b := be{p: p}
	b.bytes(4)
	n := int(b.uint(4))
	if b.bad || n > len(b.p)/w {
		return nil
	}
	offsets := make([]int64, n)
	for i := range offsets {
		offsets[i] = int64(b.uint(w))
	}
	return offsets
}

// frameOffsets lays the frames out in their chunks: stsc maps runs of chunks
// to how many frames each holds, and a chunk's frames sit back to back.
func frameOffsets(chunks []int64, stsc []byte, sizes []uint32) []int64 {
	type run struct{ first, per int }
	b := be{p: stsc}
	b.bytes(4)
	var runs []run
	for n := b.uint(4); n > 0 && !b.bad; n-- {
		first, per := int(b.uint(4)), int(b.uint(4))
		b.bytes(4) // sample description
		if !b.bad {
			runs = append(runs, run{first, per})
		}
	}
	if len(runs) == 0 {
		return nil
	}
	offsets := make([]int64, 0, len(sizes))
	r := 0
	for c, off := range chunks {
		for r+1 < len(runs) && runs[r+1].first <= c+1 {
			r++
		}
		for j := 0; j < runs[r].per && len(offsets) < len(sizes); j++ {
			offsets = append(offsets, off)
			off += int64(sizes[len(offsets)-1])
		}
	}
	return offsets
}

// findSMPB returns the iTunSMPB text in a udta box (meta, ilst, then a "----"
// item named iTunSMPB), or "".
func findSMPB(r io.ReaderAt, udta box) (string, error) {
	var smpb string
	err := eachBox(r, udta.start, udta.end, func(meta box) error {
		if meta.typ != "meta" || meta.end-meta.start < 8 {
			return nil
		}
		// ISO's meta is a full box; QuickTime's starts straight with its hdlr.
		var peek [4]byte
		if err := readAt(r, peek[:], meta.start+4); err != nil {
			return err
		}
		if string(peek[:]) != "hdlr" {
			meta.start += 4
		}
		return eachBox(r, meta.start, meta.end, func(ilst box) error {
			if ilst.typ != "ilst" {
				return nil
			}
			return eachBox(r, ilst.start, ilst.end, func(item box) error {
				if item.typ != "----" {
					return nil
				}
				var name, data string
				err := eachBox(r, item.start, item.end, func(b box) error {
					if b.typ != "name" && b.typ != "data" {
						return nil
					}
					p, err := payload(r, b)
					switch {
					case err != nil:
						return err
					case b.typ == "name" && len(p) >= 4:
						name = string(p[4:])
					case b.typ == "data" && len(p) >= 8:
						data = string(p[8:])
					}
					return nil
				})
				if name == "iTunSMPB" {
					smpb = data
				}
				return err
			})
		})
	})
	return smpb, err
}

// parseSMPB reads iTunSMPB's hex fields: the encoder delay and the music's
// length in samples, which count at the media's timescale.
func parseSMPB(s string) (delay, length int64, ok bool) {
	f := strings.Fields(s)
	if len(f) < 4 {
		return 0, 0, false
	}
	d, err1 := strconv.ParseInt(f[1], 16, 64)
	n, err2 := strconv.ParseInt(f[3], 16, 64)
	if err1 != nil || err2 != nil || d < 0 || n <= 0 || d > maxTicks || n > maxTicks {
		return 0, 0, false
	}
	return d, n, true
}
