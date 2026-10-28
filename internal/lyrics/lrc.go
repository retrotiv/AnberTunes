// Package lyrics finds tracks' lyrics and times their lines.
package lyrics

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Word is part of a line with a start of its own, from enhanced LRC's
// <mm:ss.xx> marks. A mark with no text after it ends the word before.
type Word struct {
	Start time.Duration
	Text  string
}

// Line is one line of lyrics. Start is 0 in plain lyrics.
type Line struct {
	Start time.Duration
	Text  string
	Words []Word // nil without word timings
}

// Lyrics are a track's lines, Synced when they carry timings.
type Lyrics struct {
	Lines  []Line
	Synced bool
}

var (
	stamp  = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	tagRe  = regexp.MustCompile(`^\[([a-zA-Z#]+):(.*)\]$`)
	wordAt = regexp.MustCompile(`<(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?>`)
)

// Parse reads lyrics. In LRC, lines lead with [mm:ss.xx] stamps; several
// stamps repeat a line; an [offset:ms] tag moves every line (a positive
// offset shows them sooner); enhanced LRC's <mm:ss.xx> marks time words; other
// tags ([ar:], [ti:], ...) are skipped. Text without stamps is plain lyrics, a
// line for each line, without the blank lines at its ends; so are lines that
// all carry the same stamp.
func Parse(text string) Lyrics {
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	var timed []Line
	var plain []string
	var offset time.Duration
	for _, raw := range strings.Split(text, "\n") {
		s := strings.TrimSpace(raw)
		var at []time.Duration
		for {
			m := stamp.FindStringSubmatch(s)
			if m == nil {
				break
			}
			at = append(at, clock(m[1], m[2], m[3]))
			s = s[len(m[0]):]
		}
		if len(at) == 0 {
			if m := tagRe.FindStringSubmatch(s); m != nil {
				if strings.EqualFold(m[1], "offset") {
					if n, err := strconv.Atoi(strings.TrimSpace(m[2])); err == nil {
						offset = time.Duration(n) * time.Millisecond
					}
				}
				continue
			}
			plain = append(plain, s)
			continue
		}
		words, line := wordsOf(strings.TrimSpace(s), at[0])
		if len(at) > 1 {
			words = nil // word times belong to one of the repeats only
		}
		for _, start := range at {
			timed = append(timed, Line{Start: start, Text: line, Words: words})
		}
	}
	if len(timed) > 1 && !slices.ContainsFunc(timed, func(l Line) bool { return l.Start != timed[0].Start }) {
		// Every line stamped alike, as some files are with [00:00.00]: the
		// stamps tell nothing, so the lines are plain lyrics.
		plain = plain[:0]
		for _, l := range timed {
			plain = append(plain, l.Text)
		}
		timed = nil
	}
	if len(timed) == 0 {
		start, end := 0, len(plain)
		for start < end && plain[start] == "" {
			start++
		}
		for end > start && plain[end-1] == "" {
			end--
		}
		l := Lyrics{}
		for _, s := range plain[start:end] {
			l.Lines = append(l.Lines, Line{Text: s})
		}
		return l
	}
	for i := range timed {
		timed[i].Start = max(timed[i].Start-offset, 0)
		if timed[i].Words != nil {
			ws := make([]Word, len(timed[i].Words))
			for j, w := range timed[i].Words {
				ws[j] = Word{Start: max(w.Start-offset, 0), Text: w.Text}
			}
			timed[i].Words = ws
		}
	}
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].Start < timed[j].Start })
	return Lyrics{Lines: timed, Synced: true}
}

// clock reads minutes, seconds and an optional fraction: tenths, hundredths
// or thousandths by its length.
func clock(m, s, frac string) time.Duration {
	mi, _ := strconv.Atoi(m)
	se, _ := strconv.Atoi(s)
	d := time.Duration(mi)*time.Minute + time.Duration(se)*time.Second
	if frac != "" {
		f, _ := strconv.Atoi(frac)
		d += time.Duration(f) * [4]time.Duration{0, 100, 10, 1}[len(frac)] * time.Millisecond
	}
	return d
}

// wordsOf splits enhanced LRC's word marks out of a line, returning the
// timed words and the line's text without them. Text before the first mark
// is a word starting with the line; the words are in time order and their
// characters are those of the trimmed text.
func wordsOf(s string, start time.Duration) ([]Word, string) {
	locs := wordAt.FindAllStringSubmatchIndex(s, -1)
	if locs == nil {
		return nil, s
	}
	part := func(a, b int) string {
		if a < 0 {
			return ""
		}
		return s[a:b]
	}
	var words []Word
	if lead := s[:locs[0][0]]; lead != "" {
		words = append(words, Word{Start: start, Text: lead})
	}
	for i, loc := range locs {
		end := len(s)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		w := s[loc[1]:end]
		words = append(words, Word{Start: clock(part(loc[2], loc[3]), part(loc[4], loc[5]), part(loc[6], loc[7])), Text: w})
	}
	sort.SliceStable(words, func(i, j int) bool { return words[i].Start < words[j].Start })
	words[0].Text = strings.TrimLeft(words[0].Text, " \t")
	for i := len(words) - 1; i >= 0; i-- {
		if t := strings.TrimRight(words[i].Text, " \t"); t != "" {
			words[i].Text = t
			break
		}
	}
	var text strings.Builder
	for _, w := range words {
		text.WriteString(w.Text)
	}
	return words, strings.TrimSpace(text.String())
}

// A line without word timings is taken to be sung until the next line
// starts, but over no more than maxLine; the last line over lastLine.
const (
	maxLine  = 10 * time.Second
	lastLine = 5 * time.Second
)

// At finds the line being sung at t, -1 before the first and in plain lyrics,
// and how much of it has been sung, as a share of its characters from 0 to
// 1: by its words' timings when it has them, otherwise evenly over the time
// it is sung for.
func (l Lyrics) At(t time.Duration) (line int, sung float64) {
	if !l.Synced {
		return -1, 0
	}
	i := sort.Search(len(l.Lines), func(i int) bool { return l.Lines[i].Start > t }) - 1
	if i < 0 {
		return -1, 0
	}
	ln := l.Lines[i]
	end := ln.Start + lastLine
	if i+1 < len(l.Lines) {
		end = min(l.Lines[i+1].Start, ln.Start+maxLine)
	}
	if len(ln.Words) > 0 {
		return i, wordsSung(ln.Words, t, end)
	}
	if end <= ln.Start {
		return i, 1
	}
	return i, min(float64(t-ln.Start)/float64(end-ln.Start), 1)
}

// wordsSung is the share of the words' characters sung at t; each word is sung
// from its start until the next one's, the last until end.
func wordsSung(ws []Word, t, end time.Duration) float64 {
	total := 0
	for _, w := range ws {
		total += utf8.RuneCountInString(w.Text)
	}
	if total == 0 {
		return 1
	}
	done := 0.0
	for k, w := range ws {
		if t < w.Start {
			break
		}
		n := float64(utf8.RuneCountInString(w.Text))
		stop := end
		if k+1 < len(ws) {
			stop = ws[k+1].Start
		}
		if t >= stop || stop <= w.Start {
			done += n
			continue
		}
		done += n * float64(t-w.Start) / float64(stop-w.Start)
		break
	}
	return done / float64(total)
}
