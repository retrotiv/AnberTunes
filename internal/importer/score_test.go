package importer

import (
	"slices"
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/ytmusic"
)

func song(id, artist, title, album string, d time.Duration) ytmusic.Item {
	return ytmusic.Item{Kind: ytmusic.SongItem, ID: id, Title: title, Artist: artist, Album: album, Duration: d}
}

func dur(m, s int) time.Duration { return time.Duration(m)*time.Minute + time.Duration(s)*time.Second }

var (
	sapphire = spotify.Track{ID: "4Q0qVhFQa7j6jRKzo3HDmP", Title: "Sapphire", Artists: []string{"Ed Sheeran"},
		Album: "Sapphire", Duration: 179082, ISRC: "GBAHS2500262"}
	goingBad = spotify.Track{ID: "2IRZnDFmlqMuOrYOLnZZyc", Title: "Going Bad (feat. Drake)",
		Artists: []string{"Meek Mill", "Drake"}, Album: "Championships", Duration: 180522, ISRC: "USAT21812710"}
	oRePiya = spotify.Track{ID: "7cWnks0lsRtpAi87COOiXK", Title: "O Re Piya",
		Artists: []string{"Salim–Sulaiman", "Rahat Fateh Ali Khan", "Jaideep Sahni"}, Album: "Aaja Nachle", Duration: 379768}
	tereyPapa = spotify.Track{ID: "7vIXP7osY1jcyIpX9hCeLt", Title: "Terey papa", Artists: []string{"OG Lucifer"},
		Album: "Naala Paar", Duration: 184639, ISRC: "ES71G2502682"}
	welcome = spotify.Track{ID: "1fkKe0w5hdkdBnZcuMwaMy", Title: "Welcome 2 Detroit", Artists: []string{"Trick Trick"},
		Duration: 285000, ISRC: "USUM70505628"}
	stan = spotify.Track{ID: "3UmaczJpikHgJFyBTAJVoz", Title: "Stan", Artists: []string{"Eminem", "Dido"},
		Album: "The Marshall Mathers LP", Duration: 404000}
	bohemian = spotify.Track{ID: "7tFiyTwD0nx5a1eklYtX2J", Title: "Bohemian Rhapsody - Remastered 2011", Artists: []string{"Queen"},
		Album: "A Night At The Opera (2011 Remaster)", Duration: 354000}
	loveTheWay = spotify.Track{ID: "0DQOJiE6cWNnNcuZvOxiGg", Title: "Love The Way You Lie (Part II) - Pt. 2",
		Artists: []string{"Rihanna", "Eminem"}, Album: "Loud", Duration: 296000}
)

// Real searches: what YouTube Music answered for these songs' ISRCs and for
// their artists and titles.
func TestScoring(t *testing.T) {
	for _, c := range []struct {
		t    spotify.Track
		it   ytmusic.Item
		want State
	}{
		{sapphire, song("a", "Ed Sheeran", "Sapphire", "Sapphire", dur(3, 0)), Matched},
		{sapphire, song("b", "Ed Sheeran", "Sapphire [Remix] (feat. Arijit Singh)", "Sapphire", dur(3, 1)), NotFound},
		{sapphire, song("c", "Pianofy", "Sapphire", "A Piano Tribute to Ed Sheeran", dur(2, 53)), NotFound},
		{sapphire, song("d", "Chloe Elodie", "Sapphire (French Version)", "Famous Pop Covers in French", dur(2, 58)), NotFound},
		{sapphire, song("e", "BBpanzu", "Bang Bang Bang", "", dur(3, 1)), NotFound}, // what its ISRC found
		{goingBad, song("f", "Meek Mill", "Going Bad (feat. Drake)", "Championships", dur(3, 1)), Matched},
		{goingBad, song("g", "Drake", "Headlines", "Take Care (Deluxe)", dur(3, 56)), NotFound},
		{oRePiya, song("h", "Rahat Fateh Ali Khan", "O Re Piya", "", dur(6, 20)), Matched},
		{tereyPapa, song("i", "OG Lucifer & Calm", "Terey papa", "", dur(3, 5)), Matched},
		{welcome, song("j", "Trick Trick", "Welcome 2 Detroit (feat. Eminem)", "", dur(4, 46)), Matched},
		{stan, song("k", "Eminem", "Stan (Live)", "", dur(6, 50)), NotFound},
		{stan, song("l", "Eminem", "Stan", "The Marshall Mathers LP", dur(6, 52)), Unsure}, // 8 s longer
		{bohemian, song("m", "Queen", "Bohemian Rhapsody", "A Night at the Opera", dur(5, 55)), Matched},
		// An edition's album says Version without the song being another one.
		{loveTheWay, song("o", "Rihanna", "Love The Way You Lie (Part II) (Pt. 2) (feat. Eminem)", "Loud (Japan Version)", dur(4, 57)), Matched},
	} {
		s := score(c.t, c.it)
		if got := classify(s); got != c.want {
			t.Errorf("%s as %s - %s: score %.2f, %v; want %v", c.t.Title, c.it.Artist, c.it.Title, s, got, c.want)
		}
	}
	// A live song isn't marked down for a live candidate.
	live := spotify.Track{Title: "Stan - Live", Artists: []string{"Eminem"}, Album: "Live from Wembley", Duration: 410000}
	if s := score(live, song("n", "Eminem", "Stan (Live)", "", dur(6, 50))); s < sureScore {
		t.Errorf("a live song as its live version: %.2f", s)
	}
}

func TestWords(t *testing.T) {
	for in, want := range map[string][]string{
		"Going Bad (feat. Drake)":               {"going", "bad"},
		"Bohemian Rhapsody - Remastered 2011":   {"bohemian", "rhapsody"},
		"Welcome 2 Detroit [ft Eminem]":         {"welcome", "2", "detroit"},
		"Salim–Sulaiman":                        {"salim", "sulaiman"},
		"Sapphire [Remix] (feat. Arijit Singh)": {"sapphire", "remix"},
	} {
		if got := words(in); !slices.Equal(got, want) {
			t.Errorf("words(%q) = %q, want %q", in, got, want)
		}
	}
}

// Ranking keeps the best five songs once each, best first.
func TestRank(t *testing.T) {
	items := []ytmusic.Item{
		song("b", "Ed Sheeran", "Sapphire [Remix] (feat. Arijit Singh)", "Sapphire", dur(3, 1)),
		{Kind: ytmusic.AlbumItem, ID: "MPRE", Title: "Sapphire"},
		song("a", "Ed Sheeran", "Sapphire", "Sapphire", dur(3, 0)),
		song("c", "Pianofy", "Sapphire", "A Piano Tribute to Ed Sheeran", dur(2, 53)),
	}
	got := rank(sapphire, nil, items)
	if len(got) != 3 || got[0].Song.VideoID != "a" {
		t.Fatalf("ranked %+v", got)
	}
	more := []ytmusic.Item{song("a", "Ed Sheeran", "Sapphire", "Sapphire", dur(3, 0)),
		song("x1", "X", "One", "", 0), song("x2", "X", "Two", "", 0), song("x3", "X", "Three", "", 0)}
	if got := rank(sapphire, got, more); len(got) != keep || got[0].Song.VideoID != "a" {
		t.Fatalf("ranked again %+v", got)
	}
}
