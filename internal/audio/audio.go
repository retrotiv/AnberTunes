// Package audio plays decoded sound: the Source and Output interfaces and the
// engine that moves frames from one to the other.
package audio

// Source yields interleaved stereo float32 frames in [-1, 1]. A Source is not
// safe for concurrent use.
type Source interface {
	// SampleRate is the source's rate in Hz.
	SampleRate() int
	// Frames is the length in frames, or -1 if unknown.
	Frames() int64
	// Read fills buf with up to len(buf)/2 frames and returns how many it
	// wrote. At the end it returns io.EOF, possibly with n > 0.
	Read(buf []float32) (n int, err error)
	// SeekFrame moves to frame.
	SeekFrame(frame int64) error
	// Close releases the file and decoder.
	Close() error
}
