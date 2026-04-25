package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// netshBackend talks to the Windows Defender Firewall via netsh advfirewall.
// PowerShell's New-NetFirewallRule would be cleaner but assumes PS Core /
// Windows PowerShell 5.1+; netsh ships everywhere from Windows 7 / Server
// 2008 onward, including Server Core, so it is our default.
type netshBackend struct{}

func (netshBackend) Name() string { return "windows-firewall" }

func (netshBackend) Available() bool {
	if _, err := exec.LookPath("netsh"); err != nil {
		return false
	}
	return true
}

func (b netshBackend) Apply(ctx context.Context, r Rule) error {
	args := buildNetshArgs("add", r)
	return runCmd(ctx, "netsh", args...)
}

func (b netshBackend) Remove(ctx context.Context, r Rule) error {
	name := netshRuleName(r)
	return runCmd(ctx, "netsh", "advfirewall", "firewall", "delete", "rule", "name="+name)
}

func (b netshBackend) List(ctx context.Context, tag string) ([]Rule, error) {
	out, err := exec.CommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("netsh list: %w (%s)", err, string(out))
	}
	rules := []Rule{}
	for _, block := range strings.Split(string(out), "\r\n\r\n") {
		if tag != "" && !strings.Contains(block, tag) {
			continue
		}
		if strings.TrimSpace(block) == "" {
			continue
		}
		rules = append(rules, Rule{Comment: block})
	}
	return rules, nil
}

func buildNetshArgs(verb string, r Rule) []string {
	name := netshRuleName(r)
	dir := "in"
	if r.Direction == DirectionOut {
		dir = "out"
	}
	action := "block"
	if r.Action == ActionAllow {
		action = "allow"
	}
	args := []string{
		"advfirewall", "firewall", verb, "rule",
		"name=" + name,
		"dir=" + dir,
		"action=" + action,
	}
	if r.Protocol != "" {
		args = append(args, "protocol="+r.Protocol)
	}
	if r.Port > 0 {
		args = append(args, "remoteport="+strconv.Itoa(r.Port))
	}
	if r.Source != "" {
		args = append(args, "remoteip="+r.Source)
	}
	if r.Dest != "" {
		args = append(args, "localip="+r.Dest)
	}
	return args
}

// netshRuleName produces a deterministic, valid Windows Firewall rule name.
// netsh rejects names containing "=" or ",".
func netshRuleName(r Rule) string {
	parts := []string{"controlone"}
	if r.Tag != "" {
		parts = append(parts, sanitizeNetshName(r.Tag))
	}
	if r.Source != "" {
		parts = append(parts, sanitizeNetshName(r.Source))
	}
	if r.Port > 0 {
		parts = append(parts, strconv.Itoa(r.Port))
	}
	return strings.Join(parts, "_")
}

func sanitizeNetshName(s string) string {
	s = strings.ReplaceAll(s, "=", "-")
	s = strings.ReplaceAll(s, ",", "-")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
