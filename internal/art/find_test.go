package art

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

func TestEmbedded(t *testing.T) {
	for _, name := range []string{"art.mp3", "art.flac", "art.m4a", "art.opus"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", name)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s is missing; run make fixtures", path)
			}
			data, ok := Embedded(path)
			if !ok {
				t.Fatal("no picture found")
			}
			img, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			if c := Square(img, 16).NRGBAAt(8, 8); !near(c, color.NRGBA{255, 0, 0, 255}, 40) {
				t.Errorf("the picture is %v, not red", c)
			}
		})
	}
}

func TestEmbeddedWithoutAPicture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	os.WriteFile(path, []byte("not audio"), 0o644)
	if _, ok := Embedded(path); ok {
		t.Fatal("found a picture in a file with no tags")
	}
}

func TestFolderImage(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Folder.JPG", "cover.png", "back.jpg", "cover.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "cover.jpg"), 0o755) // a folder, not a picture
	if got, ok := FolderImage(dir); !ok || filepath.Base(got) != "cover.png" {
		t.Fatalf("FolderImage = %q, %v; want cover.png", got, ok)
	}

	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "FRONT.JPEG"), []byte("x"), 0o644)
	if got, ok := FolderImage(dir2); !ok || filepath.Base(got) != "FRONT.JPEG" {
		t.Fatalf("FolderImage = %q, %v; want FRONT.JPEG", got, ok)
	}
	if _, ok := FolderImage(t.TempDir()); ok {
		t.Fatal("found a picture in an empty folder")
	}
}

func TestAlbumDirs(t *testing.T) {
	sep := string(filepath.Separator)
	disc := filepath.Join(sep+"m", "A", "B", "CD1", "01.mp3")
	if got := albumDirs(disc); !slices.Equal(got, []string{filepath.Dir(disc), filepath.Join(sep+"m", "A", "B")}) {
		t.Fatalf("albumDirs(%q) = %v", disc, got)
	}
	plain := filepath.Join(sep+"m", "A", "B", "01.mp3")
	if got := albumDirs(plain); !slices.Equal(got, []string{filepath.Dir(plain)}) {
		t.Fatalf("albumDirs(%q) = %v", plain, got)
	}
}

// damaged returns tag data cut short or lying about its sizes.
func damaged(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{
		// An ID3 header and a frame claiming far more than the file holds.
		"id3 frame": append([]byte("ID3\x03\x00\x00\x00\x00\x01\x00APIC\x00\x00\x10\x00\x00\x00"), 0, 'i', 'm'),
		// An MP4 moov, udta, meta and ilst nested with sizes past the end.
		"mp4 atoms": []byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00M4A \x00\x00\x7f\x00moov\x00\x00\x7f\x00udta\x00\x00\x7f\x00meta\x00\x00\x00\x00\x00\x00\x7f\x00ilst\x00\x00\x7f\x00covr\x00\x00\x7f\x00data"),
	}
	for _, name := range []string{"art.mp3", "art.m4a", "art.flac"} {
		whole, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			continue
		}
		for _, n := range []int{4, 10, 40, 200, 1000, 5000} {
			if n < len(whole) {
				out[name+" cut"+strconv.Itoa(n)] = whole[:n]
			}
		}
	}
	return out
}

func TestEmbeddedSurvivesDamagedTags(t *testing.T) {
	for name, data := range damaged(t) {
		path := filepath.Join(t.TempDir(), "x")
		os.WriteFile(path, data, 0o644)
		Embedded(path) // must not panic
		t.Log(name, "survived")
	}
}
