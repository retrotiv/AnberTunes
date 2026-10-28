// Package resample converts interleaved float32 audio between sample rates with
// the speexdsp resampler, vendored here.
package resample

/*
#cgo CFLAGS: -O2 -DOUTSIDE_SPEEX -DRANDOM_PREFIX=boombrick -DFLOATING_POINT -DEXPORT=
#cgo LDFLAGS: -lm
#include "speex_resampler.h"

// The speex names are macros once RANDOM_PREFIX is set, so cgo calls these.
static SpeexResamplerState *bb_init(int ch, int quality) {
	int err = 0;
	return speex_resampler_init(ch, 48000, 48000, quality, &err);
}
static void bb_set_rate(SpeexResamplerState *st, int in, int out) { speex_resampler_set_rate(st, in, out); }
static void bb_skip_zeros(SpeexResamplerState *st) { speex_resampler_skip_zeros(st); }
static void bb_process(SpeexResamplerState *st, const float *in, unsigned *inLen, float *out, unsigned *outLen) {
	speex_resampler_process_interleaved_float(st, in, inLen, out, outLen);
}
static void bb_destroy(SpeexResamplerState *st) { speex_resampler_destroy(st); }
*/
import "C"

import "unsafe"

// Resampler converts interleaved audio of a fixed channel count. It is not safe
// for concurrent use.
type Resampler struct {
	st      *C.SpeexResamplerState
	ch      int
	quality int
	in, out int       // the rates the filter is set up for
	bypass  bool      // the filter needs its rates set before it runs
	dirty   bool      // the filter may hold audio from an earlier call
	lens    [2]C.uint // frames in and out for the C call, kept here so they do not escape per call
}

// New creates a resampler for channels channels at quality 0-10.
func New(channels, quality int) *Resampler {
	st := C.bb_init(C.int(channels), C.int(quality))
	if st == nil {
		panic("resample: speex_resampler_init failed")
	}
	return &Resampler{st: st, ch: channels, quality: quality, in: 48000, out: 48000, bypass: true}
}

// Process converts in, at inRate, to outRate into out and returns the frames
// written. out must hold len(in)*outRate/inRate frames plus a few, or Process
// panics. Equal rates are copied through. The filter starts afresh after an
// equal-rate stretch or a Reset; a change between two unequal rate pairs keeps
// running, so the tail of the old rate carries into the new one.
func (r *Resampler) Process(inRate, outRate int, in, out []float32) int {
	frames := len(in) / r.ch
	if frames == 0 {
		return 0
	}
	if inRate == outRate {
		if len(out) < len(in) {
			panic("resample: out holds too few frames")
		}
		r.bypass = true
		return copy(out, in) / r.ch
	}
	if r.bypass || inRate != r.in || outRate != r.out {
		if r.bypass {
			r.restart()
		}
		C.bb_set_rate(r.st, C.int(inRate), C.int(outRate))
		if r.bypass {
			C.bb_skip_zeros(r.st)
		}
		r.in, r.out, r.bypass = inRate, outRate, false
	}
	r.lens = [2]C.uint{C.uint(frames), C.uint(len(out) / r.ch)}
	C.bb_process(r.st, (*C.float)(unsafe.Pointer(&in[0])), &r.lens[0], (*C.float)(unsafe.Pointer(&out[0])), &r.lens[1])
	r.dirty = true
	if int(r.lens[0]) < frames {
		panic("resample: out holds too few frames")
	}
	return int(r.lens[1])
}

// Reset forgets buffered audio, for a seek or an unrelated stream.
func (r *Resampler) Reset() {
	r.restart()
	r.bypass = true
}

// restart replaces the filter with a fresh one. speexdsp's own reset_mem clears
// only part of the memory of a multichannel filter, so it is not used.
func (r *Resampler) restart() {
	if !r.dirty {
		return
	}
	C.bb_destroy(r.st)
	r.st = C.bb_init(C.int(r.ch), C.int(r.quality))
	if r.st == nil {
		panic("resample: speex_resampler_init failed")
	}
	r.dirty = false
}

// Close frees the resampler.
func (r *Resampler) Close() {
	if r.st != nil {
		C.bb_destroy(r.st)
		r.st = nil
	}
}
