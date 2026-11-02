//go:build linux

package onlineapt

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// A privileged local administrator remains trusted. The staging tree and its
// ancestors cannot be writable by an unprivileged user or replaced via a link.
func protected(path string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("online plan installation requires root")
	}
	if err := canonicalPath(path); err != nil {
		return err
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || (info.Mode().Perm()&0022 != 0 && !(info.IsDir() && info.Mode()&os.ModeSticky != 0)) {
			return fmt.Errorf("online plan path must be root-owned and protected: %s", current)
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}

func lock(base string) (func(), error) {
	if err := protected(base); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(base, ".execution.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "online plan execution lock")
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, fmt.Errorf("unprotected online plan execution lock")
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another online plan staging/install operation is active: %w", err)
	}
	return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = f.Close() }, nil
}
