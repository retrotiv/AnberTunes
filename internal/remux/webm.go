package remux

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math/bits"
)

// Matroska element IDs read here.
const (
	idEBML           = 0x1A45DFA3
	idSegment        = 0x18538067
	idTracks         = 0x1654AE6B
	idTrackEntry     = 0xAE
	idTrackNumber    = 0xD7
	idCodecID        = 0x86
	idCodecPrivate   = 0x63A2
	idCluster        = 0x1F43B675
	idSimpleBlock    = 0xA3
	idBlockGroup     = 0xA0
	idBlock          = 0xA1
	idDiscardPadding = 0x75A2
)

// through are the masters whose children are read in place, so a file reads
// as one flat run of elements and the masters' sizes, known or not, needn't
// be tracked. Every other element is skipped whole.
var through = map[uint32]bool{idSegment: true, idTracks: true, idTrackEntry: true, idCluster: true, idBlockGroup: true}

// Limits on what is read into memory.
const (
	maxHeader = 1 << 10
	maxBlock  = 1 << 20
	maxTracks = 32 // a file has two or three
)

var errFormat = errors.New("not a WebM file")

var errTooBig = errors.New("block too large")

// errLaced is a block holding more than one packet, which YouTube's files
// never have.
var errLaced = errors.New("laced block")

type trackEntry struct {
	number  uint64
	codec   string
	private []byte
}

// webm reads the Opus packets of a WebM file in order.
type webm struct {
	r       *bufio.Reader
	off     int64 // bytes read
	end     int64 // where the segment ends, when its size is known
	tracks  []trackEntry
	track   uint64 // the Opus track's number
	discard int64  // nanoseconds of padding to trim from the end

	// The padding of the Opus block just read, when it sits in a BlockGroup.
	groupPad  int64
	groupOpus bool
	buf       []byte
}

func newWebM(r io.Reader) *webm {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, 64<<10)
	}
	return &webm{r: br}
}

// head reads up to the first cluster and returns the Opus track's
// CodecPrivate, which is its OpusHead.
func (w *webm) head() ([]byte, error) {
	for start := true; ; start = false {
		id, size, err := w.element()
		switch {
		case errors.Is(err, io.EOF) && !start:
			return nil, io.ErrUnexpectedEOF
		case err != nil:
			return nil, noEOF(err)
		case start && id != idEBML:
			return nil, errFormat
		}
		switch id {
		case idCluster:
			for _, t := range w.tracks {
				if t.codec == "A_OPUS" && len(t.private) >= 19 && string(t.private[:8]) == "OpusHead" {
					w.track = t.number
					return t.private, nil
				}
			}
			return nil, ErrNotOpus
		case idSegment:
			if size >= 0 {
				w.end = w.off + size
			}
		case idTrackEntry:
			if len(w.tracks) >= maxTracks {
				return nil, errFormat
			}
			w.tracks = append(w.tracks, trackEntry{})
		case idTrackNumber, idCodecID, idCodecPrivate:
			if len(w.tracks) == 0 || size < 0 || size > maxHeader {
				return nil, errFormat
			}
			b, err := w.read(size)
			if err != nil {
				return nil, err
			}
			t := &w.tracks[len(w.tracks)-1]
			switch id {
			case idTrackNumber:
				t.number = uint64(beInt(b, false))
			case idCodecID:
				t.codec = string(b)
			default:
				t.private = bytes.Clone(b)
			}
		default:
			if !through[id] {
				if err := w.skip(size); err != nil {
					return nil, err
				}
			}
		}
	}
}

