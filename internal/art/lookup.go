package art

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/ujjwal313/boombrick/internal/netx"
)

// ErrNotFound means no release group matched the album closely enough, or
// none that did has a front cover.
var ErrNotFound = errors.New("art: no cover found online")

// mbGap is the space kept between MusicBrainz requests: it allows one a
// second from each client.
const mbGap = 1100 * time.Millisecond

// maxCoverBytes bounds a downloaded cover; 500 px fronts are far smaller.
const maxCoverBytes = 8 << 20

// Lookup finds album covers online: a MusicBrainz search for the album's
// release group, then the group's front cover from the Cover Art Archive.
type Lookup struct {
	Client      *http.Client
	MusicBrainz string // base URL
	CoverArt    string // base URL

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu   sync.Mutex
	next time.Time // when MusicBrainz may next be asked
}

// NewLookup returns a Lookup against the public services.
func NewLookup(c *http.Client) *Lookup {
	return &Lookup{Client: c, MusicBrainz: "https://musicbrainz.org", CoverArt: "https://coverartarchive.org",
		now: time.Now, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Find returns the front cover of album by artist, as image bytes, and the
// release group it belongs to.
func (l *Lookup) Find(ctx context.Context, artist, album string) ([]byte, string, error) {
	if strings.TrimSpace(artist) == "" || strings.TrimSpace(album) == "" {
		return nil, "", ErrNotFound
	}
	ids, err := l.search(ctx, artist, album)
	if err != nil {
		return nil, "", err
	}
	for _, id := range ids {
		data, err := netx.Get(ctx, l.Client, l.CoverArt+"/release-group/"+id+"/front-500", maxCoverBytes)
		if netx.IsNotFound(err) {
			continue // a group without a front cover
		}
		if err != nil {
			return nil, "", err
		}
		return data, id, nil
	}
	return nil, "", ErrNotFound
}

type releaseGroups struct {
	Groups []struct {
		ID        string   `json:"id"`
		Score     int      `json:"score"`
		Title     string   `json:"title"`
		Primary   string   `json:"primary-type"`
		Secondary []string `json:"secondary-types"`
		Credits   []struct {
			Name string `json:"name"`
			Join string `json:"joinphrase"`
		} `json:"artist-credit"`
	} `json:"release-groups"`
}

// rank orders the kinds of release group by how likely their cover is the
// album's: an album, then one that is also a compilation, live or a
// soundtrack, then an EP, then anything else. A single of the same name
// comes last; its cover is rarely the album's.
func rank(primary string, secondary []string) int {
	switch {
	case primary == "Album" && len(secondary) == 0:
		return 0
	case primary == "Album":
		return 1
	case primary == "EP":
		return 2
	case primary == "Single":
		return 4
	}
	return 3
}

// search returns up to three release groups that match album by artist, best
// first, all of the best kind there is (see rank). When two of that kind
// match equally well, as the albums of a self-titled series do, it returns
// none rather than guess.
func (l *Lookup) search(ctx context.Context, artist, album string) ([]string, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	q := `releasegroup:"` + quote(album) + `" AND artist:"` + quote(artist) + `"`
	u := l.MusicBrainz + "/ws/2/release-group/?" + url.Values{"query": {q}, "fmt": {"json"}, "limit": {"5"}}.Encode()
	body, err := netx.Get(ctx, l.Client, u, 1<<20)
	if err != nil {
		return nil, err
	}
	var r releaseGroups
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	type match struct {
		id          string
		rank, score int
	}
	var ms []match
	for _, g := range r.Groups {
		var credit strings.Builder
		for _, c := range g.Credits {
			credit.WriteString(c.Name + c.Join)
		}
		if g.Score >= 90 && sameName(g.Title, album, true) && sameName(credit.String(), artist, false) {
			ms = append(ms, match{g.ID, rank(g.Primary, g.Secondary), g.Score})
		}
	}
	if len(ms) == 0 {
		return nil, nil
	}
	slices.SortStableFunc(ms, func(a, b match) int { return cmp.Or(a.rank-b.rank, b.score-a.score) })
	if len(ms) > 1 && ms[1].rank == ms[0].rank && ms[1].score == ms[0].score {
		return nil, nil
	}
	var ids []string
	for _, m := range ms {
		if m.rank != ms[0].rank || len(ids) == 3 {
			break
		}
		ids = append(ids, m.id)
	}
	return ids, nil
}

// wait holds the caller until MusicBrainz may be asked again.
func (l *Lookup) wait(ctx context.Context) error {
	l.mu.Lock()
	now := l.now()
	at := now
	if l.next.After(now) {
		at = l.next
	}
	l.next = at.Add(mbGap)
	l.mu.Unlock()
	return l.sleep(ctx, at.Sub(now))
}

// quote escapes s for a quoted term in a MusicBrainz (Lucene) query.
func quote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// edition matches what releases add to a title: "(Remastered 2009)",
// "[Deluxe Edition]".
var edition = regexp.MustCompile(`\s*[(\[][^)\]]*[)\]]\s*$`)

// sameName compares names as people would: ignoring case, accents,
// punctuation and spacing, "&" for "and", and a leading "The". A name made
// only of symbols ("÷", "!!!") is compared as written instead. Titles also
// match when exactly one side has a bracketed edition at the end and the
// rest is the same; with brackets on both sides only an exact match counts,
// so "(Vol. 1)" and "(Vol. 2)" stay apart.
func sameName(a, b string, title bool) bool {
	na, nb := normName(a), normName(b)
	if na == "" || nb == "" {
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
		return a != "" && strings.EqualFold(a, b)
	}
	if na == nb {
		return true
	}
	if !title {
		return false
	}
	ea, eb := edition.ReplaceAllString(a, ""), edition.ReplaceAllString(b, "")
	if (ea != a) == (eb != b) {
		return false
	}
	na, nb = normName(ea), normName(eb)
	return na != "" && na == nb
}

func normName(s string) string {
	var plain strings.Builder
	for _, r := range norm.NFKD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			plain.WriteRune(unicode.ToLower(r))
		}
	}
	words := strings.FieldsFunc(strings.ReplaceAll(plain.String(), "&", " and "), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '/'
	})
	if len(words) > 1 && words[0] == "the" {
		words = words[1:]
	}
	return strings.ReplaceAll(strings.Join(words, ""), "/", "")
}
