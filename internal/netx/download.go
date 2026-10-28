package netx

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Download streams url to the file path, at most limit bytes, checking it
// against the SHA-256 want as it comes and reporting progress every 256 KB
// and at the end; total is -1 when the server doesn't say. A file that comes
// whole but doesn't match is left for the caller to remove.
func Download(ctx context.Context, c *http.Client, url, path, want string, limit int64, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := send(c, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > limit {
		return ErrTooLarge
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, report: progress}
	n, err := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return ErrTooLarge
	}
	pw.flush()
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("the download's checksum is %s, not %s", got, want)
	}
	return f.Close()
}

// SumFor finds name's SHA-256 in a checksums file as sha256sum writes one.
func SumFor(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// progressWriter reports bytes as they pass, every 256 KB and at the end.
type progressWriter struct {
	done, total, told int64
	report            func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.done-p.told >= 256<<10 {
		p.flush()
	}
	return len(b), nil
}

func (p *progressWriter) flush() {
	if p.report != nil {
		p.report(p.done, p.total)
	}
	p.told = p.done
}
