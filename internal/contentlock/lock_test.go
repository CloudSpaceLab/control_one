package contentlock

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContentLockChildHelper(t *testing.T) {
	root := os.Getenv("CONTROLONE_CONTENT_LOCK_CHILD_ROOT")
	if root == "" {
		t.Skip("isolated child fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := Acquire(ctx, root, true)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestContentLockCoordinatesStandaloneProcesses(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	release, err := Acquire(ctx, root, false)
	require.NoError(t, err)
	second, err := Acquire(ctx, root, false)
	require.NoError(t, err)
	second()
	executable, err := os.Executable()
	require.NoError(t, err)
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	child := exec.CommandContext(deadline, executable, "-test.run=^TestContentLockChildHelper$", "-test.v=true")
	child.Env = append(os.Environ(), "CONTROLONE_CONTENT_LOCK_CHILD_ROOT="+root)
	out, err := child.CombinedOutput()
	release()
	release()
	require.NoError(t, err, string(out))
	writer, err := Acquire(ctx, root, true)
	require.NoError(t, err)
	writer()
}
