package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/ujjwal313/boombrick/internal/importer"
	"github.com/ujjwal313/boombrick/internal/platform/input"
	"github.com/ujjwal313/boombrick/internal/player"
	"github.com/ujjwal313/boombrick/internal/screens"
	spotifyscreen "github.com/ujjwal313/boombrick/internal/screens/spotify"
	"github.com/ujjwal313/boombrick/internal/spotify"
)

// A dialog opened by news takes every button, Menu's too, until it closes.
func TestAlertTakesInputFirst(t *testing.T) {
	s, tabs, _ := testShell()
	signIns := 0
	e := spotifyscreen.NewExpired(func() { signIns++ })
	s.alert = e
	e.Open(t0)
	for _, b := range []input.Button{input.Start, input.R1, input.Menu} {
		s.handle(press(b), t0)
	}
	if len(tabs[0].got) != 0 || s.open >= 0 || s.tab != 0 {
		t.Fatalf("buttons reached the tab: %v, overlay %d, tab %d", tabs[0].got, s.open, s.tab)
	}
	s.handle(press(input.A), t0)
	if signIns != 1 || e.IsOpen() {
		t.Fatalf("Sign in again: %d sign-ins, open %v", signIns, e.IsOpen())
	}
	s.handle(press(input.Down), t0)
	if len(tabs[0].got) != 1 {
		t.Fatal("the tab doesn't take buttons once the dialog has closed")
	}
}

// An expired session opens the dialog once, not with every song skipped;
// other news is a toast.
func TestSignedOutOpensTheDialogOnce(t *testing.T) {
	s, _, _ := testShell()
	a := &App{shell: s, opts: Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	a.expired = spotifyscreen.NewExpired(func() {})
	s.alert = a.expired
	a.notice(player.Notice{Kind: player.SignedOut}, t0)
	if !a.expired.IsOpen() || s.toast != "" {
		t.Fatalf("open %v, toast %q", a.expired.IsOpen(), s.toast)
	}
	s.handle(press(input.B), t0)
	a.notice(player.Notice{Kind: player.SignedOut}, t0)
	if a.expired.IsOpen() {
		t.Fatal("the dialog opened again before signing in again")
	}
	a.notice(player.Notice{Kind: player.Unavailable}, t0)
	if s.toast != "Skipped a song that isn’t available here" {
		t.Fatalf("toast %q", s.toast)
	}
}

// Home's row opens Liked Songs as a playlist page, and with importing X
// starts the import at its review; offline, it says why it can't.
func TestSpotifyRowOpensAndImports(t *testing.T) {
	s, _, _ := testShell()
	a := &App{shell: s}
	online := false
	a.env = &spotifyscreen.Env{Nav: a, Import: noImports{}, Online: func() bool { return online }}
	a.openPlaylist(spotify.Playlist{URI: spotify.LikedURI, Name: "Liked Songs"})
	if _, ok := s.top().(*spotifyscreen.PlaylistPage); !ok {
		t.Fatalf("Liked Songs opened %T", s.top())
	}
	road := spotify.Playlist{URI: "spotify:playlist:1", Name: "Road trip"}
	a.importPlaylist(road)
	if _, ok := s.top().(*spotifyscreen.PlaylistPage); !ok || s.toast != "Connect to Wi-Fi to import" {
		t.Fatalf("offline, X opened %T, toast %q", s.top(), s.toast)
	}
	online = true
	a.importPlaylist(road)
	if _, ok := s.top().(*spotifyscreen.Review); !ok {
		t.Fatalf("X opened %T", s.top())
	}
}

// The offline screens' Open Downloads shows the Downloads tab, closing any
// overlay.
func TestOpenDownloads(t *testing.T) {
	s, _, _ := testShell()
	s.stacks = append(s.stacks, nil, nil, nil)
	a := &App{shell: s}
	s.openOverlay(0, t0)
	if !screens.OpenDownloads(a) || s.tab != screens.DownloadsTab || s.open >= 0 {
		t.Fatalf("tab %d, overlay %d", s.tab, s.open)
	}
}

// noImports is an importer with nothing imported.
type noImports struct{}

func (noImports) Import(uri, name string, tracks []spotify.Track) {}
func (noImports) Changes(uri string, since uint64) (importer.Changes, bool) {
	return importer.Changes{}, false
}
func (noImports) Err() error                { return nil }
func (noImports) Use(uri, id string, c int) {}
func (noImports) Skip(uri, id string)       {}
func (noImports) Cancel(uri string)         {}
func (noImports) Version() uint64           { return 0 }
