// Package importer imports Spotify playlists as downloads: each song is
// matched to a YouTube Music song in the background, and sure matches join
// the download queue, tagged with their Spotify IDs.
package importer

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

// The scores a match must reach to be sure, or worth asking about.
const (
	sureScore   = 0.85
	unsureScore = 0.6
)

// keep is how many candidates a song keeps, for Other matches.
const keep = 5

// penalties are words another version of a song carries in its title: one
// with them is worth half, unless the Spotify song has them too.
// albumPenalties are those an album's name says of its songs, as a live or a
// tribute album; others there name an edition ("Japan Version").
var (
	penalties      = []string{"live", "cover", "remix", "sped up", "slowed", "karaoke", "instrumental", "version", "tribute", "8d"}
	albumPenalties = []string{"live", "cover", "karaoke", "tribute"}
)

var (
	// featuring is "(feat. X)", "[ft X]" or "(with X)" in a title.
	featuring = regexp.MustCompile(`[(\[](feat\.?|ft\.?|featuring|with) [^)\]]*[)\]]`)
	// remastered is " - Remastered 2011", or "(2011 Remaster)", in a title.
	remastered = regexp.MustCompile(` - [^-]*remaster[^-]*$|[(\[][^)\]]*remaster[^)\]]*[)\]]`)
)

// State is how far a song of an import has got.
type State uint8

const (
	Waiting  State = iota // to be matched
	Have                  // the library or the download queue has it already
	Matched               // matched surely, and queued
	Unsure                // matched, not surely: it waits for review
	NotFound              // nothing like it on YouTube Music
	Accepted              // an unsure match accepted, and queued
	Skipped               // left out
)

// Candidate is a YouTube Music song a Spotify song may be, and how likely.
type Candidate struct {
	Song  ytmusic.Song
	Score float64
}

// score is how surely YouTube Music song it is Spotify song t, from 0 to 1:
// half for the length, three tenths for the title and a fifth for the
// artist, halved for a word of another version.
func score(t spotify.Track, it ytmusic.Item) float64 {
	s := 0.5*lengthScore(time.Duration(t.Duration)*time.Millisecond, it.Duration) +
		0.3*titleScore(t.Title, it.Title) + 0.2*artistScore(t.Artists, it.Artist)
	if otherVersion(t, it) {
		s /= 2
	}
	return s
}

// lengthScore is whole within 3 s, nothing 15 s off and more, and in between
// in proportion. An unknown length counts half.
func lengthScore(a, b time.Duration) float64 {
	if a <= 0 || b <= 0 {
		return 0.5
	}
	d := (a - b).Abs()
	switch {
	case d <= 3*time.Second:
		return 1
	case d >= 15*time.Second:
		return 0
	}
	return float64(15*time.Second-d) / float64(12*time.Second)
}

// words is s in lower case, without who features on it or that it was
// remastered, split into words.
func words(s string) []string {
	s = strings.ToLower(s)
	s = featuring.ReplaceAllString(s, " ")
	s = remastered.ReplaceAllString(s, " ")
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// titleScore is the share of their words two titles have in common.
func titleScore(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	left := slices.Clone(wb)
	common := 0
	for _, w := range wa {
		if i := slices.Index(left, w); i >= 0 {
			common++
			left = slices.Delete(left, i, i+1)
		}
	}
	return 2 * float64(common) / float64(len(wa)+len(wb))
}

// artistScore is whole when one of the Spotify song's artists is among the
// candidate's, nothing otherwise.
func artistScore(artists []string, artist string) float64 {
	cand := " " + strings.Join(words(artist), " ") + " "
	for _, a := range artists {
		if w := strings.Join(words(a), " "); w != "" && strings.Contains(cand, " "+w+" ") {
			return 1
		}
	}
	return 0
}

// otherVersion reports whether the candidate says it is another version of
// the song (live, a remix…) where the Spotify song doesn't: in its title, or
// for the words albums use so, in its album's name.
func otherVersion(t spotify.Track, it ytmusic.Item) bool {
	spot := " " + strings.Join(words(t.Title+" "+t.Album), " ") + " "
	says := func(name string, ws []string) bool {
		cand := " " + strings.Join(words(name), " ") + " "
		for _, p := range ws {
			if w := " " + p + " "; strings.Contains(cand, w) && !strings.Contains(spot, w) {
				return true
			}
		}
		return false
	}
	return says(it.Title, penalties) || says(it.Album, albumPenalties)
}

// rank scores the songs among items as t, best first, adding them to
// have's, the best keep of all.
func rank(t spotify.Track, have []Candidate, items []ytmusic.Item) []Candidate {
	out := slices.Clone(have)
	for _, it := range items {
		if it.Kind != ytmusic.SongItem || it.ID == "" ||
			slices.ContainsFunc(out, func(c Candidate) bool { return c.Song.VideoID == it.ID }) {
			continue
		}
		out = append(out, Candidate{Song: it.Song(), Score: score(t, it)})
	}
	slices.SortStableFunc(out, func(a, b Candidate) int { return cmp.Compare(b.Score, a.Score) })
	return out[:min(len(out), keep)]
}

// classify is the state a song's best score puts it in.
func classify(best float64) State {
	switch {
	case best >= sureScore:
		return Matched
	case best >= unsureScore:
		return Unsure
	}
	return NotFound
}
