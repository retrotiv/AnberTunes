package download

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ujjwal313/boombrick/internal/art"
	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/lyrics"
	"github.com/ujjwal313/boombrick/internal/netx"
	"github.com/ujjwal313/boombrick/internal/remux"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// Albums finds an album's page. *ytmusic.Client is one.
type Albums interface {
	Album(ctx context.Context, id string) (ytmusic.AlbumPage, error)
}

// Lyrics finds a song's lyrics online. *lyrics.Client is one.
type Lyrics interface {
	Get(ctx context.Context, t lyrics.Track) (lyrics.Result, error)
}

// coverPx is the size covers are embedded at: YouTube Music's own album
// art's.
const coverPx = 544

// finisher turns a downloaded WebM into a tagged song in the library. Its
// lookups may each be nil.
type finisher struct {
	db      *sql.DB
	music   string
	http    *http.Client
	albums  Albums
	lyrics  Lyrics
	formats library.Formats
	remux   func(src, dst string, t remux.Tags) error
	log     *slog.Logger

	// The album page fetched last and its ID: an album's songs often come one
	// after another.
	page   ytmusic.AlbumPage
	pageID string
}

// finish files the WebM at src as song s, tagged, in the Music folder, and
// indexes it. It returns the new file and its size, and whether that
// replaced a file of the song's own, filed before. Only a failed remux
// fails: the WebM is kept then, for another try. So does a cancelled ctx,
// which would otherwise file the song without its lookups.
//
// The finisher runs on one goroutine, which is what makes picking a free name
// here, before the remux writes it, safe.
func (f *finisher) finish(ctx context.Context, s ytmusic.Song, src string) (path string, size int64, replaced bool, err error) {
	// Playing a song that isn't filed yet waits for these lookups, so the
	// lyrics are looked for while the album's page and the cover are fetched.
	// They need the album's name, which only the page has for a song without
	// one.
	var words string
	var wg sync.WaitGroup
	lookUp := func(sg ytmusic.Song) {
		wg.Go(func() {
			// The service guards the finisher's goroutine, not this one: a
			// lookup that panics leaves the song without lyrics instead.
			defer func() {
				if v := recover(); v != nil {
					f.log.Error("recovered", "loop", "lyrics", "panic", v, "stack", string(debug.Stack()))
				}
			}()
			words = f.words(ctx, sg)
		})
	}
	named := s.Album != ""
	if named {
		lookUp(s)
	}
	s = f.complete(ctx, s)
	if !named {
		lookUp(s)
	}
	picture := f.cover(ctx, s.Thumb)
	wg.Wait()
	dst := place(f.music, s)
	replaced = taken(dst) // place only picks a taken name that is the song's own
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, false, err
	}
	t := remux.Tags{Title: s.Title, Artist: s.Artist, Album: s.Album, AlbumArtist: s.AlbumArtist, Track: s.Number,
		Year: s.Year, Lyrics: words, Picture: picture, Extra: [][2]string{{youTubeIDTag, s.VideoID}}}
	if s.SpotifyID != "" {
		t.Extra = append(t.Extra, [2]string{library.SpotifyIDTag, s.SpotifyID})
	}
	if s.ISRC != "" {
		t.Extra = append(t.Extra, [2]string{"ISRC", s.ISRC})
	}
	if err := ctx.Err(); err != nil {
		f.tidy(dst)
		return "", 0, false, err
	}
	if err := f.remux(src, dst, t); err != nil {
		f.tidy(dst)
		return "", 0, false, err
	}
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		f.log.Warn("remove a downloaded WebM", "path", src, "err", err) // the song is filed all the same
	}
	info, err := os.Stat(dst)
	if err != nil {
		return "", 0, false, err
	}
	if err := library.Add(f.db, f.music, dst, f.formats); err != nil {
		f.log.Warn("index a download", "path", dst, "err", err) // the next scan finds it
	}
	return dst, info.Size(), replaced, nil
}

// complete fills in the song's track number, year, album artist and cover
// from its album's page, and makes a song with no album a single.
func (f *finisher) complete(ctx context.Context, s ytmusic.Song) ytmusic.Song {
	if s.AlbumID != "" && s.Number == 0 && f.albums != nil {
		if a, err := f.album(ctx, s.AlbumID); err == nil {
			for i, t := range a.Tracks {
				if t.VideoID == s.VideoID {
					full := a.Song(i)
					s.Number, s.AlbumArtist, s.Thumb = full.Number, full.AlbumArtist, cmp.Or(full.Thumb, s.Thumb)
					break
				}
			}
			s.AlbumArtist = cmp.Or(s.AlbumArtist, a.Artist)
			s.Thumb = cmp.Or(a.Thumb, s.Thumb)
			s.Year = cmp.Or(s.Year, a.Year)
			s.Album = cmp.Or(s.Album, a.Title)
		}
	}
	s.Album = cmp.Or(s.Album, s.Title)
	s.AlbumArtist = cmp.Or(s.AlbumArtist, s.Artist)
	return s
}

