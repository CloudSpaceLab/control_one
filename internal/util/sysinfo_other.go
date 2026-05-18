//go:build !linux && !darwin && !windows

package util

func readMachineID() (string, error) {
	return "", nil
}
