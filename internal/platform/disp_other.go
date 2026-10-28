//go:build !linux

package platform

import "errors"

// dispDevice stands in for the Brick's display driver on systems without it.
type dispDevice struct{ path string }

func (dispDevice) brightness() (int, error) { return 0, errors.ErrUnsupported }
func (dispDevice) setBrightness(int) error  { return errors.ErrUnsupported }
