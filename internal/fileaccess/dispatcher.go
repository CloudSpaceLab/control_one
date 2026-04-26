package fileaccess

import "go.uber.org/zap"

// Each platform-specific file registers a constructor here. pickFileBackend
// picks the highest-priority non-nil constructor at runtime.
var fileFactories []fileFactory

type fileFactory struct {
	priority int
	build    func(opts Options, log *zap.Logger) Collector
}

func registerFileBackend(priority int, build func(opts Options, log *zap.Logger) Collector) {
	fileFactories = append(fileFactories, fileFactory{priority: priority, build: build})
}

func pickFileBackend(log *zap.Logger, opts Options) Collector {
	// Sort once.
	for i := 0; i < len(fileFactories); i++ {
		for j := i + 1; j < len(fileFactories); j++ {
			if fileFactories[j].priority > fileFactories[i].priority {
				fileFactories[i], fileFactories[j] = fileFactories[j], fileFactories[i]
			}
		}
	}
	for _, f := range fileFactories {
		if c := f.build(opts, log); c != nil {
			return c
		}
	}
	return nil
}
