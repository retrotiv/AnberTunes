package main

import (
	"testing"

	"github.com/ujjwal313/boombrick/internal/theme"
)

func TestIcon(t *testing.T) {
	img, err := Icon(256)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("size %v", b)
	}
	if c := img.NRGBAAt(0, 0); c.A != 0 {
		t.Errorf("a corner is %v, want it clear past the rounding", c)
	}
	if c := img.NRGBAAt(128, 12); c != theme.Ground {
		t.Errorf("above the logo is %v, want the ground", c)
	}
	gold := 0
	for y := range 256 {
		for x := range 256 {
			if img.NRGBAAt(x, y) == theme.Gold {
				gold++
			}
		}
	}
	if gold < 2000 {
		t.Errorf("%d gold pixels; the logo is missing", gold)
	}
}
