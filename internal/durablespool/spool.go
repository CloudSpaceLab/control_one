package durablespool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultPrefix   = "record"
	defaultMaxBytes = int64(256 << 20)
)

var spoolSeq uint64

type Options struct {
	Dir      string
	Prefix   string
	MaxBytes int64
	FileMode os.FileMode
	DirMode  os.FileMode
}

type Spool struct {
	dir      string
	prefix   string
	maxBytes int64
	fileMode os.FileMode
	dirMode  os.FileMode
	dropped  uint64
}

type Record struct {
	Path string
	Size int64
}

type Stats struct {
	Records        int    `json:"records"`
	Bytes          int64  `json:"bytes"`
	MaxBytes       int64  `json:"max_bytes"`
	DroppedRecords uint64 `json:"dropped_records"`
}

func New(opts Options) (*Spool, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return nil, errors.New("spool dir is required")
	}
	prefix := sanitizePrefix(opts.Prefix)
	if prefix == "" {
		prefix = defaultPrefix
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	fileMode := opts.FileMode
	if fileMode == 0 {
		fileMode = 0o600
	}
	dirMode := opts.DirMode
	if dirMode == 0 {
		dirMode = 0o750
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create spool dir: %w", err)
	}
	return &Spool{dir: dir, prefix: prefix, maxBytes: maxBytes, fileMode: fileMode, dirMode: dirMode}, nil
}

func (s *Spool) AppendJSON(value any) (string, error) {
	if s == nil {
		return "", errors.New("spool is nil")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	return s.AppendBytes(data)
}

func (s *Spool) AppendBytes(data []byte) (string, error) {
	if s == nil {
		return "", errors.New("spool is nil")
	}
	if len(data) == 0 {
		return "", errors.New("spool record is empty")
	}
	if int64(len(data)) > s.maxBytes {
		return "", fmt.Errorf("spool record size %d exceeds max bytes %d", len(data), s.maxBytes)
	}
	if err := os.MkdirAll(s.dir, s.dirMode); err != nil {
		return "", err
	}
	if dropped, err := s.enforceBudget(int64(len(data))); err != nil {
		return "", err
	} else if dropped > 0 {
		atomic.AddUint64(&s.dropped, uint64(dropped))
	}
	name := fmt.Sprintf("%s-%s-%d-%06d.json", s.prefix, time.Now().UTC().Format("20060102T150405.000000000Z"), os.Getpid(), atomic.AddUint64(&spoolSeq, 1))
	path := filepath.Join(s.dir, name)
	tmp, err := os.CreateTemp(s.dir, "."+s.prefix+"-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(s.fileMode); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	removeTmp = false
	return path, nil
}

func (s *Spool) Records() ([]Record, error) {
	if s == nil {
		return nil, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, s.prefix+"-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		records = append(records, Record{Path: filepath.Join(s.dir, name), Size: info.Size()})
	}
	sort.Slice(records, func(i, j int) bool {
		return filepath.Base(records[i].Path) < filepath.Base(records[j].Path)
	})
	return records, nil
}

func (s *Spool) Stats() (Stats, error) {
	if s == nil {
		return Stats{}, nil
	}
	records, err := s.Records()
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Records: len(records), MaxBytes: s.maxBytes, DroppedRecords: atomic.LoadUint64(&s.dropped)}
	for _, record := range records {
		stats.Bytes += record.Size
	}
	return stats, nil
}

func (s *Spool) Read(record Record) ([]byte, error) {
	if s == nil {
		return nil, errors.New("spool is nil")
	}
	path, err := s.cleanRecordPath(record.Path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *Spool) Delete(record Record) error {
	if s == nil {
		return nil
	}
	path, err := s.cleanRecordPath(record.Path)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Spool) enforceBudget(incoming int64) (int, error) {
	records, err := s.Records()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, record := range records {
		total += record.Size
	}
	dropped := 0
	for len(records) > 0 && total+incoming > s.maxBytes {
		record := records[0]
		records = records[1:]
		if err := s.Delete(record); err != nil {
			return dropped, err
		}
		total -= record.Size
		dropped++
	}
	return dropped, nil
}

func (s *Spool) cleanRecordPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("spool record path is required")
	}
	cleanDir, err := filepath.Abs(s.dir)
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("spool record %s is outside %s", cleanPath, cleanDir)
	}
	return cleanPath, nil
}

func sanitizePrefix(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_")
}
