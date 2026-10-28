package uitest

import (
	"image"
	"testing"
)

// A missing reference image skips the test instead of failing it, because
// golden images are generated locally and kept out of git.
func TestGoldenSkipsWhenMissing(t *testing.T) {
	t.Setenv("BOOMBRICK_UPDATE_GOLDEN", "")
	reached := false
	ok := t.Run("missing", func(t *testing.T) {
		Golden(t, "no-such-golden", image.NewNRGBA(image.Rect(0, 0, 4, 4)))
		reached = true
	})
	if !ok {
		t.Fatal("a missing golden image failed the test")
	}
	if reached {
		t.Fatal("a missing golden image should skip the test")
	}
}