// packet returns the next packet of the Opus track, valid until the next
// call, or io.EOF after the last. A file that ends before its segment's
// stated size was cut short.
func (w *webm) packet() ([]byte, error) {
	for {
		id, size, err := w.element()
		if errors.Is(err, io.EOF) && w.off < w.end {
			return nil, io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}
		switch {
		case id == idBlockGroup:
			w.groupPad, w.groupOpus = 0, false
		case through[id]:
		case id == idSimpleBlock || id == idBlock:
			if size > maxBlock {
				return nil, errTooBig
			}
			if size < 4 {
				return nil, errFormat
			}
			b, err := w.read(size)
			if err != nil {
				return nil, err
			}
			track, n := uvint(b)
			if n == 0 || len(b) < n+3 {
				return nil, errFormat
			}
			if track != w.track {
				continue
			}
			if b[n+2]&0x06 != 0 {
				return nil, errLaced
			}
			// Padding belongs to its own block: the last Opus block's trims the end.
			w.discard, w.groupOpus = 0, id == idBlock
			if w.groupOpus {
				w.discard = w.groupPad
			}
			return b[n+3:], nil
		case id == idDiscardPadding:
			if size < 0 || size > 8 {
				return nil, errFormat
			}
			b, err := w.read(size)
			if err != nil {
				return nil, err
			}
			w.groupPad = beInt(b, true)
			if w.groupOpus {
				w.discard = w.groupPad
			}
		default:
			if err := w.skip(size); err != nil {
				return nil, err
			}
		}
	}
}

// element reads an element's ID and size; a size of -1 is unknown. io.EOF
// means the file ended between elements.
func (w *webm) element() (uint32, int64, error) {
	id, err := w.vint(true)
	if err != nil {
		return 0, 0, err
	}
	size, err := w.vint(false)
	if err != nil {
		return 0, 0, noEOF(err)
	}
	return uint32(id), size, nil
}

// vint reads a variable-length integer: with its length marker kept for an
// ID, which is at most 4 bytes, and without it for a size, where all ones
// means unknown.
func (w *webm) vint(id bool) (int64, error) {
	b, err := w.r.ReadByte()
	if err != nil {
		return 0, err
	}
	n := bits.LeadingZeros8(b) + 1
	if n > 8 || id && n > 4 {
		return 0, errFormat
	}
	v := uint64(b)
	if !id {
		v &= 0xff >> n
	}
	for range n - 1 {
		c, err := w.r.ReadByte()
		if err != nil {
			return 0, noEOF(err)
		}
		v = v<<8 | uint64(c)
	}
	w.off += int64(n)
	if !id && v == 1<<(7*n)-1 {
		return -1, nil
	}
	return int64(v), nil
}

// read reads an element's body of size bytes into a buffer reused by the
// next call.
func (w *webm) read(size int64) ([]byte, error) {
	if int64(cap(w.buf)) < size {
		w.buf = make([]byte, size)
	}
	b := w.buf[:size]
	if _, err := io.ReadFull(w.r, b); err != nil {
		return nil, noEOF(err)
	}
	w.off += size
	return b, nil
}

// skip passes over an element's body.
func (w *webm) skip(size int64) error {
	if size < 0 {
		return errFormat // a master of unknown size that isn't read through
	}
	if n, err := w.r.Discard(int(size)); int64(n) < size {
		return noEOF(err)
	}
	w.off += size
	return nil
}

// uvint reads a size-form variable-length integer from the start of b,
// returning its length, or 0 when b doesn't hold one.
func uvint(b []byte) (uint64, int) {
	if len(b) == 0 {
		return 0, 0
	}
	n := bits.LeadingZeros8(b[0]) + 1
	if n > 8 || len(b) < n {
		return 0, 0
	}
	v := uint64(b[0]) & (0xff >> n)
	for _, c := range b[1:n] {
		v = v<<8 | uint64(c)
	}
	return v, n
}

// beInt reads a big-endian integer, sign-extended when signed is set.
func beInt(b []byte, signed bool) int64 {
	var v int64
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	if signed && len(b) > 0 && len(b) < 8 && b[0]&0x80 != 0 {
		v -= 1 << (8 * len(b))
	}
	return v
}

// noEOF turns an end of file inside an element into an unexpected one.
func noEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
