package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type squidCommand func(context.Context, ...string) error

func squidRun(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "squid", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("squid %v: %w: %s", args, err, tailString(string(out), 2048))
	}
	return nil
}

func squidReady(ctx context.Context) error {
	if err := squidRun(ctx, "-k", "check"); err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", "127.0.0.1:3128")
	if err != nil {
		return fmt.Errorf("local Squid listener unavailable: %w", err)
	}
	return conn.Close()
}

// replaceSquidConfig validates with the target binary before atomic replacement.
// A reload/readiness failure restores and reloads the old configuration. Both
// failures are returned if recovery is itself unsuccessful; never claim success.
func replaceSquidConfig(ctx context.Context, path string, body []byte, run squidCommand, ready func(context.Context) error) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("existing Squid configuration must be a regular file")
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(path), ".controlone-squid-*.conf")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if _, err = stage.Write(body); err == nil {
		err = preserveProxyOwner(stage, info)
	}
	if err == nil {
		err = stage.Chmod(info.Mode().Perm())
	}
	if err == nil {
		err = stage.Sync()
	}
	closeErr := stage.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	if err = run(ctx, "-k", "parse", "-f", stagePath); err != nil {
		return fmt.Errorf("candidate configuration rejected: %w", err)
	}
	if err = os.Rename(stagePath, path); err != nil {
		return err
	}
	if err = syncPatchJournalDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err = run(ctx, "-k", "reconfigure"); err == nil {
		err = ready(ctx)
	}
	if err == nil {
		return nil
	}
	// The request context may have expired. Recovery has its own bounded budget.
	recoverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	rollback, rollbackErr := os.CreateTemp(filepath.Dir(path), ".controlone-squid-rollback-*.conf")
	if rollbackErr == nil {
		name := rollback.Name()
		defer os.Remove(name)
		_, rollbackErr = rollback.Write(old)
		if rollbackErr == nil {
			rollbackErr = preserveProxyOwner(rollback, info)
		}
		if rollbackErr == nil {
			rollbackErr = rollback.Chmod(info.Mode().Perm())
		}
		if rollbackErr == nil {
			rollbackErr = rollback.Sync()
		}
		rollbackErr = errors.Join(rollbackErr, rollback.Close())
		if rollbackErr == nil {
			rollbackErr = os.Rename(name, path)
		}
		if rollbackErr == nil {
			rollbackErr = syncPatchJournalDirectory(filepath.Dir(path))
		}
		if rollbackErr == nil {
			rollbackErr = run(recoverCtx, "-k", "reconfigure")
		}
		if rollbackErr == nil {
			rollbackErr = ready(recoverCtx)
		}
	}
	if rollbackErr != nil {
		return fmt.Errorf("configuration activation failed: %w; rollback failed: %v; operator recovery required", err, rollbackErr)
	}
	return fmt.Errorf("configuration activation failed: %w; previous configuration restored", err)
}
