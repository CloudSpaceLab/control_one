package contentpacks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

type PackContent struct {
	Manifest     Manifest `json:"manifest"`
	ManifestPath string   `json:"manifest_path"`
	Root         fs.FS    `json:"-"`
}

func ParsePackContent(data []byte) (*PackContent, error) {
	if manifest, err := ParseManifest(data); err == nil {
		return &PackContent{
			Manifest:     *manifest,
			ManifestPath: "manifest.yaml",
			Root:         newMemoryFS(map[string][]byte{"manifest.yaml": cloneBytes(data)}),
		}, nil
	}

	files, archiveErr := readPackArchive(data)
	if archiveErr != nil {
		if _, manifestErr := ParseManifest(data); manifestErr != nil {
			return nil, fmt.Errorf("parse content pack as manifest or archive: manifest: %v; archive: %w", manifestErr, archiveErr)
		}
		return nil, archiveErr
	}
	manifestPath, ok := findPackManifest(files)
	if !ok {
		return nil, fmt.Errorf("content pack archive missing manifest.yaml, manifest.yml, or manifest.json")
	}
	manifest, err := ParseManifest(files[manifestPath])
	if err != nil {
		return nil, fmt.Errorf("parse content pack archive %s: %w", manifestPath, err)
	}
	return &PackContent{
		Manifest:     *manifest,
		ManifestPath: manifestPath,
		Root:         newMemoryFS(files),
	}, nil
}

func ReplayPackContent(ctx context.Context, data []byte, opts SampleReplayOptions) (SampleReplayReport, error) {
	pack, err := ParsePackContent(data)
	if err != nil {
		return SampleReplayReport{}, err
	}
	return ReplayManifestSamples(ctx, pack.Manifest, pack.Root, opts)
}

func readPackArchive(data []byte) (map[string][]byte, error) {
	files, gzipErr := readTarArchive(data, true)
	if gzipErr == nil {
		return files, nil
	}
	files, tarErr := readTarArchive(data, false)
	if tarErr == nil {
		return files, nil
	}
	return nil, fmt.Errorf("gzip tar: %v; tar: %w", gzipErr, tarErr)
}

func readTarArchive(data []byte, gzipped bool) (map[string][]byte, error) {
	var reader io.Reader = bytes.NewReader(data)
	if gzipped {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}
	tr := tar.NewReader(reader)
	files := map[string][]byte{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		name, err := cleanPackPath(header.Name)
		if err != nil {
			return nil, fmt.Errorf("unsafe content pack path %q: %w", header.Name, err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read content pack file %s: %w", name, err)
		}
		files[name] = body
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("archive has no files")
	}
	return files, nil
}

func findPackManifest(files map[string][]byte) (string, bool) {
	candidates := []string{"manifest.yaml", "manifest.yml", "manifest.json"}
	for _, candidate := range candidates {
		if _, ok := files[candidate]; ok {
			return candidate, true
		}
	}
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		base := path.Base(key)
		switch base {
		case "manifest.yaml", "manifest.yml", "manifest.json":
			return key, true
		}
	}
	return "", false
}

type memoryFS struct {
	files map[string][]byte
}

func newMemoryFS(files map[string][]byte) memoryFS {
	out := memoryFS{files: make(map[string][]byte, len(files))}
	for name, data := range files {
		out.files[name] = cloneBytes(data)
	}
	return out
}

func (m memoryFS) Open(name string) (fs.File, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "./")
	if name == "" || name == "." {
		return nil, fs.ErrNotExist
	}
	data, ok := m.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &memoryFile{
		name:   path.Base(name),
		reader: bytes.NewReader(data),
		size:   int64(len(data)),
	}, nil
}

type memoryFile struct {
	name   string
	reader *bytes.Reader
	size   int64
}

func (f *memoryFile) Stat() (fs.FileInfo, error) {
	return memoryFileInfo{name: f.name, size: f.size}, nil
}

func (f *memoryFile) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}

func (f *memoryFile) Close() error {
	return nil
}

type memoryFileInfo struct {
	name string
	size int64
}

func (i memoryFileInfo) Name() string       { return i.name }
func (i memoryFileInfo) Size() int64        { return i.size }
func (i memoryFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i memoryFileInfo) ModTime() time.Time { return time.Time{} }
func (i memoryFileInfo) IsDir() bool        { return false }
func (i memoryFileInfo) Sys() any           { return nil }

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
