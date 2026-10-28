package art

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// runTag is in the names of the temporary files this process writes, so that
// clearing away those an earlier run left behind can go on while it writes.
var runTag = strconv.FormatInt(time.Now().UnixNano(), 36)

// fileBase names a new set of cached files for the album with key: a hash of
// the album and the time, so new art never takes the names of the copies it
// replaces while they may still be read. The files go in a folder named by
// the hash's first two digits: FAT32 fits only a few thousand albums' copies
// in one folder, and reads through all of it to find a name.
func fileBase(key string) string {
	sum := sha1.Sum([]byte(key))
	h := hex.EncodeToString(sum[:8])
	return h[:2] + "/" + h + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func writeJPEG(path string, img image.Image) error {
	return writeFile(path, func(w io.Writer) error { return jpeg.Encode(w, img, &jpeg.Options{Quality: 90}) })
}

func writePNG(path string, img image.Image) error {
	return writeFile(path, func(w io.Writer) error { return png.Encode(w, img) })
}

// writeFile writes through a temporary file and renames it into place, so a
// reader never sees half a picture. It makes the file's folder if need be.
func writeFile(path string, encode func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + "." + runTag + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	err = encode(w)
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// sweep deletes the temporary files under dir that a run cut short left
// behind.
func sweep(dir string) {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".tmp") && !strings.HasSuffix(p, "."+runTag+".tmp") {
			os.Remove(p)
		}
		return nil
	})
}

// readImage decodes a cached copy.
func readImage(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, err
	}
	return opaque(img), nil
}

// opaque converts an opaque image to NRGBA. image/draw converts JPEG's YCbCr
// quickly only into RGBA, which for opaque pixels holds the same bytes.
func opaque(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Rect, img, b.Min, draw.Src)
	return &image.NRGBA{Pix: rgba.Pix, Stride: rgba.Stride, Rect: rgba.Rect}
}
