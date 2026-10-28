package audio

// ring holds the most recent samples written to the device.
type ring struct {
	buf    []int16
	end, n int
}

func newRing(size int) *ring { return &ring{buf: make([]int16, size)} }

func (r *ring) add(s []int16) {
	if len(s) > len(r.buf) {
		s = s[len(s)-len(r.buf):]
	}
	for len(s) > 0 {
		k := copy(r.buf[r.end:], s)
		r.end = (r.end + k) % len(r.buf)
		r.n = min(r.n+k, len(r.buf))
		s = s[k:]
	}
}

// tail fills dst with the last len(dst) samples, oldest first. dst must not be
// longer than what has been added.
func (r *ring) tail(dst []int16) []int16 {
	start := (r.end - len(dst) + len(r.buf)) % len(r.buf)
	k := copy(dst, r.buf[start:])
	copy(dst[k:], r.buf)
	return dst
}

func (r *ring) len() int { return r.n }
func (r *ring) clear()   { r.end, r.n = 0, 0 }
