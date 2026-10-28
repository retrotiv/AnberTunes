package decode

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ujjwal313/boombrick/internal/audio/aac"
)

// seekMatches seeks a fresh source to frame and checks that the next 1000
// frames are those of whole, the same file decoded straight through.
func seekMatches(t *testing.T, name string, whole []float32, frame int64) {
	t.Helper()
	src := open(t, name)
	if err := src.SeekFrame(frame); err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, 2*1000)
	n, err := src.Read(buf)
	if want := int(min(1000, int64(len(whole)/2)-frame)); n != want || err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read after seeking to %d: %d frames, %v", frame, n, err)
	}
	for i := range buf[:2*n] {
		if abs(buf[i]-whole[2*frame+int64(i)]) > 1e-3 {
			t.Fatalf("%s: sample %d after seeking to %d is %v, want %v", name, i, frame, buf[i], whole[2*frame+int64(i)])
		}
	}
}

func TestM4ASeeks(t *testing.T) {
	// Near the start, where decoding restarts from frame 0; past it, where it
	// restarts two frames early; and near the end.
	for _, name := range []string{"tone-44100.m4a", "mono-22050.m4a", "noise-22050.m4a", "odd-44100.m4a"} {
		whole := readAll(t, open(t, name))
		for _, frame := range []int64{0, 1, 1500, 3000, 5000, 9000, 17000, 20000, 22050, 40000, 44000} {
			seekMatches(t, name, whole, frame)
		}
	}
}

// neverProbed decodes a stream with a decoder that has done nothing else, and
// returns what it made.
func neverProbed(t *testing.T, name string) []float32 {
	t.Helper()
	data, err := os.ReadFile(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := readMP4(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := aac.New(tr.config)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var pcm []float32
	frame := make([]float32, 2*aac.MaxFrame)
	for i := range tr.sizes {
		n, err := dec.Decode(data[tr.offsets[i]:tr.offsets[i]+int64(tr.sizes[i])], frame)
		if err != nil {
			t.Fatal(err)
		}
		pcm = append(pcm, frame[:2*n]...)
	}
	return pcm
}

func TestM4AStartsLikeAFreshDecode(t *testing.T) {
	// The probe in newM4A must leave nothing behind, or the first frames of an
	// SBR track differ from a plain decode and tracks wouldn't join exactly.
	// Frame 0 makes no samples, and the music starts 2048 in.
	want := neverProbed(t, "mono-22050.m4a")
	for _, seek := range []bool{false, true} {
		src := open(t, "mono-22050.m4a")
		if seek {
			if err := src.SeekFrame(1000); err != nil {
				t.Fatal(err)
			}
			if err := src.SeekFrame(0); err != nil {
				t.Fatal(err)
			}
		}
		buf := make([]float32, 2*4096)
		if n, err := src.Read(buf); n != 4096 || err != nil {
			t.Fatalf("read %d, %v", n, err)
		}
		for i := range buf {
			if buf[i] != want[i] {
				t.Fatalf("seek %v: sample %d is %v, a fresh decoder makes %v", seek, i, buf[i], want[i])
			}
		}
	}
}

func TestM4AOddLength(t *testing.T) {
	// The edit list counts in milliseconds; 44022 samples is 998.2 of them.
	src := open(t, "odd-44100.m4a")
	if src.Frames() != 44022 {
		t.Fatalf("Frames = %d, want 44022", src.Frames())
	}
	if n := len(readAll(t, src)) / 2; n != 44022 {
		t.Fatalf("decoded %d frames, want 44022", n)
	}
}

func TestM4AReadDoesNotAllocate(t *testing.T) {
	src := open(t, "tone-44100.m4a")
	buf := make([]float32, 2*1024)
	n := testing.AllocsPerRun(20, func() {
		if _, err := src.Read(buf); err != nil {
			t.Error(err)
		}
	})
	if n != 0 {
		t.Fatalf("a read allocates %v times, want 0", n)
	}
}

func TestM4ATruncated(t *testing.T) {
	whole, err := os.ReadFile(fixture(t, "faststart-44100.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	// The index comes first, so a cut in the audio leaves a file that opens.
	cut := filepath.Join(t.TempDir(), "cut.m4a")
	if err := os.WriteFile(cut, whole[:len(whole)*2/3], 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Open(cut)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	buf := make([]float32, 2*1000)
	for i := 0; i < 100; i++ {
		if _, err := src.Read(buf); err != nil {
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("read ended with %v, want unexpected EOF", err)
			}
			return
		}
	}
	t.Fatal("a cut file read to the end without an error")
}

func TestM4ACutBeforeIndex(t *testing.T) {
	// ffmpeg writes the index last, so a cut file has none.
	whole, err := os.ReadFile(fixture(t, "tone-44100.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	cut := filepath.Join(t.TempDir(), "cut.m4a")
	if err := os.WriteFile(cut, whole[:len(whole)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(cut); !errors.Is(err, errBadMP4) {
		t.Fatalf("Open = %v, want a damaged file", err)
	}
}

func TestM4ASeeksAcrossTheFile(t *testing.T) {
	// The encoder's noise substitution is off for this file; faad2's noise
	// generator depends on what it decoded before, so a seek into such frames
	// could never match a straight decode.
	const name = "odd-44100.m4a"
	whole := readAll(t, open(t, name))
	for frame := int64(0); frame < 44022; frame += 997 {
		seekMatches(t, name, whole, frame)
	}
	seekMatches(t, name, whole, 44021)
}
