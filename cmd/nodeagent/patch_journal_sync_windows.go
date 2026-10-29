package main

// Windows directory-flush/replacement durability needs its own qualification.
// Retain the existing Windows file-sync behavior; do not claim Unix guarantees.
func syncPatchJournalDirectory(string) error { return nil }