// album fetches the album page id, unless it was the last one fetched.
func (f *finisher) album(ctx context.Context, id string) (ytmusic.AlbumPage, error) {
	if id == f.pageID {
		return f.page, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	a, err := f.albums.Album(ctx, id)
	if err == nil {
		f.page, f.pageID = a, id
	}
	return a, err
}

// cover fetches the song's picture to embed, or nil. Google's image server
// sends it square, sized and as a JPEG; anything else is cropped, sized
// and made one here.
func (f *finisher) cover(ctx context.Context, thumb string) *remux.Picture {
	if f.http == nil || thumb == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := netx.Get(ctx, f.http, art.Sized(thumb, coverPx), 4<<20)
	if err != nil {
		f.log.Warn("fetch a cover", "url", thumb, "err", err)
		return nil
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && format == "jpeg" && cfg.Width == cfg.Height && cfg.Width <= coverPx {
		return &remux.Picture{MIME: "image/jpeg", Width: cfg.Width, Height: cfg.Height, Data: data}
	}
	img, err := art.Decode(data)
	if err != nil {
		f.log.Warn("decode a cover", "url", thumb, "err", err)
		return nil
	}
	b := img.Bounds()
	sq := art.Square(img, min(coverPx, b.Dx(), b.Dy()))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sq, &jpeg.Options{Quality: 90}); err != nil {
		return nil
	}
	return &remux.Picture{MIME: "image/jpeg", Width: sq.Rect.Dx(), Height: sq.Rect.Dy(), Data: buf.Bytes()}
}

// words is the song's lyrics from LRCLIB, synced when it has them, or ""
// when it has none or can't be reached; the lyrics service looks again
// when the song plays.
func (f *finisher) words(ctx context.Context, s ytmusic.Song) string {
	if f.lyrics == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := f.lyrics.Get(ctx, lyrics.Track{Title: s.Title, Artist: s.Artist, Album: s.Album, Duration: s.Duration})
	if err != nil {
		return ""
	}
	return cmp.Or(r.Synced, r.Plain)
}

// maxName bounds a file or folder name in bytes, well inside what FAT32 and
// exFAT allow.
const maxName = 120

// safeName makes s a name FAT32 and exFAT take: no \/:*?"<>| or control
// characters, no leading dots (the scanner skips hidden files) or trailing
// dots and spaces, and at most maxName bytes.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|`, r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	n := b.String()
	if len(n) > maxName {
		cut := maxName
		for cut > 0 && !utf8.RuneStart(n[cut]) {
			cut--
		}
		n = n[:cut]
	}
	n = strings.TrimRight(strings.TrimLeft(n, ". "), ". ")
	if n == "" {
		return "Unknown"
	}
	if base, _, _ := strings.Cut(n, "."); reserved(base) {
		n = base + "_" + n[len(base):]
	}
	return n
}

// reserved reports whether s is a name Windows won't make a file of, with or
// without an extension.
func reserved(s string) bool {
	switch u := strings.ToUpper(strings.TrimRight(s, " ")); u {
	case "CON", "PRN", "AUX", "NUL":
		return true
	default:
		return len(u) == 4 && (u[:3] == "COM" || u[:3] == "LPT") && u[3] >= '1' && u[3] <= '9'
	}
}

// youTubeIDTag names the tag holding a download's YouTube video ID.
const youTubeIDTag = "BOOMBRICK_YOUTUBE_ID"

// place is where s is filed: Music/Boombrick/<album artist>/<album>/<NN>
// <title>.opus, numbered "(2)", "(3)" when the name is taken by another
// song. A file of s's own is replaced: a power cut can lose a song's row
// after its file was filed, and the song is downloaded again.
func place(music string, s ytmusic.Song) string {
	dir := filepath.Join(music, "Boombrick", safeName(s.AlbumArtist), safeName(s.Album))
	name := safeName(s.Title)
	if s.Number > 0 {
		name = fmt.Sprintf("%02d %s", s.Number, name)
	}
	p := filepath.Join(dir, name+".opus")
	for i := 2; taken(p) && !filedAs(p, s.VideoID); i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d).opus", name, i))
	}
	return p
}

// filedAs reports whether the file at path is the song videoID, by its tag.
func filedAs(path, videoID string) bool {
	if videoID == "" {
		return false
	}
	id, err := library.ReadTag(path, youTubeIDTag)
	return err == nil && id == videoID
}

// taken reports whether path is in use. A card that can't be asked counts as
// taken, so that nothing is overwritten.
func taken(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// tidy removes the album and artist folders a failed finish made, if they are
// empty; os.Remove leaves the others be.
func (f *finisher) tidy(dst string) {
	dir := filepath.Dir(dst)
	os.Remove(dir)
	os.Remove(filepath.Dir(dir))
}
