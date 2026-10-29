//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package contentlock

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryLock(file *os.File, exclusive bool) (bool, error) {
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	err := unix.Flock(int(file.Fd()), mode|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
func unlock(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }
