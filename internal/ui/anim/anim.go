// Package anim provides tweens driven by an explicit clock, so the UI redraws
// only while something is moving.
package anim

import "time"

// Animation durations.
const (
	Focus   = 120 * time.Millisecond
	Push    = 180 * time.Millisecond
	Accent  = 600 * time.Millisecond
	ArtFade = 150 * time.Millisecond
	Lyric   = 250 * time.Millisecond
	Dialog  = 150 * time.Millisecond
)

// Ease maps linear progress in [0, 1] to eased progress.
type Ease func(t float64) float64

// EaseOutCubic starts fast and settles.
func EaseOutCubic(t float64) float64 {
	t = 1 - t
	return 1 - t*t*t
}

// EaseInOutCubic accelerates, then decelerates.
func EaseInOutCubic(t float64) float64 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	t = -2*t + 2
	return 1 - t*t*t/2
}

// Value is a number that animates towards a target. The zero Value is 0 and
// at rest.
type Value struct {
	from, to float64
	start    time.Time
	dur      time.Duration
	ease     Ease
}

// Set jumps to x.
func (v *Value) Set(x float64) { *v = Value{from: x, to: x} }

// To animates from the value at now to x over d. Retargeting mid-flight
// continues from where the value currently is.
func (v *Value) To(x float64, now time.Time, d time.Duration, e Ease) {
	if x == v.to {
		return
	}
	v.from, v.to, v.start, v.dur, v.ease = v.At(now), x, now, d, e
}

// Shift moves the value and its target by d, keeping any animation's timing,
// as when what it measures moved underneath it.
func (v *Value) Shift(d float64) { v.from, v.to = v.from+d, v.to+d }

// At returns the value at now.
func (v *Value) At(now time.Time) float64 {
	if v.Done(now) {
		return v.to
	}
	if now.Before(v.start) {
		return v.from
	}
	t := float64(now.Sub(v.start)) / float64(v.dur)
	return v.from + (v.to-v.from)*v.ease(t)
}

// Done reports whether the value has reached its target by now.
func (v *Value) Done(now time.Time) bool {
	return v.dur <= 0 || !now.Before(v.start.Add(v.dur))
}

// Target is the value being animated towards.
func (v *Value) Target() float64 { return v.to }
