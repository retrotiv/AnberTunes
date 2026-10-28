package decode

/*
#cgo CFLAGS: -I/usr/include/opus
#cgo LDFLAGS: -lopus -logg
#include <stdlib.h>
#include "opusfile.h"

// bb_opus_read fills buf with up to frames stereo frames, reading past the
// short returns and holes opusfile reports. It returns the frames read, or a
// negative opusfile error.
static int bb_opus_read(OggOpusFile *of, float *buf, int frames) {
	int got = 0;
	while (got < frames) {
		int n = op_read_float_stereo(of, buf + 2*got, 2*(frames-got));
		if (n == OP_HOLE) continue;
		if (n < 0) return got > 0 ? got : n;
		if (n == 0) break;
		got += n;
	}
	return got;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// pcmTotal is the length from op_pcm_total, which returns a negative error code
// when it can't tell, as -1.
func pcmTotal(n int64) int64 {
	if n < 0 {
		return -1
	}
	return n
}

// openOpus opens an Ogg Opus file. Opus always decodes at 48 kHz, and
// opusfile applies the pre-skip and end trimming, so tracks join without gaps.
func openOpus(path *C.char) (*decoder, error) {
	var cerr C.int
	of := C.op_open_file(path, &cerr)
	if of == nil {
		return nil, fmt.Errorf("%w (opusfile error %d)", errOpen, int(cerr))
	}
	frames := pcmTotal(int64(C.op_pcm_total(of, -1)))
	return &decoder{
		rate:   48000,
		ch:     2,
		frames: frames,
		exact:  frames > 0,
		read: func(dst unsafe.Pointer, n int) (int, error) {
			got := C.bb_opus_read(of, (*C.float)(dst), C.int(n))
			if got < 0 {
				return 0, fmt.Errorf("decode: opus read error %d", int(got))
			}
			return int(got), nil
		},
		seek:  func(f int64) bool { return C.op_pcm_seek(of, C.ogg_int64_t(f)) == 0 },
		close: func() { C.op_free(of) },
	}, nil
}
