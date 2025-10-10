package logger

import (
	"os"

	"go.uber.org/zap"
)

// New returns a production-ready zap logger writing to stdout.
func New() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stdout"}
	cfg.ErrorOutputPaths = []string{"stderr"}
	cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	return cfg.Build()
}

// Must panics if logger creation fails; useful in tests.
func Must() *zap.Logger {
	log, err := New()
	if err != nil {
		panic(err)
	}
	return log
}

// ReplaceGlobals sets zap global loggers to the given logger.
func ReplaceGlobals(log *zap.Logger) {
	zap.ReplaceGlobals(log)
}

// Sync flushes buffered log entries, ignoring EINVAL on Windows.
func Sync(log *zap.Logger) {
	if err := log.Sync(); err != nil {
		if pathErr, ok := err.(*os.PathError); ok && pathErr.Err.Error() == "sync /dev/stdout: invalid argument" {
			return
		}
	}
}
