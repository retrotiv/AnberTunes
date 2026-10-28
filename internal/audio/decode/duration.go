package decode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

var errNoLength = errors.New("decode: no length in the file's headers")

// Duration reads a file's length from its headers without decoding it, for
// the library scan: FLAC's stream info, Opus's last granule position, the MP4
// track's edit list or sample table, and an MP3's Xing, Info or VBRI header,
// or an estimate from its bitrate when it has none.
func Duration(path string) (time.Duration, error) {
	format, ok := formatOf(path)
	if !ok {
		return 0, ErrUnsupported
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return format.duration(f, info.Size())
}

func seconds(samples, rate int64) time.Duration {
	return time.Duration(float64(samples) / float64(rate) * float64(time.Second))
}

// id3Size is the length of an ID3v2 tag at the start of head, or 0.
func id3Size(head []byte) int64 {
	if len(head) < 10 || string(head[:3]) != "ID3" {
		return 0
	}
	n := int64(head[6]&0x7f)<<21 | int64(head[7]&0x7f)<<14 | int64(head[8]&0x7f)<<7 | int64(head[9]&0x7f)
	if head[5]&0x10 != 0 { // a footer follows the tag
		n += 10
	}
	return n + 10
}

func flacDuration(r io.ReaderAt, _ int64) (time.Duration, error) {
	var head [10]byte
	if _, err := r.ReadAt(head[:], 0); err != nil {
		return 0, errNoLength
	}
	// "fLaC", the first block's header, then the 34-byte stream info.
	var b [42]byte
	if _, err := r.ReadAt(b[:], id3Size(head[:])); err != nil || string(b[:4]) != "fLaC" || b[4]&0x7f != 0 {
		return 0, errNoLength
	}
	// Sample rate (20 bits), channels, bit depth, then total samples (36 bits).
	v := binary.BigEndian.Uint64(b[18:26])
	rate, total := int64(v>>44), int64(v&(1<<36-1))
	if rate == 0 || total == 0 {
		return 0, errNoLength
	}
	return seconds(total, rate), nil
}

func opusDuration(r io.ReaderAt, size int64) (time.Duration, error) {
	head := make([]byte, min(size, 4096))
	if _, err := r.ReadAt(head, 0); err != nil {
		return 0, errNoLength
	}
	i := bytes.Index(head, []byte("OpusHead"))
	if i < 0 || i+12 > len(head) {
		return 0, errNoLength
	}
	preSkip := int64(binary.LittleEndian.Uint16(head[i+10:]))
	// The last page's granule position counts every sample at 48 kHz, the
	// pre-skip included.
	tail := make([]byte, min(size, 64<<10))
	if _, err := r.ReadAt(tail, size-int64(len(tail))); err != nil {
		return 0, errNoLength
	}
	for j := bytes.LastIndex(tail, []byte("OggS")); j >= 0; j = bytes.LastIndex(tail[:j], []byte("OggS")) {
		if j+14 > len(tail) {
			continue
		}
		if g := int64(binary.LittleEndian.Uint64(tail[j+6:])); g > preSkip {
			return seconds(g-preSkip, 48000), nil
		}
	}
	return 0, errNoLength
}

func m4aDuration(r io.ReaderAt, size int64) (time.Duration, error) {
	t, err := readMP4(r, size)
	if err != nil {
		return 0, err
	}
	if t.length > 0 {
		return seconds(t.length, t.unit), nil
	}
	return seconds(int64(len(t.sizes))*1024, t.unit), nil
}

// Layer III sample rates by MPEG version (0 is 2.5, 2 is 2, 3 is 1), and
// bitrates in kbps for MPEG 1, then MPEG 2 and 2.5.
var (
	mp3Rates    = [4][3]int64{{11025, 12000, 8000}, {}, {22050, 24000, 16000}, {44100, 48000, 32000}}
	mp3Bitrates = [2][15]int64{
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	}
)

func mp3Duration(r io.ReaderAt, size int64) (time.Duration, error) {
	n, rate, err := mp3Length(r, size)
	if err != nil {
		return 0, err
	}
	return seconds(n, rate), nil
}

// mp3Length is an MP3's length in samples at its sample rate: from its Xing,
// Info or VBRI header, or else estimated from its first frame's bitrate.
func mp3Length(r io.ReaderAt, size int64) (samples, rate int64, err error) {
	var head [10]byte
	if _, err := r.ReadAt(head[:], 0); err != nil {
		return 0, 0, errNoLength
	}
	start := id3Size(head[:])
	buf := make([]byte, 16<<10)
	n, _ := r.ReadAt(buf, start)
	buf = buf[:n]
	// The first Layer III frame header: 11 sync bits, then a valid version,
	// bitrate and sample rate.
	for i := 0; i+4 <= len(buf); i++ {
		h := buf[i:]
		if h[0] != 0xff || h[1]&0xe0 != 0xe0 {
			continue
		}
		version, layer, bri, sri := h[1]>>3&3, h[1]>>1&3, h[2]>>4, h[2]>>2&3
		if version == 1 || layer != 1 || bri == 0 || bri == 15 || sri == 3 {
			continue
		}
		rate, spf, table := mp3Rates[version][sri], int64(1152), 0
		if version != 3 {
			spf, table = 576, 1
		}
		if frames, trim, ok := vbrFrames(h, version == 3, h[3]>>6 == 3); ok {
			samples := frames * spf
			if trim < samples {
				samples -= trim
			}
			return samples, rate, nil
		}
		audio := size - start - int64(i)
		var tag [3]byte
		if _, err := r.ReadAt(tag[:], size-128); err == nil && string(tag[:]) == "TAG" {
			audio -= 128 // ID3v1
		}
		bitrate := mp3Bitrates[table][bri] * 1000
		return audio * 8 * rate / bitrate, rate, nil
	}
	return 0, 0, errNoLength
}

// vbrFrames reads the frame count from a Xing or Info header, which sits after
// the side information in the first frame, or from a VBRI header, 32 bytes in.
// trim is the encoder delay and padding in samples, which the frames count
// but the music doesn't; the LAME tag after a Xing or Info header holds them.
func vbrFrames(h []byte, mpeg1, mono bool) (frames, trim int64, ok bool) {
	off := 4 + 32
	switch {
	case mpeg1 && mono, !mpeg1 && !mono:
		off = 4 + 17
	case !mpeg1 && mono:
		off = 4 + 9
	}
	if len(h) >= off+12 {
		if tag := string(h[off : off+4]); (tag == "Xing" || tag == "Info") && h[off+7]&1 != 0 {
			frames = int64(binary.BigEndian.Uint32(h[off+8:]))
			// Optional byte count, table of contents and quality follow the
			// frame count; then the LAME tag, whose delay and padding sit 21
			// bytes in as two 12-bit values.
			flags, pos := h[off+7], off+12
			for bit, n := range map[byte]int{2: 4, 4: 100, 8: 4} {
				if flags&bit != 0 {
					pos += n
				}
			}
			if len(h) >= pos+24 {
				v := int64(h[pos+21])<<16 | int64(h[pos+22])<<8 | int64(h[pos+23])
				trim = v>>12 + v&0xfff
			}
			return frames, trim, true
		}
	}
	if len(h) >= 36+18 && string(h[36:40]) == "VBRI" {
		return int64(binary.BigEndian.Uint32(h[36+14:])), 0, true
	}
	return 0, 0, false
}
