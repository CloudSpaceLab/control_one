package netflow

import (
	"runtime"

	"go.uber.org/zap"
)

// pickCollector chooses the strongest backend available at runtime. The
// platform-specific files register their constructor here via init():
//
//   - collector_linux_ebpf.go (build tag linux,cgo,ebpf): registers ebpfBackend
//   - collector_linux_proc.go (build tag linux): registers procBackend
//   - collector_windows.go    (build tag windows):    registers winBackend
//   - collector_darwin.go     (build tag darwin):     registers darwinBackend
//
// We try them in priority order; first non-nil non-error wins. On unsupported
// OSes the fallback is a stub `nilBackend` that returns a placeholder which
// the manager logs and otherwise ignores.
var collectorFactories []collectorFactory

type collectorFactory struct {
	priority int
	build    func(opts Options, log *zap.Logger) Collector
}

func registerCollector(priority int, build func(opts Options, log *zap.Logger) Collector) {
	collectorFactories = append(collectorFactories, collectorFactory{priority: priority, build: build})
}

func pickCollector(log *zap.Logger, opts Options) Collector {
	// Sort high-priority first.
	for i := 0; i < len(collectorFactories); i++ {
		for j := i + 1; j < len(collectorFactories); j++ {
			if collectorFactories[j].priority > collectorFactories[i].priority {
				collectorFactories[i], collectorFactories[j] = collectorFactories[j], collectorFactories[i]
			}
		}
	}
	for _, f := range collectorFactories {
		if c := f.build(opts, log); c != nil {
			return c
		}
	}
	if log != nil {
		log.Info("netflow: no collector backend registered for this build", zap.String("goos", runtime.GOOS))
	}
	return nil
}
