package logs

import (
	"bufio"
	"context"
	"encoding/json"
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
	defer func() { _ = watcher.Close() }()

	for _, p := range c.cfg.Paths {
		if err := watcher.Add(filepath.Dir(p)); err != nil {
			c.logger.Warn("watcher add failed", zap.String("path", p), zap.Error(err))
		}
	}

	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	state := loadFileCursorState(c.cfg.CursorStateFile, c.logger)

	processFile := func(path string) {
		info, err := os.Stat(path)
		if err != nil {
			c.logger.Debug("stat log file", zap.String("path", path), zap.Error(err))
			return
		}
		identity := fileIdentity(path, info)
		lastOffset := state.Offset(path, identity, info.Size())
		file, err := os.Open(path)
		if err != nil {
			c.logger.Debug("open log file", zap.String("path", path), zap.Error(err))
			return
		}
		defer func() { _ = file.Close() }()

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
			state.Set(path, identity, offset, info.Size())
			if err := state.Save(c.cfg.CursorStateFile); err != nil {
				c.logger.Debug("save log cursor state", zap.String("path", path), zap.Error(err))
			}
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

type fileCursorState struct {
	Cursors map[string]fileCursor `json:"cursors"`
}

type fileCursor struct {
	Path      string `json:"path"`
	Identity  string `json:"identity"`
	Offset    int64  `json:"offset"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updated_at"`
}

func loadFileCursorState(path string, logger *zap.Logger) *fileCursorState {
	state := &fileCursorState{Cursors: map[string]fileCursor{}}
	path = strings.TrimSpace(path)
	if path == "" {
		return state
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && logger != nil {
			logger.Debug("read log cursor state", zap.String("path", path), zap.Error(err))
		}
		return state
	}
	if err := json.Unmarshal(data, state); err != nil && logger != nil {
		logger.Debug("decode log cursor state", zap.String("path", path), zap.Error(err))
	}
	if state.Cursors == nil {
		state.Cursors = map[string]fileCursor{}
	}
	return state
}

func (s *fileCursorState) Offset(path, identity string, size int64) int64 {
	if s == nil {
		return 0
	}
	if cur, ok := s.Cursors[identity]; ok && cur.Offset > 0 {
		if cur.Offset <= size {
			return cur.Offset
		}
		return 0
	}
	if strings.TrimSpace(identity) != "" && !strings.HasPrefix(identity, "path:") {
		return 0
	}
	fallbackKey := "path:" + filepath.Clean(path)
	if cur, ok := s.Cursors[fallbackKey]; ok && cur.Offset > 0 {
		if cur.Offset <= size {
			return cur.Offset
		}
	}
	return 0
}

func (s *fileCursorState) Set(path, identity string, offset, size int64) {
	if s == nil {
		return
	}
	if s.Cursors == nil {
		s.Cursors = map[string]fileCursor{}
	}
	if strings.TrimSpace(identity) == "" {
		identity = "path:" + filepath.Clean(path)
	}
	cur := fileCursor{
		Path:      filepath.Clean(path),
		Identity:  identity,
		Offset:    offset,
		Size:      size,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.Cursors[identity] = cur
	s.Cursors["path:"+filepath.Clean(path)] = cur
}

func (s *fileCursorState) Save(path string) error {
	path = strings.TrimSpace(path)
	if s == nil || path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".control-one-cursors-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTmp = false
	return nil
}
