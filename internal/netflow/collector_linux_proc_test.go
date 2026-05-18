//go:build linux

package netflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcNetSourcesDedupesNetworkNamespaces(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "net"))
	mustMkdir(t, filepath.Join(root, "self", "ns"))
	if err := os.Symlink("net:[1]", filepath.Join(root, "self", "ns", "net")); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []string{"100", "101"} {
		mustMkdir(t, filepath.Join(root, pid, "ns"))
		mustMkdir(t, filepath.Join(root, pid, "net"))
		if err := os.Symlink("net:[1]", filepath.Join(root, pid, "ns", "net")); err != nil {
			t.Fatal(err)
		}
	}
	mustMkdir(t, filepath.Join(root, "200", "ns"))
	mustMkdir(t, filepath.Join(root, "200", "net"))
	if err := os.Symlink("net:[2]", filepath.Join(root, "200", "ns", "net")); err != nil {
		t.Fatal(err)
	}

	got := procNetSources(root)
	if len(got) != 2 {
		t.Fatalf("sources = %d, want 2: %#v", len(got), got)
	}
	if got[0].netns != "net:[1]" || got[0].dirs[0] != filepath.Join(root, "net") {
		t.Fatalf("self namespace should be first with /proc/net fallback: %#v", got[0])
	}
	if got[1].netns != "net:[2]" {
		t.Fatalf("second namespace = %q, want net:[2]", got[1].netns)
	}
}

func TestReadProcNetFromSourceFallsBackToNextRepresentative(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	mustMkdir(t, first)
	mustMkdir(t, second)
	const sample = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 0200007F:01BB 01 00000000:00000000 00:00000000 00000000 1000 0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(filepath.Join(second, "tcp"), []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := readProcNetFromSource(procNetSource{netns: "net:[2]", dirs: []string{first, second}}, "tcp", "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].srcIP != "127.0.0.1" || entries[0].dstIP != "127.0.0.2" || entries[0].inode != 12345 {
		t.Fatalf("unexpected entry: %#v", entries[0])
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}
