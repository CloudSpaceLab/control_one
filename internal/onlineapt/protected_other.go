//go:build !linux

package onlineapt

import "fmt"

func protected(string) error      { return fmt.Errorf("exact online APT planning requires Linux") }
func lock(string) (func(), error) { return nil, fmt.Errorf("exact online APT planning requires Linux") }
