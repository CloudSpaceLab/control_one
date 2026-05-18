//go:build aix

package main

import (
	"fmt"
	"os"
	"os/exec"
)

const aixServiceName = "controlone-agent"

func init() {
	uninstallServiceHook = uninstallService
}

func installService(configPath string) error {
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "/usr/local/bin/controlone-agent"
	}
	_ = exec.Command("stopsrc", "-s", aixServiceName).Run()
	_ = exec.Command("rmssys", "-s", aixServiceName).Run()
	if err := exec.Command(
		"mkssys",
		"-s", aixServiceName,
		"-p", binaryPath,
		"-u", "0",
		"-S",
		"-n", "15",
		"-f", "9",
		"-a", "--config "+configPath,
	).Run(); err != nil {
		return fmt.Errorf("mkssys %s: %w", aixServiceName, err)
	}
	if err := exec.Command("startsrc", "-s", aixServiceName).Run(); err != nil {
		return fmt.Errorf("startsrc %s: %w", aixServiceName, err)
	}
	return nil
}

func uninstallService() error {
	_ = exec.Command("stopsrc", "-s", aixServiceName).Run()
	_ = exec.Command("rmssys", "-s", aixServiceName).Run()
	return nil
}
