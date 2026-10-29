// Package contentlock coordinates supported content publishers and assessment
// readers sharing a content root, including standalone offline import tools.
package contentlock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func Acquire(ctx context.Context, root string, exclusive bool) (func(), error) {
	if strings.TrimSpace(root) == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	flags := os.O_RDONLY | os.O_CREATE
	if exclusive {
		flags = os.O_RDWR | os.O_CREATE
	}
	file, err := os.OpenFile(filepath.Join(root, ".vulnerability-publication.lock"), flags, 0600)
	if err != nil {
		return nil, err
	}
	for {
		locked, err := tryLock(file, exclusive)
		if err != nil {
			file.Close()
			return nil, err
		}
		if locked {
			var once sync.Once
			return func() { once.Do(func() { _ = unlock(file); _ = file.Close() }) }, nil
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
