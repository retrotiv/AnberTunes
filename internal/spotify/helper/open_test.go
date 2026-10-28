package helper

import (
	"errors"
	"math"
	"testing"

	librespot "github.com/devgianlu/go-librespot"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
)

// A track kept from the country plays as its first alternative that isn't.
func TestPlayableTakesAnAlternative(t *testing.T) {
	only := func(countries string) []*metadatapb.Restriction {
		return []*metadatapb.Restriction{{CountryRestriction: &metadatapb.Restriction_CountriesAllowed{CountriesAllowed: countries}}}
	}
	not := func(countries string) []*metadatapb.Restriction {
		return []*metadatapb.Restriction{{CountryRestriction: &metadatapb.Restriction_CountriesForbidden{CountriesForbidden: countries}}}
	}
	track := func() *metadatapb.Track {
		return &metadatapb.Track{Gid: []byte{1}, Restriction: only("USGB"), Alternative: []*metadatapb.Track{
			{Gid: []byte{2}, Restriction: not("DEIN")},
			{Gid: []byte{3}, Restriction: only("INFR"), File: []*metadatapb.AudioFile{{FileId: []byte{9}}}},
		}}
	}
	if got, err := playable(track(), "GB"); err != nil || got.Gid[0] != 1 {
		t.Fatalf("in GB: %v, %v", got, err)
	}
	got, err := playable(track(), "in")
	if err != nil || got.Gid[0] != 3 || len(got.File) != 1 || got.Alternative != nil {
		t.Fatalf("in India: %v, %v", got, err)
	}
	if _, err := playable(track(), "JP"); err != nil {
		t.Fatalf("in Japan, the first alternative isn't kept out: %v", err)
	}
	if _, err := playable(&metadatapb.Track{Restriction: only("US")}, "IN"); !errors.Is(err, librespot.ErrMediaRestricted) {
		t.Fatalf("no alternative: %v", err)
	}
	if _, err := playable(track(), ""); err != nil {
		t.Fatalf("with no country known: %v", err)
	}
}

// The Vorbis file nearest the quality asked for is chosen; others are left
// alone, as they can't be decoded.
func TestVorbisFile(t *testing.T) {
	file := func(f metadatapb.AudioFile_Format) *metadatapb.AudioFile {
		return &metadatapb.AudioFile{Format: f.Enum(), FileId: []byte{byte(f)}}
	}
	files := []*metadatapb.AudioFile{
		file(metadatapb.AudioFile_MP3_160),
		file(metadatapb.AudioFile_OGG_VORBIS_96),
		file(metadatapb.AudioFile_OGG_VORBIS_320),
		file(metadatapb.AudioFile_FLAC_FLAC),
	}
	for kbps, want := range map[int]metadatapb.AudioFile_Format{
		96:  metadatapb.AudioFile_OGG_VORBIS_96,
		160: metadatapb.AudioFile_OGG_VORBIS_96,
		320: metadatapb.AudioFile_OGG_VORBIS_320,
	} {
		if got := vorbisFile(files, kbps); got.GetFormat() != want {
			t.Errorf("%d kbps: %v, want %v", kbps, got.GetFormat(), want)
		}
	}
	if got := vorbisFile(files[:1], 160); got != nil {
		t.Errorf("only MP3: %v", got)
	}
}

// Levelling brings a track to -14 LUFS, unless its peak would clip.
func TestLevelling(t *testing.T) {
	for _, c := range []struct{ loudness, peak, want float32 }{
		{-8, -1, 0.501},   // loud: turned down by 6 dB
		{-14, -3, 1},      // on target
		{-20, -0.5, 1.06}, // quiet, but 6 dB up would clip: up as far as its peak allows
	} {
		if got := levelling(c.loudness, c.peak); math.Abs(float64(got-c.want)) > 0.005 {
			t.Errorf("%v LUFS, %v dBTP: %v, want %v", c.loudness, c.peak, got, c.want)
		}
	}
}
