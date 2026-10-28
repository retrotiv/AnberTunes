package platform

import (
	"os"
	"syscall"
	"unsafe"
)

// The brightness requests of Allwinner's display driver, as NextUI's
// msettings sends them on the Brick.
const (
	dispSetBrightness = 0x102 // DISP_LCD_SET_BRIGHTNESS
	dispGetBrightness = 0x103 // DISP_LCD_GET_BRIGHTNESS
)

// dispDevice is the display driver, opened for each request.
type dispDevice struct{ path string }

// ioctl sends req for screen 0 with value v, and returns the driver's answer.
func (d dispDevice) ioctl(req uintptr, v int) (int, error) {
	f, err := os.OpenFile(d.path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	args := [4]uintptr{0, uintptr(v), 0, 0}
	r, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

func (d dispDevice) brightness() (int, error) { return d.ioctl(dispGetBrightness, 0) }

func (d dispDevice) setBrightness(v int) error {
	_, err := d.ioctl(dispSetBrightness, v)
	return err
}
