package logs

import (
    "context"
    "errors"
    "fmt"
    "sync"

    "github.com/CloudSpaceLab/control_one/internal/config"
    "go.uber.org/zap"
)

// Formatter transforms a raw log into a structured representation.
type Formatter interface {
    Format(raw RawLog, source config.LogSourceConfig) (StructuredLog, error)
}

// Collector pulls logs from a particular source definition.
type Collector interface {
    Run(ctx context.Context, out chan<- RawLog) error
}

// CollectorFactory constructs a collector for a log source.
type CollectorFactory func(cfg config.LogSourceConfig, logger *zap.Logger) (Collector, error)

var (
    formatterMu sync.RWMutex
    formatters  = map[string]Formatter{}

    collectorMu sync.RWMutex
    collectors  = map[string]CollectorFactory{}

    errUnknownCollector = errors.New("unknown collector type")
)

// RegisterFormatter registers a formatter by name.
func RegisterFormatter(name string, formatter Formatter) {
    formatterMu.Lock()
    defer formatterMu.Unlock()
    formatters[name] = formatter
}

// GetFormatter retrieves a formatter by name, falling back to the default formatter.
func GetFormatter(name string) Formatter {
    formatterMu.RLock()
    defer formatterMu.RUnlock()
    if name != "" {
        if f, ok := formatters[name]; ok {
            return f
        }
    }
    if f, ok := formatters["default"]; ok {
        return f
    }
    return defaultFormatter{}
}

// RegisterCollectorFactory registers a collector factory by type string.
func RegisterCollectorFactory(typ string, factory CollectorFactory) {
    collectorMu.Lock()
    defer collectorMu.Unlock()
    collectors[typ] = factory
}

// NewCollector constructs a collector for the provided log source.
func NewCollector(cfg config.LogSourceConfig, logger *zap.Logger) (Collector, error) {
    typ := cfg.Type
    if typ == "" || typ == "auto" {
        if len(cfg.Paths) > 0 {
            typ = "file"
        }
    }
    collectorMu.RLock()
    factory, ok := collectors[typ]
    collectorMu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("%w: %s", errUnknownCollector, typ)
    }
    return factory(cfg, logger)
}
