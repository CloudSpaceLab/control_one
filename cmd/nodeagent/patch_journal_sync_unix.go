//go:build !windows

package main

import "os"

// A synced temporary file alone does not durably publish its replacement name.
func syncPatchJournalDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
