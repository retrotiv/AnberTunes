package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ujjwal313/boombrick/internal/netx"
)

// Track is what finding a track's lyrics needs: a file, or a Spotify song
// played as a stream, which has no path.
type Track struct {
	Path                 string
	SpotifyID            string // names a streamed song, which has no Path
	Title, Artist, Album string
	Duration             time.Duration // 0 when unknown
}

// Key names t for the service's answers and the cache: its file's path, or
// "spotify:" and the song's ID when it has none. It is empty for a track that
// is neither, which has no lyrics to look for.
func (t Track) Key() string {
	switch {
	case t.Path != "":
		return t.Path
	case t.SpotifyID != "":
		return "spotify:" + t.SpotifyID
	}
	return ""
}

// Result is what LRCLIB has for a track.
type Result struct {
	Synced, Plain string
	Instrumental  bool
}

// ErrNotFound means LRCLIB has nothing for the track.
var ErrNotFound = errors.New("lyrics: none found online")

// Client asks LRCLIB for lyrics.
type Client struct {
	HTTP *http.Client
	Base string
}

// NewClient returns a client for lrclib.net.
func NewClient(c *http.Client) *Client { return &Client{HTTP: c, Base: "https://lrclib.net"} }

type record struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	Plain        string  `json:"plainLyrics"`
	Synced       string  `json:"syncedLyrics"`
}

func (r record) result() Result {
	return Result{Synced: r.Synced, Plain: r.Plain, Instrumental: r.Instrumental}
}

func (r record) empty() bool { return r.Synced == "" && r.Plain == "" && !r.Instrumental }

// sameTitle and sameArtist report whether r is titled or credited as t,
// ignoring case and surrounding spaces. LRCLIB's search is fuzzy: without a
// length, both are all that tells the track from a remix or another song;
// with one, either tells it from another song of the same length.
func (r record) sameTitle(t Track) bool {
	return strings.EqualFold(strings.TrimSpace(r.TrackName), strings.TrimSpace(t.Title))
}

func (r record) sameArtist(t Track) bool {
	return strings.EqualFold(strings.TrimSpace(r.ArtistName), strings.TrimSpace(t.Artist))
}

// Get finds t's lyrics: LRCLIB's exact match for its title, artist, album and
// length when the length is known, then a search by title and artist that
// takes a result within 2 s of the length and of the same title or artist,
// or without a length one of the same title and artist, synced lyrics first.
func (c *Client) Get(ctx context.Context, t Track) (Result, error) {
	if t.Duration > 0 {
		q := url.Values{"track_name": {t.Title}, "artist_name": {t.Artist}, "album_name": {t.Album},
			"duration": {strconv.Itoa(int(t.Duration.Round(time.Second) / time.Second))}}
		body, err := netx.Get(ctx, c.HTTP, c.Base+"/api/get?"+q.Encode(), 1<<20)
		if err == nil {
			var r record
			if err := json.Unmarshal(body, &r); err != nil {
				return Result{}, err
			}
			if !r.empty() {
				return r.result(), nil
			}
		} else if !netx.IsNotFound(err) {
			return Result{}, err
		}
	}
	q := url.Values{"track_name": {t.Title}, "artist_name": {t.Artist}}
	body, err := netx.Get(ctx, c.HTTP, c.Base+"/api/search?"+q.Encode(), 2<<20)
	if err != nil {
		return Result{}, err
	}
	var rs []record
	if err := json.Unmarshal(body, &rs); err != nil {
		return Result{}, err
	}
	best := -1
	for i, r := range rs {
		if t.Duration > 0 && (math.Abs(r.Duration-t.Duration.Seconds()) > 2 || !r.sameTitle(t) && !r.sameArtist(t)) {
			continue
		}
		if t.Duration <= 0 && !(r.sameTitle(t) && r.sameArtist(t)) {
			continue
		}
		if r.empty() {
			continue
		}
		if best < 0 || r.Synced != "" && rs[best].Synced == "" {
			best = i
		}
	}
	if best < 0 {
		return Result{}, ErrNotFound
	}
	return rs[best].result(), nil
}
