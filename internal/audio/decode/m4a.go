package decode

import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"unsafe"

	"github.com/ujjwal313/boombrick/internal/audio/aac"
)

// m4a reads AAC frames from an MP4 file and decodes them with faad2, trimming
// the encoder's priming and padding so tracks join without gaps.
type m4a struct {
	f       *os.File
	t       *mp4Track
	dec     *aac.Decoder
	spf     int64 // decoded samples per frame
	delay   int64 // decoded samples of priming
	length  int64 // decoded samples of music, or -1
	next    int   // the next frame to decode
	skip    int64 // decoded samples still to discard
	pos     int64 // samples of music read
	frame   []byte
	pcm     []float32
	pending []float32 // decoded stereo samples not yet read
}

func openM4A(path *C.char) (*decoder, error) {
	f, err := os.Open(C.GoString(path))
	if err != nil {
		return nil, err
	}
	m, err := newM4A(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	frames := m.length
	if frames < 0 { // an estimate, for the duration shown
		frames = int64(len(m.t.sizes))*m.spf - m.delay
		if frames <= 0 {
			frames = -1
		}
	}
	return &decoder{
		rate:   m.dec.Rate(),
		ch:     2,
		frames: frames,
		exact:  m.length > 0,
		read: func(dst unsafe.Pointer, n int) (int, error) {
			return m.read(unsafe.Slice((*float32)(dst), 2*n))
		},
		seek:  m.seek,
		close: m.close,
	}, nil
}

func newM4A(f *os.File) (*m4a, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	t, err := readMP4(f, info.Size())
	if err != nil {
		return nil, err
	}
	dec, err := aac.New(t.config)
	if err != nil {
		return nil, err
	}
	m := &m4a{f: f, t: t, dec: dec, spf: 1024, pcm: make([]float32, 2*aac.MaxFrame)}
	// faad2 outputs nothing for the first frame; the second shows how many
	// samples a frame makes, which SBR or upsampling doubles.
	var n int
	for i := 0; i < min(2, len(t.sizes)); i++ {
		buf, err := m.readFrame(i)
		if err == nil {
			if n, err = dec.Decode(buf, m.pcm); n > 0 {
				m.spf = int64(n)
			}
		}
		if err != nil {
			dec.Close()
			return nil, err
		}
	}
	rate := int64(dec.Rate())
	m.delay, m.length = t.delay*rate/t.unit, -1
	if t.length > 0 {
		m.length = t.length * rate / t.unit
	}
	// Encoders prime with a few thousand samples; a second's worth is a
	// damaged file, and would have us decode that much before any music.
	if m.delay > rate {
		dec.Close()
		return nil, errBadMP4
	}
	// faad2 never outputs frame 0, so no music can start before frame 1, and
	// none can end after the last frame.
	m.delay = max(m.delay, m.spf)
	if limit := int64(len(t.sizes))*m.spf - m.delay; m.length > limit {
		m.length = limit
	}
	if m.length <= 0 {
		m.length = -1
	}
	// Those frames were decoded just as reading from the start decodes them,
	// so reading carries on from them: what the second made is the first to
	// be read, less what is left of the priming.
	m.next, m.skip = min(2, len(t.sizes)), m.delay-m.spf
	drop := min(m.skip, int64(n))
	m.skip -= drop
	m.pending = m.pcm[2*drop : 2*n]
	return m, nil
}

// read fills dst with stereo samples and returns the frames written; fewer
// than asked means the music ended.
func (m *m4a) read(dst []float32) (int, error) {
	want, got := len(dst)/2, 0
	for got < want && (m.length < 0 || m.pos < m.length) {
		if len(m.pending) == 0 {
			if m.next >= len(m.t.sizes) {
				break
			}
			buf, err := m.readFrame(m.next)
			if err != nil {
				return got, err
			}
			m.next++
			n, err := m.dec.Decode(buf, m.pcm)
			if err != nil {
				return got, err
			}
			drop := min(m.skip, int64(n))
			m.skip -= drop
			m.pending = m.pcm[2*drop : 2*n]
			continue
		}
		k := min(want-got, len(m.pending)/2)
		if m.length >= 0 {
			k = int(min(int64(k), m.length-m.pos))
		}
		copy(dst[2*got:], m.pending[:2*k])
		m.pending = m.pending[2*k:]
		got += k
		m.pos += int64(k)
	}
	return got, nil
}

func (m *m4a) readFrame(i int) ([]byte, error) {
	n := int(m.t.sizes[i])
	if n > 1<<20 {
		return nil, errBadMP4
	}
	if cap(m.frame) < n {
		m.frame = make([]byte, n)
	}
	buf := m.frame[:n]
	if _, err := m.f.ReadAt(buf, m.t.offsets[i]); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("decode: frame %d of %d: %w", i, len(m.t.sizes), err)
	}
	return buf, nil
}

// settle is how far into a stream a seek may restart in the middle: closer in,
// faad2's SBR output still differs from a straight decode for thousands of
// samples after the two discarded frames, so those seeks decode from the top.
const settle = 8

// seek moves to sample s of the music. Decoding restarts two frames early and
// discards them: a frame decoded cold comes out wrong, and SBR's filterbank
// carries the tail of the frame before into the next, so one isn't enough.
func (m *m4a) seek(s int64) bool {
	if s < 0 || m.length >= 0 && s > m.length {
		return false
	}
	a := m.delay + s // in decoded samples from the stream's start
	j := a/m.spf - 2
	if j < settle {
		j = 0
	}
	if j >= int64(len(m.t.sizes)) {
		return false
	}
	if j == 0 {
		// A reset leaves faad2's SBR state behind; a new decoder starts the
		// stream as an open does, so joins from the start are exact.
		dec, err := aac.New(m.t.config)
		if err != nil {
			return false
		}
		m.dec.Close()
		m.dec = dec
	} else {
		m.dec.Reset(int(j))
	}
	// Starting from frame 0, faad2 outputs nothing for the first frame.
	start := j * m.spf
	if j == 0 {
		start = m.spf
	}
	m.next, m.skip, m.pos, m.pending = int(j), max(a-start, 0), s, nil
	return true
}

func (m *m4a) close() {
	m.dec.Close()
	m.f.Close()
}
