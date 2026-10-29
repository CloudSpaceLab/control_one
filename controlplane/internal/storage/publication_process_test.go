package storage

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPublicationChildProcessHelper(t *testing.T) {
	mode := os.Getenv("CONTROLONE_PUBLICATION_CHILD")
	if mode == "" {
		t.Skip("isolated child-process fixture only")
	}
	s := phase2SnapshotTestStore(t)
	if mode == "blocked" {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		_, err := s.AcquireVulnerabilityPublication(ctx, true)
		require.Error(t, err)
		fmt.Println("PUBLICATION_BLOCKED_OK")
		return
	}
	release, err := s.AcquireVulnerabilityPublication(context.Background(), true)
	require.NoError(t, err)
	defer release()
	fmt.Println("PUBLICATION_LEASE_ACQUIRED")
	var input [1]byte
	_, _ = io.ReadFull(os.Stdin, input[:])
}

func TestPublicationIndependentProcessCrashAndPoolPressure(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	executable, err := os.Executable()
	require.NoError(t, err)
	reader, err := s.AcquireVulnerabilityPublication(ctx, false)
	require.NoError(t, err)
	childCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	child := exec.CommandContext(childCtx, executable, "-test.run=^TestPublicationChildProcessHelper$", "-test.v=true")
	child.Env = append(os.Environ(), "CONTROLONE_PUBLICATION_CHILD=blocked")
	output, err := child.CombinedOutput()
	cancel()
	reader()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "PUBLICATION_BLOCKED_OK")
	childCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	child = exec.CommandContext(childCtx, executable, "-test.run=^TestPublicationChildProcessHelper$", "-test.v=true")
	child.Env = append(os.Environ(), "CONTROLONE_PUBLICATION_CHILD=crash")
	stdin, err := child.StdinPipe()
	require.NoError(t, err)
	defer stdin.Close()
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	require.NoError(t, child.Start())
	acquired := false
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "PUBLICATION_LEASE_ACQUIRED") {
			acquired = true
			break
		}
	}
	killErr := child.Process.Kill()
	waitErr := child.Wait()
	// Wait joins os/exec's stderr copier before reading its buffer.
	require.True(t, acquired, stderr.String())
	require.NoError(t, killErr)
	require.Error(t, waitErr)
	recovery, recoveryCancel := context.WithTimeout(ctx, 2*time.Second)
	defer recoveryCancel()
	release, err := s.AcquireVulnerabilityPublication(recovery, true)
	require.NoError(t, err)
	require.NoError(t, s.SetVulnerabilityPublication(ctx, "pool-pressure-fixture"))
	release()
	pressure, pressureCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pressureCancel()
	var wg sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := s.AcquireVulnerabilityPublication(pressure, false)
			if err != nil {
				results <- err
				return
			}
			defer release()
			results <- s.CheckVulnerabilityPublication(pressure, "pool-pressure-fixture")
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err, "assessment leases must leave database capacity for their work")
	}
}
