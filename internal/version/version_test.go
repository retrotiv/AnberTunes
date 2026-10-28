package version

import "testing"

func TestString(t *testing.T) {
	if got, want := String(), "AnberTunes dev"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
