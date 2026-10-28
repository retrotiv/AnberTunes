package decode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadMP4(t *testing.T) {
	f, err := os.Open(fixture(t, "tone-44100.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := readMP4(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.config) < 2 || len(tr.sizes) < 44 || len(tr.offsets) != len(tr.sizes) {
		t.Fatalf("config %x, %d sizes, %d offsets", tr.config, len(tr.sizes), len(tr.offsets))
	}
	for i := 1; i < len(tr.offsets); i++ {
		if tr.offsets[i] < tr.offsets[i-1]+int64(tr.sizes[i-1]) {
			t.Fatalf("frame %d overlaps the one before", i)
		}
	}
	// ffmpeg's encoder primes with 1024 samples and records them in an edit list.
	if tr.delay != 1024 || tr.length != 44100 || tr.unit != 44100 {
		t.Fatalf("delay %d, length %d, unit %d; want 1024, 44100, 44100", tr.delay, tr.length, tr.unit)
	}
}

func TestParseSMPB(t *testing.T) {
	s := " 00000000 00000840 0000037C 000000000000AC44 00000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000"
	if d, n, ok := parseSMPB(s); !ok || d != 2112 || n != 44100 {
		t.Fatalf("parseSMPB = %d, %d, %v; want 2112, 44100, true", d, n, ok)
	}
	if _, _, ok := parseSMPB("junk"); ok {
		t.Fatal("parseSMPB accepted junk")
	}
}

// mkbox builds a box around its body.
func mkbox(typ string, body ...[]byte) []byte {
	p := bytes.Join(body, nil)
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(p))), append([]byte(typ), p...)...)
}

func TestReadMP4RejectsJunk(t *testing.T) {
	ftyp := mkbox("ftyp", []byte("M4A \x00\x00\x00\x00"))
	tests := []struct {
		name string
		file []byte
		want error
	}{
		{"a box smaller than its header", append(slices.Clone(ftyp), mkbox("moov", []byte("\x00\x00\x00\x04trak"))...), errBadMP4},
		// A 64-bit size whose 16-byte header runs past the end of its parent.
		{"a header straddling its parent", append(slices.Clone(ftyp), mkbox("moov", []byte("\x00\x00\x00\x01trak\x00\x00\x00\x00"))...), errBadMP4},
		{"a header straddling the file", append(slices.Clone(ftyp), "\x00\x00\x00\x01mdat\x00\x00"...), errBadMP4},
		{"no index", append(slices.Clone(ftyp), mkbox("mdat", make([]byte, 100))...), errBadMP4},
		{"a 64-bit size too small for its header", append(slices.Clone(ftyp), "\x00\x00\x00\x01moov\x00\x00\x00\x00\x00\x00\x00\x08"...), errBadMP4},
		{"an index with no tracks", append(slices.Clone(ftyp), mkbox("moov")...), errNotAAC},
	}
	for _, tt := range tests {
		if _, err := readMP4(bytes.NewReader(tt.file), int64(len(tt.file))); !errors.Is(err, tt.want) {
			t.Errorf("%s: readMP4 = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestReadMP4DeepNesting(t *testing.T) {
	// Boxes nested 8 bytes apiece, far deeper than any recursion could take.
	const depth = 100000
	nest := make([]byte, 0, 8*depth)
	for i := 0; i < depth; i++ {
		nest = binary.BigEndian.AppendUint32(nest, uint32(8*(depth-i)))
		nest = append(nest, "edts"...)
	}
	file := mkbox("moov", mkbox("trak", nest))
	if _, err := readMP4(bytes.NewReader(file), int64(len(file))); !errors.Is(err, errNotAAC) {
		t.Fatalf("readMP4 = %v, want no AAC track", err)
	}
}

func TestSampleSizesBounded(t *testing.T) {
	stsz := func(size, count uint32) []byte {
		p := binary.BigEndian.AppendUint32(make([]byte, 4), size)
		return binary.BigEndian.AppendUint32(p, count)
	}
	// A table of one size is twelve bytes whatever its count; the count must
	// fit the file, and so must the frames.
	for _, count := range []uint32{1 << 24, 1 << 22, 262} {
		if s := sampleSizes(stsz(1, count), 261); s != nil {
			t.Errorf("a 261-byte file's table of %d sizes gave %d", count, len(s))
		}
	}
	if s := sampleSizes(stsz(4, 100), 399); s != nil {
		t.Errorf("100 frames of 4 bytes in 399 bytes gave %d", len(s))
	}
	if s := sampleSizes(stsz(4, 100), 400); len(s) != 100 {
		t.Errorf("100 frames of 4 bytes in 400 bytes gave %d", len(s))
	}
	if s := sampleSizes(stsz(0, 1<<22), 1<<30); s != nil {
		t.Errorf("a table of %d sizes in 12 bytes gave %d", 1<<22, len(s))
	}
}

func TestFrameOffsetsBounded(t *testing.T) {
	// One chunk claiming a billion frames can't make more offsets than there
	// are sizes.
	stsc := binary.BigEndian.AppendUint32(make([]byte, 4), 1)
	for _, v := range []uint32{1, 1 << 30, 1} {
		stsc = binary.BigEndian.AppendUint32(stsc, v)
	}
	if o := frameOffsets([]int64{0}, stsc, make([]uint32, 3)); len(o) != 3 {
		t.Fatalf("%d offsets, want 3", len(o))
	}
}

func FuzzReadMP4(f *testing.F) {
	for _, name := range []string{"tone-44100.m4a", "mono-22050.m4a", "noise-22050.m4a", "odd-44100.m4a", "faststart-44100.m4a", "alac.m4a"} {
		if data, err := os.ReadFile(filepath.Join("testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add(append(mkbox("ftyp", []byte("M4A \x00\x00\x00\x00")), mkbox("moov", []byte("\x00\x00\x00\x01trak"))...))
	f.Fuzz(func(t *testing.T, data []byte) {
		tr, err := readMP4(bytes.NewReader(data), int64(len(data)))
		if err == nil && (len(tr.sizes) == 0 || len(tr.offsets) != len(tr.sizes) || tr.unit <= 0) {
			t.Fatalf("a track of %d sizes, %d offsets, unit %d", len(tr.sizes), len(tr.offsets), tr.unit)
		}
	})
}

func TestFirstEditSkipsEmptyEdit(t *testing.T) {
	// An empty edit, which delays the start, then the music's: media time -1,
	// then 1024.
	v0 := []byte{0, 0, 0, 0, 0, 0, 0, 2}
	for _, e := range [][2]uint32{{500, 0xffffffff}, {1000, 1024}} {
		v0 = binary.BigEndian.AppendUint32(v0, e[0])
		v0 = binary.BigEndian.AppendUint32(v0, e[1])
		v0 = append(v0, 0, 1, 0, 0)
	}
	v1 := []byte{1, 0, 0, 0, 0, 0, 0, 2}
	for _, e := range [][2]uint64{{500, 1<<64 - 1}, {1000, 1024}} {
		v1 = binary.BigEndian.AppendUint64(v1, e[0])
		v1 = binary.BigEndian.AppendUint64(v1, e[1])
		v1 = append(v1, 0, 1, 0, 0)
	}
	for name, p := range map[string][]byte{"version 0": v0, "version 1": v1} {
		if dur, start := firstEdit(p); dur != 1000 || start != 1024 {
			t.Errorf("%s: firstEdit = %d, %d; want 1000, 1024", name, dur, start)
		}
	}
}
