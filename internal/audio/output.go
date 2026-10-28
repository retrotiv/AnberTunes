package audio

// Output plays interleaved stereo 16-bit frames.
type Output interface {
	// Open prepares the device for rate Hz, reopening it if the rate changed.
	Open(rate int) error
	// Write plays buf, blocking until the device has room for it.
	Write(buf []int16) error
	// Drop discards audio queued in the device, for pause, seek and skip, and
	// reports how many frames it held.
	Drop() (int, error)
	// Close releases the device, discarding queued audio.
	Close() error
}
