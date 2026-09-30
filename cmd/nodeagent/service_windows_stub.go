//go:build !windows

package main

// runAsWindowsService is only meaningful on Windows. Other operating systems
// use their native service manager integration or run the agent directly.
func runAsWindowsService() bool { return false }
