package lyrics

import (
	"math"
	"slices"
	"testing"
	"time"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func starts(l Lyrics) []time.Duration {
	var s []time.Duration
	for _, ln := range l.Lines {
		s = append(s, ln.Start)
	}
	return s
}

func texts(l Lyrics) []string {
	var s []string
	for _, ln := range l.Lines {
		s = append(s, ln.Text)
	}
	return s
}

func TestParseStamps(t *testing.T) {
	l := Parse("[00:12.00]Line one\n[00:15.50]Line two\n[1:02.5]Tenths\n[01:02.123]Millis\n[01:03]Whole\n[01:04:25]Colon")
	if !l.Synced {
		t.Fatal("not synced")
	}
	want := []time.Duration{ms(12000), ms(15500), ms(62500), ms(62123), ms(63000), ms(64250)}
	if got := starts(l); !slices.Equal(got, []time.Duration{want[0], want[1], want[3], want[2], want[4], want[5]}) {
		t.Fatalf("starts %v", got)
	}
	if l.Lines[0].Text != "Line one" {
		t.Fatalf("text %q", l.Lines[0].Text)
	}
}

func TestParseRepeatsAndSorts(t *testing.T) {
	l := Parse("[00:10.00][00:30.00]Chorus\n[00:20.00]Verse")
	if got := texts(l); !slices.Equal(got, []string{"Chorus", "Verse", "Chorus"}) {
		t.Fatalf("lines %v", got)
	}
	if got := starts(l); !slices.Equal(got, []time.Duration{ms(10000), ms(20000), ms(30000)}) {
		t.Fatalf("starts %v", got)
	}
}

func TestParseOffsetAndTags(t *testing.T) {
	l := Parse("[ar:Odalys]\n[ti:Rust]\n[offset:+500]\n[00:10.00]A\n[00:00.20]B")
	if got := starts(l); !slices.Equal(got, []time.Duration{0, ms(9500)}) {
		t.Fatalf("starts with +500 ms offset: %v", got)
	}
	if got := texts(l); !slices.Equal(got, []string{"B", "A"}) {
		t.Fatalf("lines %v; tags must not become lines", got)
	}
	if got := starts(Parse("[offset:-500]\n[00:10.00]A")); got[0] != ms(10500) {
		t.Fatalf("start with -500 ms offset: %v", got)
	}
}

func TestParseWords(t *testing.T) {
	l := Parse("[00:10.00]<00:10.00>Hello <00:10.50>world<00:11.00>")
	ln := l.Lines[0]
	if ln.Text != "Hello world" || len(ln.Words) != 3 {
		t.Fatalf("line %+v", ln)
	}
	if ln.Words[0] != (Word{ms(10000), "Hello "}) || ln.Words[1] != (Word{ms(10500), "world"}) || ln.Words[2] != (Word{ms(11000), ""}) {
		t.Fatalf("words %+v", ln.Words)
	}
}

func TestParsePlain(t *testing.T) {
	l := Parse("\ufeff\r\n\r\nFirst\r\n\r\nSecond  \r\n\r\n")
	if l.Synced || !slices.Equal(texts(l), []string{"First", "", "Second"}) {
		t.Fatalf("plain lyrics %+v", l)
	}
	if got := Parse("[00:40.00]"); len(got.Lines) != 1 || got.Lines[0].Text != "" {
		t.Fatalf("a stamp with no words: %+v", got)
	}
	if got := Parse(""); len(got.Lines) != 0 {
		t.Fatalf("empty text: %+v", got)
	}
}

// Some files stamp every line alike, most often [00:00.00]: they are plain
// lyrics, in the file's order.
func TestParseOneStampForEveryLine(t *testing.T) {
	l := Parse("[ti:Rust]\n[00:00.00]First\n[00:00.00]Second\n[00:00.00]\n[00:00.00]Third\n[00:00.00]")
	if l.Synced || !slices.Equal(texts(l), []string{"First", "Second", "", "Third"}) {
		t.Fatalf("lines %+v", l)
	}
	if l.Lines[0].Start != 0 || l.Lines[0].Words != nil {
		t.Fatalf("a plain line keeps a time: %+v", l.Lines[0])
	}
	if one := Parse("[00:10.00]Only"); !one.Synced {
		t.Fatal("a single stamped line is synced")
	}
}

func TestAt(t *testing.T) {
	l := Parse("[00:10.00]Ten\n[00:14.00]Fourteen\n[01:00.00]Sixty\n[01:05.00]<01:05.00>Hello <01:05.50>world<01:06.00>")
	for _, c := range []struct {
		at   time.Duration
		line int
		sung float64
	}{
		{ms(5000), -1, 0},
		{ms(10000), 0, 0},
		{ms(12000), 0, 0.5},
		{ms(19000), 1, 0.5}, // the next line is 46 s away: the fill takes 10 s
		{ms(30000), 1, 1},
		{ms(65250), 3, 3.0 / 11},
		{ms(65750), 3, 8.5 / 11},
		{ms(66500), 3, 1},
	} {
		line, sung := l.At(c.at)
		if line != c.line || math.Abs(sung-c.sung) > 1e-9 {
			t.Errorf("At(%v) = %d, %v; want %d, %v", c.at, line, sung, c.line, c.sung)
		}
	}
	last := Parse("[00:10.00]Only")
	if line, sung := last.At(ms(12500)); line != 0 || math.Abs(sung-0.5) > 1e-9 {
		t.Errorf("the last line is filled over 5 s: At(12.5s) = %d, %v", line, sung)
	}
	if line, _ := Parse("Plain").At(ms(1000)); line != -1 {
		t.Error("plain lyrics have a current line")
	}
}

func TestParseLeadingText(t *testing.T) {
	l := Parse("[00:10.00]Hello <00:10.50>world<00:11.00>")
	ln := l.Lines[0]
	want := []Word{{ms(10000), "Hello "}, {ms(10500), "world"}, {ms(11000), ""}}
	if !slices.Equal(ln.Words, want) {
		t.Fatalf("words %+v", ln.Words)
	}
	if _, sung := l.At(ms(10250)); math.Abs(sung-3.0/11) > 1e-9 {
		t.Errorf("At(10.25s) sung %v, want 3/11", sung)
	}
}

func TestParseWordsOutOfOrder(t *testing.T) {
	ln := Parse("[00:10.00]<00:10.50>world<00:10.00>Hello ").Lines[0]
	want := []Word{{ms(10000), "Hello"}, {ms(10500), "world"}}
	if !slices.Equal(ln.Words, want) {
		t.Fatalf("words %+v", ln.Words)
	}
}

func TestParseWordsTrimmed(t *testing.T) {
	ln := Parse("[00:10.00]<00:10.00> Hello <00:10.50>world <00:11.00>").Lines[0]
	if ln.Text != "Hello world" {
		t.Fatalf("text %q", ln.Text)
	}
	n := 0
	for _, w := range ln.Words {
		n += len([]rune(w.Text))
	}
	if n != 11 {
		t.Fatalf("words %+v have %d characters, want 11", ln.Words, n)
	}
}
