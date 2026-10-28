package spotify

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Rate is the sample rate Spotify's audio decodes to.
const Rate = 44100

const (
	// buffered is how much decoded audio a Stream holds ahead of the player:
	// enough to ride over a hiccup on the helper's side, little enough to
	// keep the memory small.
	buffered = 2 * Rate // samples: a second of stereo
	// readWait is the longest Read waits for audio before returning none,
	// so the engine stays free to pause or seek.
	readWait = 50 * time.Millisecond
	// reconnectWait is the pause between tries to pick a broken stream up.
	reconnectWait = 2 * time.Second
)

// Stream is a track from the helper, decoded, as the player's audio Source.
// It reads ahead on a goroutine of its own; when the helper's stream breaks
// part way, it asks again from where it got to until it succeeds, and plays
// silence meanwhile.
type Stream struct {
	h           *Helper
	id          string
	kbps        int
	level       bool
	rate        int
	frames      int64
	more, space chan struct{} // signalled when audio arrives or is read

	mu      sync.Mutex
	ring    []float32
	r, n    int   // where the next sample is read, and how many are held
	pos     int64 // the frame Read returns next
	end     bool  // the track ended: what is held is the rest of it
	err     error // the stream failed for good
	stalled bool  // the stream broke, and is being picked up again
	cancel  context.CancelFunc
	gen     int // the fill's generation: a seek starts a new one
	closed  bool
}

// Open opens track id at kbps from frame, levelled with Spotify's loudness
// data if level is set. It returns once the helper has answered, so a
// refusal comes back here and not from Read.
func (h *Helper) Open(ctx context.Context, id string, frame int64, kbps int, level bool) (*Stream, error) {
	s := &Stream{h: h, id: id, kbps: kbps, level: level, pos: frame,
		ring: make([]float32, buffered), more: make(chan struct{}, 1), space: make(chan struct{}, 1)}
	path, skip := s.at(frame)
	resp, err := h.do(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	s.rate, _ = strconv.Atoi(resp.Header.Get(HeaderRate))
	ms, _ := strconv.ParseInt(resp.Header.Get(HeaderDuration), 10, 64)
	if s.rate <= 0 || ms <= 0 {
		resp.Body.Close()
		return nil, fmt.Errorf("spotify helper: no rate or length for %s", id)
	}
	s.frames = ms * int64(s.rate) / 1000
	s.mu.Lock()
	s.restartLocked(resp.Body, frame, skip)
	s.mu.Unlock()
	return s, nil
}

// at is the request for the stream from frame, and how many frames before it
// the helper's answer starts, as it starts at a whole millisecond.
func (s *Stream) at(frame int64) (string, int64) {
	level := "0"
	if s.level {
		level = "1"
	}
	ms := frame * 1000 / Rate
	return fmt.Sprintf("/stream/%s?ms=%d&kbps=%d&level=%s", s.id, ms, s.kbps, level), frame - ms*Rate/1000
}

// SampleRate is the rate the helper decodes the track at.
func (s *Stream) SampleRate() int { return s.rate }

// Frames is the track's length in frames, as Spotify gives it.
func (s *Stream) Frames() int64 { return s.frames }

// Kbps is the quality the stream was opened at.
func (s *Stream) Kbps() int { return s.kbps }

// Stalled reports whether the stream broke and is being picked up again.
func (s *Stream) Stalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stalled
}

// Read fills buf with up to len(buf)/2 frames. With none held yet it waits a
// moment, then returns none, so the engine isn't held up.
func (s *Stream) Read(buf []float32) (int, error) {
	s.mu.Lock()
	if s.n == 0 && !s.end && s.err == nil && !s.closed {
		s.mu.Unlock()
		select {
		case <-s.more:
		case <-time.After(readWait):
		}
		s.mu.Lock()
	}
	defer s.mu.Unlock()
	if s.n == 0 {
		switch {
		case s.err != nil:
			return 0, s.err
		case s.end || s.closed:
			return 0, io.EOF
		}
		return 0, nil
	}
	k := min(len(buf)/2, s.n/2)
	for i := range 2 * k {
		buf[i] = s.ring[(s.r+i)%len(s.ring)]
	}
	s.r, s.n = (s.r+2*k)%len(s.ring), s.n-2*k
	s.pos += int64(k)
	signal(s.space)
	if s.n == 0 && s.end {
		return k, io.EOF
	}
	return k, nil
}

