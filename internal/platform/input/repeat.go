package input

import "time"

// Repeater produces Repeat events for a held D-pad direction: the first after
// Delay, then every Interval, and every FastInterval once FastAfter repeats
// have fired so long lists scroll quicker. Only the latest direction repeats.
type Repeater struct {
	Delay, Interval, FastInterval time.Duration
	FastAfter                     int

	held  Button
	next  time.Time
	count int
}

// NewRepeater returns a repeater that starts after 300 ms and repeats every
// 60 ms, then every 30 ms after ten repeats.
func NewRepeater() *Repeater {
	return &Repeater{
		Delay:        300 * time.Millisecond,
		Interval:     60 * time.Millisecond,
		FastInterval: 30 * time.Millisecond,
		FastAfter:    10,
	}
}

// Press starts repeating b if it is a D-pad direction.
func (r *Repeater) Press(b Button, now time.Time) {
	if !b.IsDpad() {
		return
	}
	r.held, r.next, r.count = b, now.Add(r.Delay), 0
}

// Release stops repeating b.
func (r *Repeater) Release(b Button) {
	if b == r.held {
		r.held = None
	}
}

// Due returns at most one repeat if it has come due by now. The next one is
// scheduled from now, so a stalled loop never gets a burst of repeats.
func (r *Repeater) Due(now time.Time) []Event {
	if r.held == None || now.Before(r.next) {
		return nil
	}
	r.count++
	interval := r.Interval
	if r.count >= r.FastAfter {
		interval = r.FastInterval
	}
	r.next = now.Add(interval)
	return []Event{{Button: r.held, Kind: Repeat}}
}

// Next reports when the next repeat is due, so the event loop knows how long
// it may sleep.
func (r *Repeater) Next() (time.Time, bool) { return r.next, r.held != None }
