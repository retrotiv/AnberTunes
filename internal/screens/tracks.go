package screens

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/lyrics"
	"github.com/ujjwal313/boombrick/internal/player"
)

// Tracks converts library tracks to queue entries.
func Tracks(ts []library.Track) []player.Track {
	out := make([]player.Track, len(ts))
	for i := range ts {
		out[i] = TrackOf(&ts[i])
	}
	return out
}

// TrackOf converts one library track to a queue entry.
func TrackOf(t *library.Track) player.Track {
	albumArtist := t.AlbumArtist
	if albumArtist == "" {
		albumArtist = t.Artist
	}
	return player.Track{Path: t.Path, Title: t.Title, Artist: t.Artist, AlbumArtist: albumArtist, Album: t.Album,
		Gain: t.Gain, Peak: t.Peak, HasGain: t.HasGain,
		AlbumGain: t.AlbumGain, AlbumPeak: t.AlbumPeak, HasAlbumGain: t.HasAlbumGain, Duration: t.Duration}
}

// Clock formats d as minutes and seconds: 3:42.
func Clock(d time.Duration) string {
	s := int(d / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ClockText is a Clock shown every frame, formatted again only when the
// second it shows changes. The zero ClockText is ready to use.
type ClockText struct {
	sec  int
	text string
}

// Of is Clock(d).
func (c *ClockText) Of(d time.Duration) string {
	if s := int(d / time.Second); s != c.sec || c.text == "" {
		c.sec, c.text = s, Clock(d)
	}
	return c.text
}

// SongsAndTime describes a list of n songs lasting d: "12 songs · 46 min",
// "40 songs · 2 h 5 min", or just the count when the length is unknown.
func SongsAndTime(n int, d time.Duration) string {
	s := Count(n) + " songs"
	if n == 1 {
		s = "1 song"
	}
	m := int(d / time.Minute)
	switch {
	case d <= 0:
		return s
	case m < 60:
		return fmt.Sprintf("%s · %d min", s, max(m, 1))
	}
	return fmt.Sprintf("%s · %d h %d min", s, m/60, m%60)
}

// Count formats n with thousands separators: 2,431.
func Count(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// LyricsTrack is what finding a queued track's lyrics needs.
func LyricsTrack(t player.Track) lyrics.Track {
	return lyrics.Track{Path: t.Path, SpotifyID: t.SpotifyID, Title: t.Title, Artist: t.Artist, Album: t.Album, Duration: t.Duration}
}

// PlayingLyricsTrack is LyricsTrack for the track st is on, with the length
// the decoder found when the library has none.
func PlayingLyricsTrack(st player.State) lyrics.Track {
	t := LyricsTrack(st.Track)
	if t.Duration <= 0 {
		t.Duration = st.Duration
	}
	return t
}
