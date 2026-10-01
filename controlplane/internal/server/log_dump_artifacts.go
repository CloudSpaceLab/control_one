package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	defaultLogDumpRoot      = "/var/lib/control-one/log-dumps"
	logDumpDirEnv           = "CONTROL_ONE_LOG_DUMPS_DIR"
	maxLogDumpArtifactBytes = int64(32 << 20) // 32 MiB
	maxLogDumpChunkBytes    = int64(4 << 20)  // 4 MiB
)

type logDumpArtifactResult struct {
	Path      string
	SHA256    string
	SizeBytes int64
}

func logDumpRootDir() string {
	if v := strings.TrimSpace(os.Getenv(logDumpDirEnv)); v != "" {
		return filepath.Clean(v)
	}
	return defaultLogDumpRoot
}

func ensureLogDumpRoot() (string, error) {
	root := logDumpRootDir()
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", fmt.Errorf("create log dump root: %w", err)
	}
	if err := os.Chmod(root, 0o750); err != nil {
		return "", fmt.Errorf("secure log dump root: %w", err)
	}
	return root, nil
}

func writeLogDumpArtifactAtomic(tenantID, nodeID, dumpID uuid.UUID, write func(io.Writer) error) (logDumpArtifactResult, error) {
	root, err := ensureLogDumpRoot()
	if err != nil {
		return logDumpArtifactResult{}, err
	}
	dir := filepath.Join(root, tenantID.String(), nodeID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return logDumpArtifactResult{}, fmt.Errorf("create log dump directory: %w", err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		return logDumpArtifactResult{}, fmt.Errorf("secure log dump directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+dumpID.String()+"-*.tmp")
	if err != nil {
		return logDumpArtifactResult{}, fmt.Errorf("create log dump temp artifact: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o640); err != nil {
		return logDumpArtifactResult{}, fmt.Errorf("secure log dump temp artifact: %w", err)
	}

	h := sha256.New()
	limited := &hardLimitWriter{w: io.MultiWriter(tmp, h), remaining: maxLogDumpArtifactBytes}
	if err := write(limited); err != nil {
		return logDumpArtifactResult{}, err
	}
	if err := tmp.Sync(); err != nil {
		return logDumpArtifactResult{}, fmt.Errorf("sync log dump artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return logDumpArtifactResult{}, fmt.Errorf("close log dump artifact: %w", err)
	}
	finalPath := filepath.Join(dir, dumpID.String()+".ndjson")
	if err := os.Rename(tmpName, finalPath); err != nil {
		return logDumpArtifactResult{}, fmt.Errorf("publish log dump artifact: %w", err)
	}
	committed = true
	return logDumpArtifactResult{
		Path:      finalPath,
		SHA256:    hex.EncodeToString(h.Sum(nil)),
		SizeBytes: maxLogDumpArtifactBytes - limited.remaining,
	}, nil
}

type hardLimitWriter struct {
	w         io.Writer
	remaining int64
}

func (w *hardLimitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("log dump exceeds %d bytes", maxLogDumpArtifactBytes)
	}
	n, err := w.w.Write(p)
	w.remaining -= int64(n)
	return n, err
}

func writeLogDumpChunk(dumpID, jobID uuid.UUID, ordinal int, src io.Reader, expectedSHA string) (path string, size int64, sha string, err error) {
	if ordinal < 0 || ordinal > 10000 {
		return "", 0, "", errors.New("invalid chunk ordinal")
	}
	root, err := ensureLogDumpRoot()
	if err != nil {
		return "", 0, "", err
	}
	dir := filepath.Join(root, ".chunks", dumpID.String(), jobID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", 0, "", fmt.Errorf("create log dump chunk directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+strconv.Itoa(ordinal)+"-*.tmp")
	if err != nil {
		return "", 0, "", fmt.Errorf("create log dump chunk: %w", err)
	}
	path = tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := tmp.Chmod(0o640); err != nil {
		return "", 0, "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, maxLogDumpChunkBytes+1))
	if err != nil {
		return "", 0, "", fmt.Errorf("write log dump chunk: %w", err)
	}
	if n > maxLogDumpChunkBytes {
		return "", 0, "", fmt.Errorf("log dump chunk exceeds %d bytes", maxLogDumpChunkBytes)
	}
	sha = hex.EncodeToString(h.Sum(nil))
	if sha != strings.ToLower(strings.TrimSpace(expectedSHA)) {
		return "", 0, "", errors.New("log dump chunk checksum mismatch")
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, "", err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, "", err
	}
	keep = true
	return path, n, sha, nil
}

type logDumpChunkArtifact struct {
	Ordinal   int
	Path      string
	SHA256    string
	SizeBytes int64
}

func assembleLogDumpChunks(tenantID, nodeID, dumpID uuid.UUID, chunks []logDumpChunkArtifact, expectedSHA string) (logDumpArtifactResult, error) {
	if len(chunks) > 10000 {
		return logDumpArtifactResult{}, errors.New("too many log dump chunks")
	}
	var expectedOrdinal int
	var total int64
	for _, c := range chunks {
		if c.Ordinal != expectedOrdinal {
			return logDumpArtifactResult{}, fmt.Errorf("missing log dump chunk %d", expectedOrdinal)
		}
		if c.SizeBytes < 0 || c.SizeBytes > maxLogDumpChunkBytes || total+c.SizeBytes > maxLogDumpArtifactBytes {
			return logDumpArtifactResult{}, errors.New("log dump artifact size limit exceeded")
		}
		expectedOrdinal++
		total += c.SizeBytes
	}
	result, err := writeLogDumpArtifactAtomic(tenantID, nodeID, dumpID, func(dst io.Writer) error {
		for _, c := range chunks {
			f, err := openScopedLogDumpFile(c.Path)
			if err != nil {
				return fmt.Errorf("open log dump chunk %d: %w", c.Ordinal, err)
			}
			h := sha256.New()
			n, copyErr := io.Copy(io.MultiWriter(dst, h), io.LimitReader(f, c.SizeBytes+1))
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if n != c.SizeBytes || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
				return fmt.Errorf("log dump chunk %d changed after upload", c.Ordinal)
			}
		}
		return nil
	})
	if err != nil {
		return logDumpArtifactResult{}, err
	}
	if expected := strings.ToLower(strings.TrimSpace(expectedSHA)); expected != "" && result.SHA256 != expected {
		_ = os.Remove(result.Path)
		return logDumpArtifactResult{}, errors.New("log dump artifact checksum mismatch")
	}
	return result, nil
}

func openScopedLogDumpFile(storedPath string) (*os.File, error) {
	root, err := ensureLogDumpRoot()
	if err != nil {
		return nil, err
	}
	clean := filepath.Clean(strings.TrimSpace(storedPath))
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("invalid log dump artifact path")
	}
	f, err := os.Open(clean)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("log dump artifact is not a regular file")
	}
	return f, nil
}

func removeScopedLogDumpFile(storedPath string) error {
	if strings.TrimSpace(storedPath) == "" {
		return nil
	}
	f, err := openScopedLogDumpFile(storedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func removeLogDumpChunkFiles(dumpID uuid.UUID) error {
	root, err := ensureLogDumpRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, ".chunks", dumpID.String())
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errors.New("invalid log dump chunk directory")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove log dump chunks: %w", err)
	}
	return nil
}
