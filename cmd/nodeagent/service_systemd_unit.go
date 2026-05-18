package main

import "fmt"

func systemdUnit(binaryPath, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Control One Node Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s --config %s
Restart=always
RestartSec=10
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, binaryPath, configPath)
}
