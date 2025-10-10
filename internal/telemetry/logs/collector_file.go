package logs

import (
    "bufio"
    "context"
    "errors"
    "io"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/fsnotify/fsnotify"

    "github.com/CloudSpaceLab/control_one/internal/config"
    "go.uber.org/zap"
)

// fileCollector tails files defined in log source configuration.
type fileCollector struct {
    cfg    config.LogSourceConfig
    logger *zap.Logger
}

// NewFileCollector creates a file collector for the provided configuration.
func NewFileCollector(cfg config.LogSourceConfig, logger *zap.Logger) (Collector, error) {
    if len(cfg.Paths) == 0 {
        return nil, errors.New("file collector requires paths")
    }
    return &fileCollector{cfg: cfg, logger: logger}, nil
}

func (c *fileCollector) Run(ctx context.Context, out chan<- RawLog) error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }
    defer watcher.Close()

    for _, p := range c.cfg.Paths {
        if err := watcher.Add(filepath.Dir(p)); err != nil {
            c.logger.Warn("watcher add failed", zap.String("path", p), zap.Error(err))
        }
    }

    ticker := time.NewTicker(c.cfg.PollInterval)
    defer ticker.Stop()

    state := make(map[string]int64)

    processFile := func(path string) {
        lastOffset := state[path]
        file, err := os.Open(path)
        if err != nil {
            c.logger.Debug("open log file", zap.String("path", path), zap.Error(err))
            return
        }
        defer file.Close()

        if lastOffset > 0 {
            if _, err := file.Seek(lastOffset, io.SeekStart); err != nil {
                c.logger.Debug("seek log file", zap.String("path", path), zap.Error(err))
            }
        }

        reader := bufio.NewReader(file)
        for {
            line, err := reader.ReadString('\n')
            if len(line) > 0 {
                trimmed := strings.TrimRight(line, "\r\n")
                select {
                case <-ctx.Done():
                    return
                case out <- RawLog{
                    Timestamp: time.Now().UTC(),
                    Program:   c.cfg.Program,
                    Source:    path,
                    Message:   trimmed,
                }:
                }
            }
            if err != nil {
                if errors.Is(err, io.EOF) {
                    break
                }
                c.logger.Debug("read log file", zap.String("path", path), zap.Error(err))
                break
            }
        }

        if offset, err := file.Seek(0, io.SeekCurrent); err == nil {
            state[path] = offset
        }
    }

    poll := func() {
        for _, p := range c.cfg.Paths {
            processFile(p)
        }
    }

    poll()

    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            poll()
        case event := <-watcher.Events:
            if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
                for _, path := range c.cfg.Paths {
                    if filepath.Clean(path) == filepath.Clean(event.Name) {
                        processFile(path)
                        break
                    }
                }
            }
        case err := <-watcher.Errors:
            c.logger.Debug("watcher error", zap.Error(err))
        }
    }
}

func init() {
    RegisterCollectorFactory("file", NewFileCollector)
}
