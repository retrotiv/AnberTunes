//go:build race

package widgets

// raceEnabled is set under the race detector, whose own allocations make
// allocation counts meaningless.
const raceEnabled = true
