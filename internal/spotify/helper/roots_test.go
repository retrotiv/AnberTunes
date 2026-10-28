package helper

import (
	"crypto/x509"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The Brick has no CA certificates of its own, so the helper brings its own:
// without them, every request to Spotify fails, signing in first. The test
// runs itself again with the system's hidden, as on the Brick.
func TestRootsWithoutTheSystems(t *testing.T) {
	if os.Getenv("BOOMBRICK_ROOTS_CHILD") == "1" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool.Equal(x509.NewCertPool()) {
			os.Exit(1)
		}
		os.Exit(0)
	}
	none := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRootsWithoutTheSystems$")
	cmd.Env = append(os.Environ(), "BOOMBRICK_ROOTS_CHILD=1",
		"SSL_CERT_FILE="+filepath.Join(none, "none.pem"), "SSL_CERT_DIR="+none)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("no CA certificates without the system's: %v\n%s", err, out)
	}
}
