package lyrics

import "testing"

func TestClearForgetsWhatWasFetched(t *testing.T) {
	db := openDB(t)
	f := &fakeFetch{r: Result{Synced: twoLines}}
	s := service(t, db, f, true)
	tr := song(t, t.TempDir(), "")
	if _, st := answer(t, s, tr); st != Found || f.count() != 1 {
		t.Fatalf("first answer %v after %d fetches", st, f.count())
	}
	size, err := s.Size()
	if err != nil || size != int64(len(twoLines)) {
		t.Fatalf("Size = %d, %v; want %d", size, err, len(twoLines))
	}

	freed, err := s.Clear()
	if err != nil || freed != size {
		t.Fatalf("Clear freed %d, %v; want %d", freed, err, size)
	}
	if size, _ := s.Size(); size != 0 {
		t.Fatalf("Size = %d after Clear", size)
	}
	// Asked for again, the lyrics are fetched again.
	if _, st := answer(t, s, tr); st != Found || f.count() != 2 {
		t.Fatalf("answer after Clear %v after %d fetches, want a second fetch", st, f.count())
	}
}

func TestClearLeavesLrcFilesAlone(t *testing.T) {
	s := service(t, openDB(t), &fakeFetch{}, false)
	tr := song(t, t.TempDir(), twoLines)
	answer(t, s, tr)
	if _, err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if l, st := answer(t, s, tr); st != Found || len(l.Lines) != 2 {
		t.Fatalf("after Clear: %v, %d lines; want the .lrc's two", st, len(l.Lines))
	}
}
