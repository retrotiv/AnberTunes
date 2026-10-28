package resume

import (
	"testing"
	"time"

	"github.com/ujjwal313/boombrick/internal/player"
)

// What a hand-over leaves reads back as it was written, survives the queue
// being saved again, and clears.
func TestHandoverRoundTrip(t *testing.T) {
	db := openDB(t)
	if h, err := LoadHandover(db); err != nil || h.Playing || h.Sleep != (player.Sleep{}) {
		t.Fatalf("with nothing saved: %+v, %v", h, err)
	}
	if err := SaveHandover(db, Handover{Playing: true}); err != nil {
		t.Fatal(err)
	}
	if h, _ := LoadHandover(db); h.Playing {
		t.Fatal("with no queue saved, a hand-over was stored")
	}
	if err := Save(db, snapshot()); err != nil {
		t.Fatal(err)
	}
	at := time.UnixMilli(time.Now().Add(20 * time.Minute).UnixMilli())
	want := Handover{Playing: true, Sleep: player.Sleep{Mode: player.Sleep30, At: at}}
	if err := SaveHandover(db, want); err != nil {
		t.Fatal(err)
	}
	if err := Save(db, snapshot()); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHandover(db)
	if err != nil || got.Playing != want.Playing || got.Sleep.Mode != want.Sleep.Mode || got.Sleep.At.Sub(want.Sleep.At).Abs() > 50*time.Millisecond {
		t.Fatalf("LoadHandover = %+v, %v; want %+v", got, err, want)
	}
	// A time with a monotonic reading, as the sleep timer's, comes back at the
	// same instant, re-based on the wall clock.
	mono := time.Now().Add(20 * time.Minute)
	if err := SaveHandover(db, Handover{Sleep: player.Sleep{Mode: player.Sleep30, At: mono}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadHandover(db); got.Sleep.At.Sub(mono).Abs() > 50*time.Millisecond {
		t.Fatalf("a monotonic time came back as %v, want %v", got.Sleep.At, mono)
	}
	if err := SaveHandover(db, Handover{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadHandover(db); got.Playing || got.Sleep.Mode != player.SleepOff || !got.Sleep.At.IsZero() {
		t.Fatalf("cleared, it reads %+v", got)
	}
}
