package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Fail2BanController is orthogonal to the host firewall: it manages dynamic
// bans inside fail2ban jails. Most operators run fail2ban alongside their
// host firewall, so we keep it as a separate manager rather than another
// Backend in the rotation. The agent calls Ban / Unban from the auto-block
// pipeline when a rule fires repeatedly on the same source IP.
type Fail2BanController struct{}

// Available returns true if fail2ban-client is on PATH and the daemon is up.
func (Fail2BanController) Available() bool {
	if _, err := exec.LookPath("fail2ban-client"); err != nil {
		return false
	}
	out, err := exec.Command("fail2ban-client", "ping").CombinedOutput()
	return err == nil && strings.Contains(string(out), "pong")
}

// Jails returns the configured jails.
func (Fail2BanController) Jails(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "fail2ban-client", "status").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("fail2ban status: %w (%s)", err, string(out))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Jail list:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, nil
		}
		raw := strings.TrimSpace(parts[1])
		if raw == "" {
			return nil, nil
		}
		jails := strings.Split(raw, ",")
		out := make([]string, 0, len(jails))
		for _, j := range jails {
			j = strings.TrimSpace(j)
			if j != "" {
				out = append(out, j)
			}
		}
		return out, nil
	}
	return nil, nil
}

// Ban adds an IP to a jail's ban list.
func (Fail2BanController) Ban(ctx context.Context, jail, ip string) error {
	if jail == "" || ip == "" {
		return fmt.Errorf("jail and ip required")
	}
	return runCmd(ctx, "fail2ban-client", "set", jail, "banip", ip)
}

// Unban removes an IP from a jail.
func (Fail2BanController) Unban(ctx context.Context, jail, ip string) error {
	if jail == "" || ip == "" {
		return fmt.Errorf("jail and ip required")
	}
	return runCmd(ctx, "fail2ban-client", "set", jail, "unbanip", ip)
}

// Banned returns the IPs currently banned in a jail.
func (Fail2BanController) Banned(ctx context.Context, jail string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "fail2ban-client", "status", jail).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("fail2ban status %s: %w", jail, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Banned IP list:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, nil
		}
		raw := strings.TrimSpace(parts[1])
		if raw == "" {
			return nil, nil
		}
		ips := strings.Fields(raw)
		return ips, nil
	}
	return nil, nil
}
