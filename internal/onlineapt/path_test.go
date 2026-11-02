package onlineapt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalPathRejectsShellExpansion(t *testing.T) {
	for _, path := range []string{"/tmp/$(id)", "/tmp/`id`", "/tmp/file;id", "/tmp/file|id", "/tmp/file&", "/tmp/file'", "/tmp/file\n", "/tmp/file space", "/tmp/file>output", "/tmp/file*"} {
		t.Run(path, func(t *testing.T) { require.ErrorContains(t, canonicalPath(path), "canonical local paths") })
	}
}
