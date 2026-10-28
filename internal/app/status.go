package app

import (
	"time"

	"github.com/ujjwal313/boombrick/internal/platform"
)

// statusEvery is how often the battery, network and Bluetooth are read.
const statusEvery = 10 * time.Second

// watchStatus calls read at once and then every interval until stop is
// closed, and hands each reading that differs from the one before to out,
// then calls wake. out holds one reading; one the loop hasn't taken yet is
// replaced, so it only ever sees the latest. lit says when the screen goes off
// and on: while it is off nothing is read, and as it comes on it is read at
// once.
func watchStatus(read func() platform.Status, every time.Duration, out chan platform.Status, wake func(), lit <-chan bool, stop <-chan struct{}) {
	t := time.NewTimer(every)
	defer t.Stop()
	var last platform.Status
	first, on := true, true
	for {
		if on {
			if st := read(); first || st != last {
				first, last = false, st
				select {
				case <-out:
				default:
				}
				out <- st // the only sender, so there is room now
				wake()
			}
			t.Reset(every)
		}
		select {
		case <-stop:
			return
		case <-t.C:
		case on = <-lit:
			if !on {
				t.Stop()
			}
		}
	}
}
