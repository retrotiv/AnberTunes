package helper

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/ujjwal313/boombrick/internal/spotify"
)

// vorbisFixture is the Ogg Vorbis fixture, and ffmpeg's decoding of it.
func vorbisFixture(t *testing.T) ([]byte, []float32) {
	t.Helper()
	src := filepath.Join("testdata", "noise-160k.ogg")
	ogg, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("%s is missing; run make fixtures", src)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "noise-160k.f32"))
	if err != nil {
		t.Skipf("%s is missing; run make fixtures", src)
	}
	want := make([]float32, len(raw)/4)
	for i := range want {
		want[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return ogg, want
}

// spotifyFile puts a page of Spotify's own in front of ogg, as Spotify's
// files have it: a seek table of steps all step long, a gains segment and a
// segment of FFs.
func spotifyFile(ogg []byte, frames int, step byte) []byte {
	var packet bytes.Buffer
	packet.WriteByte(0x81)
	segment := func(b []byte) {
		binary.Write(&packet, binary.LittleEndian, uint16(len(b)))
		packet.Write(b)
	}
	table := []byte{0}
	table = binary.LittleEndian.AppendUint32(table, uint32(frames))
	table = binary.LittleEndian.AppendUint32(table, uint32(len(ogg)))
	table = append(table, 0)
	table = append(table, bytes.Repeat([]byte{step}, 100)...)
	segment(table)
	segment(append([]byte{1}, make([]byte, 16)...))
	segment([]byte{2, 0xff, 0xff, 0xff, 0xff})
	return append(oggPage(0x02, 0, packet.Bytes()), ogg...)
}

// oggPage makes a page holding one packet.
func oggPage(flags byte, granule int64, packet []byte) []byte {
	var lacing []byte
	for n := len(packet); ; n -= 255 {
		if n < 255 {
			lacing = append(lacing, byte(n))
			break
		}
		lacing = append(lacing, 255)
	}
	h := append([]byte("OggS"), 0, flags)
	h = binary.LittleEndian.AppendUint64(h, uint64(granule))
	h = binary.LittleEndian.AppendUint32(h, 7) // serial
	h = binary.LittleEndian.AppendUint32(h, 0) // sequence
	h = append(h, 0, 0, 0, 0, byte(len(lacing)))
	p := append(append(h, lacing...), packet...)
	binary.LittleEndian.PutUint32(p[22:], crc(0, p))
	return p
}

// readAll reads s to its end.
func readAll(t *testing.T, s *vorbisStream) []float32 {
	t.Helper()
	var out []float32
	buf := make([]float32, 4096)
	for {
		n, err := s.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return out
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func same(t *testing.T, what string, got, want []float32, tolerance float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d samples, want %d", what, len(got), len(want))
	}
	for i := range got {
		if d := math.Abs(float64(got[i] - want[i])); d > tolerance {
			t.Fatalf("%s: sample %d is %v, want %v", what, i, got[i], want[i])
		}
	}
}

// A track decodes as ffmpeg decodes it, to the frame, with Spotify's page
// left out of the audio.
func TestVorbisDecodesTheWholeTrack(t *testing.T) {
	ogg, want := vorbisFixture(t)
	file := spotifyFile(ogg, len(want)/2, stepNear(655))
	s, err := newVorbis(bytes.NewReader(file), int64(len(file)), 1)
	if err != nil {
		t.Fatal(err)
	}
	if s.start != int64(len(file)-len(ogg)) || !s.table.ok || s.table.frames != uint32(len(want)/2) {
		t.Fatalf("Spotify's page: start %d, table %+v", s.start, s.table.ok)
	}
	same(t, "decoded", readAll(t, s), want, 1e-5)
}

// A seek lands on its frame exactly: with a table that finds the page, with
// one that points too far on, and with one that points past the end.
func TestVorbisSeeksToTheFrame(t *testing.T) {
	ogg, _ := vorbisFixture(t)
	plain, err := newVorbis(bytes.NewReader(ogg), int64(len(ogg)), 1)
	if err != nil {
		t.Fatal(err)
	}
	all := readAll(t, plain)
	frames := len(all) / 2
	// Steps of 655 make a table that finds the page; of 1000, one that
	// points too far on; of 8191, one that points past the end.
	for _, c := range []struct {
		name string
		step int
	}{{"good table", 655}, {"late table", 1000}, {"table past the end", 8191}} {
		file := spotifyFile(ogg, frames, stepNear(c.step))
		s, err := newVorbis(bytes.NewReader(file), int64(len(file)), 1)
		if err != nil {
			t.Fatal(err)
		}
		positions := []int64{0, 400, 1000, 7333, 15000, 29990, 12}
		for i := range int64(60) {
			positions = append(positions, i*7919%30000) // spread, out of order
		}
		for _, ms := range positions {
			if err := s.SeekMs(ms); err != nil {
				t.Fatalf("%s: seek to %d ms: %v", c.name, ms, err)
			}
			at := 2 * int(ms*spotify.Rate/1000)
			want := all[at:min(at+8820, len(all))]
			same(t, fmt.Sprintf("%s at %d ms", c.name, ms), readFrames(t, s, len(want)), want, 0)
		}
		for _, ms := range []int64{31000, 3600000} {
			if err := s.SeekMs(ms); err != nil {
				t.Fatalf("%s: seek to %d ms, past the end: %v", c.name, ms, err)
			}
			if n, err := s.Read(make([]float32, 64)); n != 0 || !errors.Is(err, io.EOF) {
				t.Fatalf("%s: read at %d ms, past the end: %d, %v", c.name, ms, n, err)
			}
		}
	}
}

// Levelling scales every sample by the gain.
func TestVorbisGain(t *testing.T) {
	ogg, want := vorbisFixture(t)
	s, err := newVorbis(bytes.NewReader(ogg), int64(len(ogg)), 0.5)
	if err != nil {
		t.Fatal(err)
	}
	half := make([]float32, len(want))
	for i, v := range want {
		half[i] = v * 0.5
	}
	same(t, "levelled", readAll(t, s), half, 1e-5)
}

// A page is found past bytes that only look like one.
func TestPagesSyncPastFalseStarts(t *testing.T) {
	good := oggPage(0, 1234, []byte("packet"))
	junk := append([]byte("xxOggS\x00\x00junk"), bytes.Repeat([]byte{9}, 100)...)
	data := append(junk, good...)
	p := newPages(bytes.NewReader(data), int64(len(data)))
	if err := p.sync(1); err != nil {
		t.Fatal(err)
	}
	pg, err := p.next()
	if err != nil || pg.granule != 1234 || string(pg.body) != "packet" {
		t.Fatalf("found %+v, %v", pg, err)
	}
	if err := p.sync(int64(len(junk) + 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("a sync past the last page: %v", err)
	}
}

// readFrames reads n samples from s, or to its end.
func readFrames(t *testing.T, s *vorbisStream, n int) []float32 {
	t.Helper()
	out := make([]float32, 0, n)
	buf := make([]float32, 1024)
	for len(out) < n {
		k, err := s.Read(buf[:min(len(buf), n-len(out))])
		out = append(out, buf[:k]...)
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// stepNear is the seek table's entry whose step is nearest v.
func stepNear(v int) byte {
	best := 0
	for i, s := range seekSteps {
		if abs(int(s)-v) < abs(int(seekSteps[best])-v) {
			best = i
		}
	}
	return byte(best)
}

func abs(n int) int { return max(n, -n) }
