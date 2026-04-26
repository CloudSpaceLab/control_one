//go:build !linux

package fileaccess

import (
	"context"

	"go.uber.org/zap"
)

// Non-Linux platforms get a no-op stub for now. Windows ETW + macOS
// fs_usage backends land in a follow-up; this stub keeps Manager.Run from
// blocking on platforms we don't yet have file-access hooks for.
type stubBackend struct{}

func init() {
	registerFileBackend(0, func(opts Options, log *zap.Logger) Collector {
		_ = opts
		_ = log
		return stubBackend{}
	})
}

func (stubBackend) Name() string                                              { return "fileaccess-stub" }
func (stubBackend) Run(ctx context.Context, _ chan<- FileEvent) error {
	<-ctx.Done()
	return nil
}
