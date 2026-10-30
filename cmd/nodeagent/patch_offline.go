package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/offlineapt"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

var patchOfflineRoot string

const offlineStagingRoot = "/var/cache/control-one/offline-patches"

func offlineAPTPlatform() bool {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || os.Geteuid() != 0 {
		return false
	}
	body, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(value, "\"")
		}
	}
	return values["ID"] == "ubuntu" && values["VERSION_ID"] == "24.04"
}

// This command stages public package content only. It does not enroll a node,
// change configuration, install packages or bypass server approval.
func runOfflineStage(args []string, output io.Writer) error {
	fs := flag.NewFlagSet("offline-stage", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "existing node configuration")
	repository := fs.String("repository", "", "local signed APT repository directory")
	keyring := fs.String("keyring", "/usr/share/keyrings/ubuntu-archive-keyring.gpg", "trusted operator keyring")
	pin := fs.String("keyring-sha256", "", "trusted keyring SHA-256 pin")
	suite := fs.String("suite", "noble-updates", "repository suite")
	component := fs.String("component", "main", "repository component")
	packages := fs.String("packages", "", "comma-separated exact name=version selections")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !offlineAPTPlatform() {
		return fmt.Errorf("offline staging requires root on Ubuntu 24.04 amd64")
	}
	if *cfgPath == "" || *repository == "" || *pin == "" || *packages == "" || len(fs.Args()) != 0 {
		return fmt.Errorf("config, repository, trusted keyring-sha256 and exact packages are required")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if cfg.StateFile == "" {
		return fmt.Errorf("existing node state path is required")
	}
	base := offlineStagingRoot
	if err = os.MkdirAll(base, 0755); err != nil {
		return err
	}
	requested := []offlineapt.Selection{}
	for _, raw := range strings.Split(*packages, ",") {
		name, version, ok := strings.Cut(raw, "=")
		if !ok {
			return fmt.Errorf("each package must be name=version")
		}
		requested = append(requested, offlineapt.Selection{Name: name, Version: version})
	}
	snapshot, err := offlineapt.Stage(offlineapt.Spec{Repository: *repository, Keyring: *keyring, KeyringSHA256: *pin, WorkRoot: base, Suite: *suite, Component: *component, Architecture: "amd64"}, requested)
	if err != nil {
		return err
	}
	defer snapshot.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	identity, err := snapshot.Publish(ctx, base)
	if err != nil {
		return err
	}
	if err = offlineapt.PublishSelection(base, identity); err != nil {
		return fmt.Errorf("publish offline plan selection: %w", err)
	}
	enc := json.NewEncoder(output)
	enc.SetIndent("", "  ")
	return enc.Encode(identity)
}

func heartbeatPublishedOfflinePlan(ctx context.Context) *patchsupport.PublishedPlanReport {
	if !offlineAPTPlatform() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	report := offlineapt.PublishedSelection(ctx, offlineStagingRoot)
	return &report
}
