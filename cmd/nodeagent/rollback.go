package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// runRollback is the operator escape hatch for a bad self-update. It swaps
// <exe>.prev (saved in TriggerUpdate before the rename) back over the
// running binary and exits — the service manager will restart with the old
// agent. No automatic post-update health check is wired in PR 4a, so this
// is a manual recovery action invoked over SSH or via a future control-plane
// command channel.
//
// Usage:
//
//	nodeagent rollback                  # roll back the agent at os.Executable()
//	nodeagent rollback --exe /path      # roll back a specific binary path
//
// Exit codes:
//
//	0 — rollback applied
//	1 — usage error or .prev missing
//	2 — copy/rename failed
func runRollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	exeFlag := fs.String("exe", "", "binary path to roll back (defaults to running executable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe := *exeFlag
	if exe == "" {
		resolved, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		exe = resolved
	}

	prevPath := exe + ".prev"
	info, err := os.Stat(prevPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("nothing to roll back: %s does not exist", prevPath)
		}
		return fmt.Errorf("stat .prev: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("nothing to roll back: %s is empty", prevPath)
	}

	if err := swapPrev(exe, prevPath); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "rolled back %s from %s (%d bytes)\n", exe, prevPath, info.Size())
	return nil
}

// swapPrev copies prev → exe via a temp file alongside exe so the final
// rename is on the same filesystem (rename(2) atomic). Removes .prev on
// success — leaving a stale .prev around would confuse the next rollback.
func swapPrev(exe, prevPath string) error {
	src, err := os.Open(prevPath) // #nosec G304 — operator-supplied path.
	if err != nil {
		return fmt.Errorf("open .prev: %w", err)
	}
	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp(dirOf(exe), "controlone-agent-rollback-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy .prev to temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp over exe: %w", err)
	}
	if err := os.Remove(prevPath); err != nil {
		// Non-fatal — the swap is done; we just leave a stale .prev.
		// Caller stdout shows success; the next rollback will fail loudly
		// if .prev is empty/invalid, prompting the operator to clean up.
		fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", prevPath, err)
	}
	return nil
}

// dirOf is filepath.Dir without importing filepath (keeps this file's
// import set tiny — main.go already pulls filepath).
func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
