package connect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/masterzen/winrm"
)

// WinRMConnector tests WinRM reachability against a Windows host.
type WinRMConnector struct{}

func NewWinRMConnector() *WinRMConnector { return &WinRMConnector{} }

func (c *WinRMConnector) Name() Protocol { return ProtoWinRM }

func (c *WinRMConnector) Test(ctx context.Context, t Target) (*Probe, error) {
	if t.Host == "" {
		return nil, errors.New("winrm: host required")
	}
	if t.Auth != AuthPassword {
		return nil, errors.New("winrm: only password auth is supported in this build")
	}
	if t.Username == "" || t.Password == "" {
		return nil, errors.New("winrm: username and password required")
	}
	port := t.Port
	if port == 0 {
		port = DefaultPort(ProtoWinRM, t.HTTPS)
	}

	endpoint := winrm.NewEndpoint(t.Host, port, t.HTTPS, t.SkipVerify, nil, nil, nil, applyTimeout(t.Timeout))
	client, err := winrm.NewClient(endpoint, t.Username, t.Password)
	if err != nil {
		return nil, fmt.Errorf("winrm: %w", err)
	}

	start := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, applyTimeout(t.Timeout))
	defer cancel()

	// Single round-trip pulls everything we need: hostname, OS, arch, PS
	// version. Output one line per field for trivial parsing.
	script := `$h = $env:COMPUTERNAME
$os = (Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).Caption
$ver = (Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue).Version
$arch = $env:PROCESSOR_ARCHITECTURE
$psv = $PSVersionTable.PSVersion.ToString()
"hostname=$h"; "os=$os"; "version=$ver"; "arch=$arch"; "powershell=$psv"`

	stdout, stderr, code, err := client.RunPSWithContext(cmdCtx, script)
	if err != nil {
		return nil, fmt.Errorf("winrm exec: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("winrm exit %d: %s", code, strings.TrimSpace(stderr))
	}

	probe := &Probe{
		Reachable:    true,
		LatencyMs:    time.Since(start).Milliseconds(),
		OS:           "windows",
		Capabilities: []string{"winrm", "powershell"},
		Detected:     time.Now().UTC(),
	}
	for _, line := range strings.Split(stdout, "\n") {
		k, v, ok := splitOnce(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "hostname":
			probe.Hostname = v
		case "os":
			probe.OSVersion = v
		case "version":
			if probe.OSVersion == "" {
				probe.OSVersion = v
			} else {
				probe.OSVersion = probe.OSVersion + " (" + v + ")"
			}
		case "arch":
			probe.Architecture = v
		case "powershell":
			probe.Banner = "PowerShell " + v
		}
	}
	return probe, nil
}

func splitOnce(s, sep string) (string, string, bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}
