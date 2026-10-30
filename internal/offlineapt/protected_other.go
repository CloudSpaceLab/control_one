//go:build !linux

package offlineapt

import "fmt"

func protectedPath(string) error {
	return fmt.Errorf("offline installation is supported only on qualified Linux hosts")
}
func lockStaging(string) (func(), error) {
	return nil, fmt.Errorf("offline installation is supported only on qualified Linux hosts")
}
