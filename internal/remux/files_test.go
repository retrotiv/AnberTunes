package remux

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/ujjwal313/boombrick/internal/art"
	"github.com/ujjwal313/boombrick/internal/audio/decode"
	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/lyrics"
)

func decodeAll(t *testing.T, path string) []float32 {
	t.Helper()
	src, err := decode.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	var all []float32
	buf := make([]float32, 4096)
	for {
		n, err := src.Read(buf)
		all = append(all, buf[:2*n]...)
		if errors.Is(err, io.EOF) {
			return all
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestRemuxedFileDecodes remuxes the tone scripts/fixtures.sh makes. It must
// decode to the samples of ffmpeg's own remux, last as long as ffmpeg's
// decoding of the WebM, which honours its padding, and have tags the
// library, the art cache and the lyrics service read.
func TestRemuxedFileDecodes(t *testing.T) {
	src := filepath.Join("testdata", "tone.webm")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("%s is missing; run make fixtures", src)
	}
	var cover bytes.Buffer
	if err := jpeg.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "tone.opus")
	tags := Tags{Title: "Tone", Artist: "Fixtures", Album: "Tones", AlbumArtist: "Fixtures", Track: 3, Year: 2026,
		Lyrics: "[00:00.50]La", Extra: [][2]string{{"BOOMBRICK_YOUTUBE_ID", "tone1"}},
		Picture: &Picture{MIME: "image/jpeg", Width: 16, Height: 16, Data: cover.Bytes()}}
	if err := File(src, dst, tags); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("the part file was left")
	}

	// The audio packets, byte for byte, with no decoder in between.
	mine, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := os.ReadFile(filepath.Join("testdata", "tone-ffmpeg.opus"))
	if err != nil {
		t.Fatal(err)
	}
	myPackets, _ := packetsOf(readPages(t, mine))
	ffPackets, _ := packetsOf(readPages(t, theirs))
	if len(myPackets) != len(ffPackets) || len(myPackets) < 100 {
		t.Fatalf("%d packets; ffmpeg's remux has %d", len(myPackets), len(ffPackets))
	}
	for i := 2; i < len(myPackets); i++ {
		if !bytes.Equal(myPackets[i], ffPackets[i]) {
			t.Fatalf("packet %d differs from ffmpeg's remux", i)
		}
	}

	got, ref := decodeAll(t, dst), decodeAll(t, filepath.Join("testdata", "tone-ffmpeg.opus"))
	// ffmpeg's remux drops the padding, so its file decodes a little longer.
	if len(got) > len(ref) {
		t.Fatalf("%d samples; ffmpeg's remux decodes only %d", len(got), len(ref))
	}
	for i := range min(len(got), len(ref)) {
		if got[i] != ref[i] {
			t.Fatalf("sample %d: %v, ffmpeg's remux %v", i, got[i], ref[i])
		}
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "tone.f32"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(raw)/4 {
		t.Fatalf("%d samples; ffmpeg decodes %d", len(got), len(raw)/4)
	}
	// Two decoders: close, not equal.
	var diff, sig float64
	for i := range got {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:])))
		diff += (float64(got[i]) - v) * (float64(got[i]) - v)
		sig += v * v
	}
	if diff > sig*1e-4 {
		t.Fatalf("decodes far from ffmpeg's: error %g of %g", diff, sig)
	}

	read, err := library.ReadTags(dst)
	if err != nil || read.Title != "Tone" || read.Artist != "Fixtures" || read.Album != "Tones" ||
		read.AlbumArtist != "Fixtures" || read.Track != 3 || read.Year != 2026 {
		t.Fatalf("tags %+v, %v", read, err)
	}
	if data, ok := art.Embedded(dst); !ok || !bytes.Equal(data, cover.Bytes()) {
		t.Fatal("the art cache doesn't find the cover")
	}
	if text, ok := lyrics.Embedded(dst); !ok || text != "[00:00.50]La" {
		t.Fatalf("lyrics %q, %v", text, ok)
	}
}
