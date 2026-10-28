package focus

import "testing"

func TestScroll(t *testing.T) {
	cases := []struct{ offset, top, bottom, view, want float64 }{
		{0, 100, 200, 300, 0},     // already visible
		{0, 250, 400, 300, 100},   // below: scroll down just enough
		{200, 100, 150, 300, 100}, // above: scroll up to its top
	}
	for _, tc := range cases {
		if got := Scroll(tc.offset, tc.top, tc.bottom, tc.view); got != tc.want {
			t.Errorf("Scroll(%v) = %v, want %v", tc, got, tc.want)
		}
	}
}
