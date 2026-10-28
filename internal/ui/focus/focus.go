// Package focus keeps the focused item in view.
package focus

// Scroll returns the offset that keeps [top, bottom) inside a viewport of
// height view, moving as little as possible from offset.
func Scroll(offset, top, bottom, view float64) float64 {
	if top < offset {
		return top
	}
	if bottom > offset+view {
		return bottom - view
	}
	return offset
}
