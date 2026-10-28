package helper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/devgianlu/go-librespot/session"
)

// A display name is looked up once and remembered; one that can't be had
// shows as the ID and is asked again next time.
func TestNamesAreLookedUpOnce(t *testing.T) {
	asked := map[string]int{}
	s := &Librespot{log: slog.New(slog.NewTextHandler(io.Discard, nil)), names: map[string]string{},
		profile: func(_ context.Context, _ *session.Session, user string) (string, error) {
			asked[user]++
			switch user {
			case "spotify":
				return "Spotify", nil
			case "22nicxbosy3tj5izxtvat4p6y":
				return "VAQ", nil
			}
			return "", errors.New("503 Service Unavailable")
		}}
	ctx := context.Background()
	for range 2 {
		if got := s.nameOf(ctx, nil, "spotify"); got != "Spotify" {
			t.Fatalf("spotify is %q", got)
		}
		if got := s.nameOf(ctx, nil, "22nicxbosy3tj5izxtvat4p6y"); got != "VAQ" {
			t.Fatalf("22nicxbosy3tj5izxtvat4p6y is %q", got)
		}
		if got := s.nameOf(ctx, nil, "bmfjclzv7pstc3zzlq0nteyni"); got != "bmfjclzv7pstc3zzlq0nteyni" {
			t.Fatalf("a name that can't be had is %q", got)
		}
	}
	if asked["spotify"] != 1 || asked["22nicxbosy3tj5izxtvat4p6y"] != 1 || asked["bmfjclzv7pstc3zzlq0nteyni"] != 2 {
		t.Fatalf("asked %v", asked)
	}
}
