package netx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDownload(t *testing.T) {
	body := bytes.Repeat([]byte("boombrick "), 100_000) // 1 MB
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Boombrick/") {
			http.Error(w, "who?", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "file")

	var reports int
	var last, total int64
	err := Download(context.Background(), srv.Client(), srv.URL, path, want, 2<<20, func(d, n int64) { reports, last, total = reports+1, d, n })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, body) {
		t.Fatal("the file isn't what was served")
	}
	if last != int64(len(body)) || total != last || reports < 4 || reports > 6 {
		t.Fatalf("%d reports, the last %d of %d", reports, last, total)
	}

	if err := Download(context.Background(), srv.Client(), srv.URL, path, strings.Repeat("0", 64), 2<<20, nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("a wrong checksum: %v", err)
	}
	if err := Download(context.Background(), srv.Client(), srv.URL, path, want, 512<<10, nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
}

// trackedBody is a response body that notes whether it was read to its end
// and closed.
type trackedBody struct {
	io.Reader
	read, closed bool
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read = b.read || err == io.EOF
	return n, err
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadReportsTheStatus(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("busy")}
	c := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"7"}},
			Body: body, Request: r}, nil
	})}
	path := filepath.Join(t.TempDir(), "file")
	err := Download(context.Background(), c, "https://example.com/file", path, strings.Repeat("0", 64), 1<<20, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 503 || se.RetryAfter != 7*time.Second {
		t.Fatalf("503 gave %v", err)
	}
	if !body.read || !body.closed {
		t.Fatal("the body wasn't read to its end and closed, so the connection can't be reused")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a file was made for an error")
	}
}

func TestSumFor(t *testing.T) {
	sums := []byte("ABC123  Boombrick.pak.zip\n0000 *yt-dlp_linux.zip\nnot a line\n")
	if s, ok := SumFor(sums, "Boombrick.pak.zip"); !ok || s != "abc123" {
		t.Fatalf("SumFor = %q, %v", s, ok)
	}
	if s, ok := SumFor(sums, "yt-dlp_linux.zip"); !ok || s != "0000" {
		t.Fatalf("a binary-mode line: %q, %v", s, ok)
	}
	if _, ok := SumFor(sums, "other.zip"); ok {
		t.Fatal("found a file that isn't listed")
	}
}
