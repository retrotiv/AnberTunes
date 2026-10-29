package main

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
)

// control is what the in-game hotkeys drive. *player.Player is one.
type control interface {
	Toggle()
	Next()
	Prev()
}

// evdev EV_KEY codes on the RG353V gamepad. L1+R1 held is the music modifier (a
// chord games rarely use), so it does not clash with the emulators' own MENU.
const (
	evKey = 1 // EV_KEY

	keyB  = 304
	keyA  = 305
	keyY  = 308
	keyL1 = 310
	keyR1 = 311
)

// watchHotkeys lets L1+R1 plus a face button control the music while a game is
// running and there is no screen for a menu: hold L1+R1 and press A to pause or
// play, Y for the next track, B for the previous one. The background player has
// no SDL window, so it reads the input devices directly. It stops when ctx ends.
func watchHotkeys(ctx context.Context, c control) {
	paths, _ := filepath.Glob("/dev/input/event*")
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		go readDevice(ctx, f, c)
	}
}

func readDevice(ctx context.Context, f *os.File, c control) {
	defer f.Close()
	go func() { <-ctx.Done(); f.Close() }() // closing the fd unblocks Read on shutdown

	buf := make([]byte, 24*64) // input_event is 24 bytes on arm64 (16-byte timeval + 8)
	var l1, r1 bool
	for {
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		for i := 0; i+24 <= n; i += 24 {
			if binary.LittleEndian.Uint16(buf[i+16:]) != evKey {
				continue
			}
			code := binary.LittleEndian.Uint16(buf[i+18:])
			val := int32(binary.LittleEndian.Uint32(buf[i+20:]))
			switch code {
			case keyL1:
				l1 = val != 0
			case keyR1:
				r1 = val != 0
			case keyA:
				if l1 && r1 && val == 1 {
					c.Toggle()
				}
			case keyY:
				if l1 && r1 && val == 1 {
					c.Next()
				}
			case keyB:
				if l1 && r1 && val == 1 {
					c.Prev()
				}
			}
		}
	}
}
