package helper

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/ap"
	"github.com/devgianlu/go-librespot/audio"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
	playlist4pb "github.com/devgianlu/go-librespot/proto/spotify/playlist4"
	"github.com/devgianlu/go-librespot/spclient"
	"google.golang.org/protobuf/proto"

	"github.com/ujjwal313/boombrick/internal/spotify"
)

func TestPlaylistsLeaveFoldersOut(t *testing.T) {
	items := []*playlist4pb.Item{
		{Uri: proto.String("spotify:start-group:abc:Workouts")},
		{Uri: proto.String("spotify:playlist:37i9dQZF1DX76Wlfdnj7AP")},
		{Uri: proto.String("spotify:end-group:abc")},
		{Uri: proto.String("spotify:playlist:5ABHKGoOzxkaa28ttQV9sE")},
	}
	metas := []*playlist4pb.MetaItem{
		{},
		{Attributes: &playlist4pb.ListAttributes{Name: proto.String("Beast Mode"), PictureSize: []*playlist4pb.PictureSize{
			{TargetName: proto.String("large"), Url: proto.String("https://i.scdn.co/image/large")},
			{TargetName: proto.String("default"), Url: proto.String("https://i.scdn.co/image/default")},
		}}, OwnerUsername: proto.String("spotify"), Length: proto.Int32(50)},
		{},
		{Attributes: &playlist4pb.ListAttributes{Name: proto.String("Road trip"), Picture: []byte{0xab, 0x01}},
			OwnerUsername: proto.String("sam"), Length: proto.Int32(88)},
	}
	want := []spotify.Playlist{
		{URI: "spotify:playlist:37i9dQZF1DX76Wlfdnj7AP", Name: "Beast Mode", Owner: "spotify", Count: 50, Cover: "https://i.scdn.co/image/default"},
		{URI: "spotify:playlist:5ABHKGoOzxkaa28ttQV9sE", Name: "Road trip", Owner: "sam", Count: 88, Cover: "https://i.scdn.co/image/ab01"},
	}
	if got := playlists(items, metas); !reflect.DeepEqual(got, want) {
		t.Fatalf("playlists\n got %+v\nwant %+v", got, want)
	}
}

func TestTrackFromMetadata(t *testing.T) {
	m := &metadatapb.Track{
		Name:     proto.String("Stan"),
		Duration: proto.Int32(404000),
		Artist:   []*metadatapb.Artist{{Name: proto.String("Eminem")}, {Name: proto.String("Dido")}},
		Album: &metadatapb.Album{
			Name:   proto.String("The Marshall Mathers LP"),
			Artist: []*metadatapb.Artist{{Name: proto.String("Eminem")}},
			CoverGroup: &metadatapb.ImageGroup{Image: []*metadatapb.Image{
				{FileId: []byte{0x01}, Size: metadatapb.Image_SMALL.Enum()},
				{FileId: []byte{0x02}, Size: metadatapb.Image_LARGE.Enum()},
				{FileId: []byte{0x03}, Size: metadatapb.Image_DEFAULT.Enum()},
			}},
		},
		ExternalId: []*metadatapb.ExternalId{{Type: proto.String("isrc"), Id: proto.String("USIR10000449")}},
	}
	want := spotify.Track{ID: "3UmaczJpikHgJFyBTAJVoz", Title: "Stan", Artists: []string{"Eminem", "Dido"},
		Album: "The Marshall Mathers LP", AlbumArtist: "Eminem", Duration: 404000,
		Cover: "https://i.scdn.co/image/02", ISRC: "USIR10000449"}
	if got := track("3UmaczJpikHgJFyBTAJVoz", m); !reflect.DeepEqual(got, want) {
		t.Fatalf("track\n got %+v\nwant %+v", got, want)
	}
	if got := cover(nil); got != "" {
		t.Fatalf("cover of nothing = %q", got)
	}
}

func TestProductType(t *testing.T) {
	for _, c := range []struct{ info, want string }{
		{`<?xml version="1.0" encoding="utf-8"?><products><product><type>premium</type><head-files-url>x</head-files-url></product></products>`, "premium"},
		{"<products>\n  <product>\n    <type>free</type>\n  </product>\n  <product><type>premium</type></product>\n</products>", "free"},
		{`<products><product><head-files-url>x</head-files-url></product></products>`, ""},
		{`<products><product><type>premium`, ""},
		{"garbage", ""},
		{"", ""},
	} {
		if got := productType([]byte(c.info)); got != c.want {
			t.Errorf("productType(%q) = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	var th *spotify.Throttled
	if err := classify(fmt.Errorf("failed retrieving audio key: %w", &audio.KeyProviderError{Code: 2})); !errors.As(err, &th) || th.Wait != spotify.KeyWait {
		t.Fatalf("a refused key: %v", err)
	}
	if err := classify(&spclient.RateLimitedError{RetryAfter: 12 * time.Second}); !errors.As(err, &th) || th.Wait != 12*time.Second {
		t.Fatalf("a rate limit: %v", err)
	}
	if err := classify(librespot.ErrMediaRestricted); !errors.Is(err, spotify.ErrUnavailable) {
		t.Fatalf("a restricted track: %v", err)
	}
	if err := classify(&ap.AccesspointLoginError{}); !errors.Is(err, spotify.ErrSignedOut) {
		t.Fatalf("a refused login: %v", err)
	}
	if err := classify(fmt.Errorf("resolve: %w", &netErr{})); !errors.Is(err, spotify.ErrOffline) {
		t.Fatalf("a network error: %v", err)
	}
	// A certificate that can't be checked comes inside a network error, but
	// the network is there: saying "offline" would send the user the wrong
	// way, as when the helper had no CA certificates on the Brick.
	bad := &url.Error{Op: "Post", URL: "https://clienttoken.spotify.com/v1/clienttoken",
		Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}}
	if err := classify(fmt.Errorf("failed requesting clienttoken: %w", bad)); errors.Is(err, spotify.ErrOffline) {
		t.Fatalf("a certificate that can't be checked: %v", err)
	}
}

type netErr struct{}

func (*netErr) Error() string   { return "no route to host" }
func (*netErr) Timeout() bool   { return false }
func (*netErr) Temporary() bool { return false }

// The credentials file is the owner's alone, and reads back as written.
func TestStoreKeepsCredentialsPrivate(t *testing.T) {
	s := store{filepath.Join(t.TempDir(), "state.json")}
	st, err := s.Load()
	if err != nil || st.Credentials.Username != "" {
		t.Fatalf("a missing file loads as %+v, %v", st, err)
	}
	st.DeviceId = "0123"
	st.Credentials.Username, st.Credentials.Data = "sam", []byte{1, 2, 3}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", fi.Mode(), err)
	}
	back, err := s.Load()
	if err != nil || back.DeviceId != "0123" || back.Credentials.Username != "sam" || !reflect.DeepEqual(back.Credentials.Data, []byte{1, 2, 3}) {
		t.Fatalf("loaded %+v, %v", back, err)
	}
}
