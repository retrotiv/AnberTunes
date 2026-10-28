package lyrics

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/dhowden/tag"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

// maxSidecar bounds an .lrc file; real ones are a few kilobytes.
const maxSidecar = 512 << 10

// Embedded returns the lyrics in the tags of the file at path: ID3's USLT, a
// LYRICS comment in FLAC or Ogg, or MP4's ©lyr. Mp3tag names the comment
// UNSYNCEDLYRICS and foobar2000 UNSYNCED LYRICS; those are read too.
func Embedded(path string) (text string, ok bool) {
	// The tag package can panic on a truncated or damaged tag.
	defer func() {
		if recover() != nil {
			text, ok = "", false
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return "", false
	}
	text = strings.TrimSpace(m.Lyrics())
	if text == "" && m.Format() == tag.VORBIS {
		// The tag package keeps Vorbis comments under lower-case names.
		raw := m.Raw()
		for _, k := range []string{"unsyncedlyrics", "unsynced lyrics"} {
			if s, ok := raw[k].(string); ok && strings.TrimSpace(s) != "" {
				text = strings.TrimSpace(s)
				break
			}
		}
	}
	return text, text != ""
}

// Sidecar returns the lyrics in an .lrc file beside path with the same name.
func Sidecar(path string) (string, bool) {
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	// Case in file names isn't reliable on FAT cards, so any case matches,
	// but one that matches exactly wins.
	var found string
	for _, e := range entries {
		name := e.Name()
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		if e.IsDir() || !strings.EqualFold(ext, ".lrc") || !strings.EqualFold(stem, base) {
			continue
		}
		if found == "" || stem == base {
			found = name
		}
	}
	if found == "" {
		return "", false
	}
	p := filepath.Join(dir, found)
	if fi, err := os.Stat(p); err != nil || fi.Size() > maxSidecar {
		return "", false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return decodeText(data), true
}

// decodeText reads a text file as UTF-16 when it starts with a byte order
// mark, else as UTF-8, or as Windows-1252, which older .lrc files use, when it
// isn't valid UTF-8.
func decodeText(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xff && b[1] == 0xfe || b[0] == 0xfe && b[1] == 0xff) {
		dec := unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder()
		if s, err := dec.Bytes(b); err == nil {
			return string(s)
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	s, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(s)
}
