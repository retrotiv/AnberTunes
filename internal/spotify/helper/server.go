// Package helper is the Spotify helper's own side: a Session on go-librespot,
// and the routes Boombrick asks it over a Unix socket.
package helper

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/ujjwal313/boombrick/internal/spotify"
)

// Session is one Spotify account, signed in or not.
type Session interface {
	Status() spotify.Status
	// Login starts a device sign-in, dropping any under way, and returns its
	// code once Spotify has issued it.
	Login(ctx context.Context) (spotify.Login, error)
	Logout() error
	Playlists(ctx context.Context) ([]spotify.Playlist, error)
	// Tracks lists a playlist's tracks, or Liked Songs' (spotify.LikedURI).
	Tracks(ctx context.Context, uri string) ([]spotify.Track, error)
	// Stream opens a track, decoded, from ms; levelled with Spotify's own
	// loudness data if level is set.
	Stream(ctx context.Context, id string, ms int64, kbps int, level bool) (Stream, error)
}

// Stream is a track being decoded.
type Stream interface {
	SampleRate() int
	Duration() int64 // in milliseconds
	// Read fills p with interleaved stereo samples, returning io.EOF at the
	// track's end, or errTaken once a newer request has the stream.
	Read(p []float32) (int, error)
	// Release hands the stream back; one let go before its end may be kept a
	// while, so a seek or a return to the track doesn't ask Spotify for its
	// key again.
	Release()
}

// errTaken ends the reading of a stream a newer request for the same track
// has taken over, as a seek's does.
var errTaken = errors.New("stream taken over by a newer request")

// chunk is how many samples go out per write: about 23 ms of audio.
const chunk = 2048

// Handler answers Boombrick's requests for s.
func Handler(s Session, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.Status(), nil)
	})
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.Login(r.Context())
		reply(w, l, err)
	})
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Logout(); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /playlists", func(w http.ResponseWriter, r *http.Request) {
		ps, err := s.Playlists(r.Context())
		reply(w, ps, err)
	})
	mux.HandleFunc("GET /tracks", func(w http.ResponseWriter, r *http.Request) {
		ts, err := s.Tracks(r.Context(), r.URL.Query().Get("uri"))
		reply(w, ts, err)
	})
	mux.HandleFunc("GET /stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ms, _ := strconv.ParseInt(q.Get("ms"), 10, 64)
		kbps, err := strconv.Atoi(q.Get("kbps"))
		if err != nil {
			kbps = spotify.DefaultKbps
		}
		st, err := s.Stream(r.Context(), r.PathValue("id"), max(ms, 0), kbps, q.Get("level") == "1")
		if err != nil {
			log.Warn("stream", "id", r.PathValue("id"), "err", err)
			fail(w, err)
			return
		}
		defer st.Release()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set(spotify.HeaderRate, strconv.Itoa(st.SampleRate()))
		w.Header().Set(spotify.HeaderDuration, strconv.FormatInt(st.Duration(), 10))
		if err := send(w, st); err != nil && r.Context().Err() == nil {
			if !errors.Is(err, errTaken) { // a seek's newer request has it
				log.Warn("stream cut short", "id", r.PathValue("id"), "err", err)
			}
			// Ending the response abruptly tells the reader the track didn't
			// end: it asks again from where it got to.
			panic(http.ErrAbortHandler)
		}
	})
	return mux
}

// send writes st's samples as little-endian float32 until it ends.
func send(w io.Writer, st Stream) error {
	samples := make([]float32, chunk)
	buf := make([]byte, 4*chunk)
	for {
		n, err := st.Read(samples)
		for i, v := range samples[:n] {
			binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(v))
		}
		if n > 0 {
			if _, werr := w.Write(buf[:4*n]); werr != nil {
				return werr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	code, wait := spotify.StatusOf(err)
	if wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	}
	http.Error(w, err.Error(), code)
}
