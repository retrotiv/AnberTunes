package art

import "testing"

func TestClearDeletesTheCopiesAndMakesThemAgain(t *testing.T) {
	f := newFixture(t)
	cv := albumOn(t, f.root, "Odalys", "Copper Season", &red)
	c := f.cache(t, nil, false)
	waitCover(t, c, cv, 100)
	size, err := c.Size()
	if err != nil || size == 0 {
		t.Fatalf("Size = %d, %v; want the copies' size", size, err)
	}

	freed, err := c.Clear()
	if err != nil || freed != size {
		t.Fatalf("Clear freed %d, %v; want %d", freed, err, size)
	}
	if n := len(cached(t, f.dir)); n != 0 {
		t.Fatalf("%d files left after Clear", n)
	}
	var rows int
	f.db.QueryRow(`SELECT count(*) FROM art`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d art records left after Clear", rows)
	}
	if size, _ := c.Size(); size != 0 {
		t.Fatalf("Size = %d after Clear", size)
	}

	// Asked for again, the art is made again from the music.
	img := waitCover(t, c, cv, 100)
	if !near(img.NRGBAAt(50, 50), red, 6) {
		t.Fatalf("cover after Clear, centre %v", img.NRGBAAt(50, 50))
	}
}

func TestSizeOfAnEmptyCache(t *testing.T) {
	c := newFixture(t).cache(t, nil, false)
	if n, err := c.Size(); n != 0 || err != nil {
		t.Fatalf("Size = %d, %v before anything was cached", n, err)
	}
}
