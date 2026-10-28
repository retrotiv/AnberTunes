package lyrics

import (
	"os"
	"path/filepath"
	"testing"
)

// id3WithLyrics is an ID3v2.3 tag holding text in a USLT frame, followed by
// a few bytes standing in for the audio.
func id3WithLyrics(text string) []byte {
	data := append([]byte{0, 'e', 'n', 'g', 0}, text...) // Latin-1, English, no description
	n := len(data)
	frame := append([]byte("USLT"), byte(n>>24), byte(n>>16), byte(n>>8), byte(n), 0, 0)
	frame = append(frame, data...)
	m := len(frame)
	tag := []byte{'I', 'D', '3', 3, 0, 0, byte(m >> 21 & 0x7f), byte(m >> 14 & 0x7f), byte(m >> 7 & 0x7f), byte(m & 0x7f)}
	return append(append(tag, frame...), make([]byte, 16)...)
}

func TestEmbeddedInID3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(path, id3WithLyrics("[00:01.00]Hello"), 0o644)
	if text, ok := Embedded(path); !ok || text != "[00:01.00]Hello" {
		t.Fatalf("Embedded = %q, %v", text, ok)
	}
}

// flacWithComments is a FLAC stream's header and a Vorbis comment block holding
// comments, with no audio after it.
func flacWithComments(comments ...string) []byte {
	le := func(n int) []byte { return []byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)} }
	block := append(le(4), "test"...) // the vendor
	block = append(block, le(len(comments))...)
	for _, c := range comments {
		block = append(append(block, le(len(c))...), c...)
	}
	n := len(block)
	head := []byte{'f', 'L', 'a', 'C', 0x80 | 4, byte(n >> 16), byte(n >> 8), byte(n)} // the last block, a comment
	return append(head, block...)
}

// Mp3tag writes UNSYNCEDLYRICS and foobar2000 UNSYNCED LYRICS into Vorbis
// comments, not LYRICS.
func TestEmbeddedUnderOtherVorbisNames(t *testing.T) {
	for _, key := range []string{"UNSYNCEDLYRICS", "UNSYNCED LYRICS", "unsyncedlyrics"} {
		path := filepath.Join(t.TempDir(), "a.flac")
		os.WriteFile(path, flacWithComments("TITLE=Rust", key+"=[00:01.00]Hello"), 0o644)
		if text, ok := Embedded(path); !ok || text != "[00:01.00]Hello" {
			t.Errorf("%s: Embedded = %q, %v", key, text, ok)
		}
	}
	path := filepath.Join(t.TempDir(), "b.flac")
	os.WriteFile(path, flacWithComments("LYRICS=Kept", "UNSYNCEDLYRICS=Not this"), 0o644)
	if text, _ := Embedded(path); text != "Kept" {
		t.Errorf("LYRICS should come first: %q", text)
	}
}

func TestEmbeddedInOtherContainers(t *testing.T) {
	for _, name := range []string{"lyrics.flac", "lyrics.opus", "lyrics.m4a"} {
		path := filepath.Join("testdata", name)
		if _, err := os.Stat(path); err != nil {
			t.Skipf("%s is missing; run make fixtures", path)
		}
		text, ok := Embedded(path)
		if l := Parse(text); !ok || !l.Synced || len(l.Lines) != 2 || l.Lines[1].Text != "Second line" {
			t.Errorf("%s: %q, %v", name, text, ok)
		}
	}
}

func TestEmbeddedWithoutLyrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(path, []byte("not audio"), 0o644)
	if _, ok := Embedded(path); ok {
		t.Fatal("found lyrics in a file without tags")
	}
}

func TestSidecar(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "01 Rust.flac")
	os.WriteFile(song, nil, 0o644)
	if _, ok := Sidecar(song); ok {
		t.Fatal("found an .lrc that isn't there")
	}
	os.WriteFile(filepath.Join(dir, "01 Rust.LRC"), []byte("[00:01.00]Rust"), 0o644)
	os.WriteFile(filepath.Join(dir, "02 Ember.lrc"), []byte("[00:01.00]Ember"), 0o644)
	if text, ok := Sidecar(song); !ok || text != "[00:01.00]Rust" {
		t.Fatalf("Sidecar = %q, %v", text, ok)
	}
}

func TestSidecarInWindows1252(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "Café.mp3")
	os.WriteFile(song, nil, 0o644)
	os.WriteFile(filepath.Join(dir, "Café.lrc"), []byte("[00:01.00]Caf\xe9 au lait"), 0o644)
	if text, ok := Sidecar(song); !ok || text != "[00:01.00]Café au lait" {
		t.Fatalf("Sidecar = %q, %v", text, ok)
	}
}

func TestEmbeddedSurvivesDamagedTags(t *testing.T) {
	whole := id3WithLyrics("[00:01.00]Hello")
	for n := 0; n < len(whole); n++ {
		path := filepath.Join(t.TempDir(), "x.mp3")
		os.WriteFile(path, whole[:n], 0o644)
		Embedded(path) // must not panic
	}
	bogus := append([]byte("ID3\x04\x00\x00\x7f\x7f\x7f\x7f"), 0xff, 0xff, 0xff, 0xff)
	path := filepath.Join(t.TempDir(), "y.mp3")
	os.WriteFile(path, bogus, 0o644)
	Embedded(path)
}

func TestSidecarIgnoresCaseOfName(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "01 Rust.flac")
	os.WriteFile(song, nil, 0o644)
	os.WriteFile(filepath.Join(dir, "01 rust.lrc"), []byte("[00:01.00]lower"), 0o644)
	if text, ok := Sidecar(song); !ok || text != "[00:01.00]lower" {
		t.Fatalf("Sidecar = %q, %v", text, ok)
	}
	os.WriteFile(filepath.Join(dir, "01 Rust.lrc"), []byte("[00:01.00]exact"), 0o644)
	if text, _ := Sidecar(song); text != "[00:01.00]exact" {
		t.Fatalf("exact-case match not preferred: %q", text)
	}
}

func TestSidecarInUTF16(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "a.mp3")
	os.WriteFile(song, nil, 0o644)
	le := []byte{0xff, 0xfe}
	be := []byte{0xfe, 0xff}
	for _, r := range "[00:01.00]Café" {
		le = append(le, byte(r), byte(r>>8))
		be = append(be, byte(r>>8), byte(r))
	}
	for name, data := range map[string][]byte{"le": le, "be": be} {
		os.WriteFile(filepath.Join(dir, "a.lrc"), data, 0o644)
		if text, ok := Sidecar(song); !ok || text != "[00:01.00]Café" {
			t.Errorf("%s: Sidecar = %q, %v", name, text, ok)
		}
	}
}
