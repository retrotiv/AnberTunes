// Command boombrick-spotify is Boombrick's Spotify helper: the Spotify
// session, sign-in, library and streams, on go-librespot, answered over a
// Unix socket.
//
//	boombrick-spotify serve <data dir> <socket>
//	boombrick-spotify login <data dir>
//
// serve answers on the socket until its standard input closes, which happens
// when Boombrick, holding the other end, quits or crashes. login signs in
// from a terminal.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/ujjwal313/boombrick/internal/spotify"
	"github.com/ujjwal313/boombrick/internal/spotify/helper"
)

const usage = `usage: boombrick-spotify serve <data dir> <socket>
       boombrick-spotify login <data dir>`

func main() {
	// The helper runs beside Boombrick in the Brick's 1 GB; most of its heap
	// is the downloads of the streams open.
	debug.SetMemoryLimit(48 << 20)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("from", "spotify")
	var err error
	switch {
	case len(os.Args) == 4 && os.Args[1] == "serve":
		err = serve(os.Args[2], os.Args[3], os.Stdin, log)
	case len(os.Args) == 3 && os.Args[1] == "login":
		err = login(os.Args[2], os.Stdout, log)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

// serve answers Boombrick on socket until parent ends.
func serve(dir, socket string, parent io.Reader, log *slog.Logger) error {
	s, err := helper.NewLibrespot(dir, log)
	if err != nil {
		return err
	}
	defer s.Close()
	os.Remove(socket) // left by a helper that crashed
	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer os.Remove(socket)
	srv := &http.Server{Handler: helper.Handler(s, log)}
	go srv.Serve(l)
	defer srv.Close()
	io.Copy(io.Discard, parent)
	return nil
}

// login signs in with a device code, which it prints, and waits for it to be
// approved.
func login(dir string, out io.Writer, log *slog.Logger) error {
	s, err := helper.NewLibrespot(dir, log)
	if err != nil {
		return err
	}
	defer s.Close()
	l, err := s.Login(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Open %s and approve AnberTunes (code %s) before %s.\n", l.URL, l.Code, l.Expires.Format(time.Kitchen))
	for {
		switch st := s.Status(); st.State {
		case spotify.Ready:
			fmt.Fprintf(out, "Signed in as %s.\n", st.User)
			return nil
		case spotify.SignedOut:
			return errors.New("not signed in: the code was refused or expired")
		}
		time.Sleep(time.Second)
	}
}
