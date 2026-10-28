package screens

import (
	"testing"

	"github.com/ujjwal313/boombrick/internal/library"
	"github.com/ujjwal313/boombrick/internal/media"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/ui/widgets"
)

func copperSeason() *library.Album {
	return &library.Album{Artist: "Odalys", Title: "Copper Season", Tracks: []library.Track{
		{Path: "/m/cs/1.flac", Title: "Rust", Artist: "Odalys", AlbumArtist: "Odalys", Album: "Copper Season"},
		{Path: "/m/cs/2.flac", Title: "Ember", Artist: "Odalys", AlbumArtist: "Odalys", Album: "Copper Season"},
	}}
}

func TestAlbumArt(t *testing.T) {
	a := copperSeason()
	s := AlbumArt(a)
	if s.Cover != (media.Cover{Artist: "Odalys", Album: "Copper Season", Path: "/m/cs/1.flac"}) {
		t.Fatalf("cover %+v", s.Cover)
	}
	if s.Hue != widgets.SpecFor("Copper Season").Hue {
		t.Fatal("the placeholder isn't the album's")
	}
	if AlbumArt(&library.Album{Title: "Empty"}).Cover.Key() != "" {
		t.Fatal("an album without tracks names a cover")
	}
}

func TestSongsAndQueuedTracksShareTheirAlbumsArt(t *testing.T) {
	a := copperSeason()
	key := AlbumArt(a).Cover.Key()
	if k := SongArt(&a.Tracks[1]).Cover.Key(); k != key {
		t.Errorf("a song's cover key %q, the album's %q", k, key)
	}
	if k := TrackArt(TrackOf(&a.Tracks[1])).Cover.Key(); k != key {
		t.Errorf("a queued track's cover key %q, the album's %q", k, key)
	}
	if p := TrackArt(TrackOf(&a.Tracks[1])).Cover.Path; p != "/m/cs/2.flac" {
		t.Errorf("a queued track looks for its art in %q", p)
	}
}

func TestStreamedTracksArtIsTheirCover(t *testing.T) {
	tr := player.Track{SpotifyID: "3UmaczJpikHgJFyBTAJVoz", Album: "The Marshall Mathers LP", AlbumArtist: "Eminem",
		Cover: "https://i.scdn.co/image/ab67616d0000b273dbb3dd82da45b7d7f31b1b42"}
	if c := TrackArt(tr).Cover; c != (media.Cover{URL: tr.Cover}) {
		t.Fatalf("cover %+v", c)
	}
	tr.Cover = ""
	if c := TrackArt(tr).Cover; c.URL != "" || c.Album != "The Marshall Mathers LP" {
		t.Fatalf("without a link: %+v", c)
	}
}

func TestArtistArtIsTheirNewestAlbum(t *testing.T) {
	old := &library.Album{Artist: "Odalys", Title: "Northbound", Tracks: []library.Track{{Path: "/m/n/1.flac"}}}
	ar := &library.Artist{Name: "Odalys", Albums: []*library.Album{old, copperSeason()}}
	s := ArtistArt(ar)
	if s.Cover.Album != "Copper Season" || s.Hue != widgets.SpecFor("Odalys").Hue {
		t.Fatalf("artist art %+v", s)
	}
}
