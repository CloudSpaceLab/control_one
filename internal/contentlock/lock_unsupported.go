//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package contentlock

import (
	"errors"
	"os"
)

func tryLock(*os.File, bool) (bool, error) {
	return false, errors.New("content publication locks unsupported on this control-plane platform")
}
func unlock(*os.File) error { return nil }