// SeekFrame moves to frame. At the frame it is already at it does nothing;
// elsewhere it drops what it holds and asks the helper from there.
func (s *Stream) SeekFrame(frame int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if frame == s.pos && s.err == nil {
		return nil
	}
	s.pos, s.r, s.n, s.end, s.err = frame, 0, 0, false, nil
	s.restartLocked(nil, frame, 0)
	return nil
}

// Close stops reading ahead; Read returns io.EOF once what is held is read.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	signal(s.more)
	signal(s.space)
	return nil
}

// restartLocked stops the fill under way and starts one from frame: reading
// body, which starts skip frames before it, or asking the helper when body
// is nil.
func (s *Stream) restartLocked(body io.ReadCloser, frame, skip int64) {
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.gen++
	go s.run(ctx, s.gen, body, frame, skip)
}

// run reads the helper's answers into the ring, from frame on, until the
// track ends, the stream fails for good or the fill is replaced.
func (s *Stream) run(ctx context.Context, gen int, body io.ReadCloser, frame, skip int64) {
	raw := make([]byte, 8*1024)
	for {
		if body == nil {
			var path string
			path, skip = s.at(frame)
			resp, err := s.h.do(ctx, http.MethodGet, path)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if !errors.Is(err, ErrOffline) && !errors.Is(err, ErrNoHelper) {
					s.finish(gen, err)
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(reconnectWait):
				}
				continue
			}
			body = resp.Body
		}
		frame, body = s.read(ctx, gen, body, frame, skip, raw)
		if body != nil || ctx.Err() != nil {
			return // ended, or replaced
		}
		s.mu.Lock()
		if gen == s.gen {
			s.stalled = true
		}
		s.mu.Unlock()
	}
}

// read copies body into the ring, after dropping its first skip frames,
// until it ends. It returns the frame reached and, at the track's clean end
// or when replaced, body itself (closed); nil when it broke.
func (s *Stream) read(ctx context.Context, gen int, body io.ReadCloser, frame, skip int64, raw []byte) (int64, io.ReadCloser) {
	defer body.Close()
	stop := context.AfterFunc(ctx, func() { body.Close() })
	defer stop()
	held := 0 // bytes of a frame not yet whole
	for {
		k, err := body.Read(raw[held:])
		k += held
		whole := k - k%8 // whole frames: two float32s
		from := int(min(skip, int64(whole/8))) * 8
		skip -= int64(from / 8)
		if whole > from && !s.put(ctx, gen, raw[from:whole]) {
			return frame, body
		}
		frame += int64((whole - from) / 8)
		held = copy(raw, raw[whole:k])
		switch {
		case errors.Is(err, io.EOF):
			s.finish(gen, nil)
			return frame, body
		case err != nil:
			return frame, nil
		}
	}
}

// put adds the frames in b to the ring, waiting for room; false if the fill
// was replaced meanwhile.
func (s *Stream) put(ctx context.Context, gen int, b []byte) bool {
	for len(b) > 0 {
		s.mu.Lock()
		if gen != s.gen || s.closed {
			s.mu.Unlock()
			return false
		}
		s.stalled = false
		room := (len(s.ring) - s.n) &^ 1
		k := min(room, len(b)/4)
		for i := range k {
			s.ring[(s.r+s.n+i)%len(s.ring)] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
		}
		s.n += k
		b = b[4*k:]
		if k > 0 {
			signal(s.more)
		}
		s.mu.Unlock()
		if len(b) > 0 {
			select {
			case <-s.space:
			case <-ctx.Done():
				return false
			}
		}
	}
	return true
}

// finish records the end of the fill of generation gen: the track's end, or
// err if it failed for good.
func (s *Stream) finish(gen int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		return
	}
	s.end, s.err, s.stalled = err == nil, err, false
	signal(s.more)
}

func signal(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}
