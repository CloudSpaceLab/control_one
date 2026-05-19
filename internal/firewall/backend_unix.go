package firewall

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// --- ufw ----------------------------------------------------------------

type ufwBackend struct{}

func (ufwBackend) Name() string { return "ufw" }

func (ufwBackend) Available() bool {
	if _, err := exec.LookPath("ufw"); err != nil {
		return false
	}
	out, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		return false
	}
	return ufwStatusActive(string(out))
}

func (b ufwBackend) Apply(ctx context.Context, r Rule) error {
	args := buildUFWArgs(r)
	if len(args) == 0 {
		return ErrUnsupported
	}
	return runCmd(ctx, "ufw", args...)
}

func (b ufwBackend) Remove(ctx context.Context, r Rule) error {
	args := append([]string{"delete"}, buildUFWArgs(r)...)
	return runCmd(ctx, "ufw", args...)
}

func (b ufwBackend) List(ctx context.Context, tag string) ([]Rule, error) {
	out, err := exec.CommandContext(ctx, "ufw", "status", "verbose").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ufw status: %w (%s)", err, string(out))
	}
	rules := []Rule{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if tag != "" && !strings.Contains(line, tag) {
			continue
		}
		// ufw format is non-trivial; surface raw lines as Comment so callers
		// can audit. Full parse would mean reimplementing ufw's pretty-print.
		if line != "" {
			rules = append(rules, Rule{Comment: line})
		}
	}
	return rules, nil
}

func buildUFWArgs(r Rule) []string {
	args := []string{string(r.Action)}
	if r.Direction == DirectionOut {
		args = append(args, "out")
	}
	if r.Source != "" {
		args = append(args, "from", r.Source)
	}
	if r.Dest != "" {
		args = append(args, "to", r.Dest)
	}
	if r.Port > 0 {
		args = append(args, "port", strconv.Itoa(r.Port))
	}
	if r.Protocol != "" {
		args = append(args, "proto", r.Protocol)
	}
	if r.Comment != "" || r.Tag != "" {
		args = append(args, "comment", joinComment(r.Tag, r.Comment))
	}
	return args
}

// --- firewalld ----------------------------------------------------------

type firewalldBackend struct{}

func (firewalldBackend) Name() string { return "firewalld" }

func (firewalldBackend) Available() bool {
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		return false
	}
	out, err := exec.Command("firewall-cmd", "--state").CombinedOutput()
	return err == nil && strings.Contains(string(out), "running")
}

func (b firewalldBackend) Apply(ctx context.Context, r Rule) error {
	rich := buildFirewalldRich(r)
	if rich == "" {
		return ErrUnsupported
	}
	if err := runCmd(ctx, "firewall-cmd", "--add-rich-rule="+rich); err != nil {
		return err
	}
	return runCmd(ctx, "firewall-cmd", "--permanent", "--add-rich-rule="+rich)
}

func (b firewalldBackend) Remove(ctx context.Context, r Rule) error {
	rich := buildFirewalldRich(r)
	if rich == "" {
		return ErrUnsupported
	}
	_ = runCmd(ctx, "firewall-cmd", "--remove-rich-rule="+rich)
	return runCmd(ctx, "firewall-cmd", "--permanent", "--remove-rich-rule="+rich)
}

func (b firewalldBackend) List(ctx context.Context, tag string) ([]Rule, error) {
	out, err := exec.CommandContext(ctx, "firewall-cmd", "--list-rich-rules").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("firewalld list: %w (%s)", err, string(out))
	}
	rules := []Rule{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if tag != "" && !strings.Contains(line, tag) {
			continue
		}
		rules = append(rules, Rule{Comment: line})
	}
	return rules, nil
}

func buildFirewalldRich(r Rule) string {
	if r.Direction == DirectionOut {
		return ""
	}
	parts := []string{"rule"}
	if strings.Contains(r.Source, ":") || strings.Contains(r.Dest, ":") {
		parts = append(parts, "family=ipv6")
	} else {
		parts = append(parts, "family=ipv4")
	}
	if r.Source != "" {
		parts = append(parts, fmt.Sprintf(`source address="%s"`, r.Source))
	}
	if r.Dest != "" {
		parts = append(parts, fmt.Sprintf(`destination address="%s"`, r.Dest))
	}
	if r.Port > 0 {
		proto := r.Protocol
		if proto == "" {
			proto = "tcp"
		}
		parts = append(parts, fmt.Sprintf(`port port=%d protocol=%s`, r.Port, proto))
	}
	switch r.Action {
	case ActionBlock:
		parts = append(parts, "drop")
	case ActionAllow:
		parts = append(parts, "accept")
	}
	return strings.Join(parts, " ")
}

// --- nftables -----------------------------------------------------------

type nftablesBackend struct{}

func (nftablesBackend) Name() string { return "nftables" }

func (nftablesBackend) Available() bool {
	_, err := exec.LookPath("nft")
	return err == nil
}

func (b nftablesBackend) Apply(ctx context.Context, r Rule) error {
	expr := buildNftRule(r)
	if expr == "" {
		return ErrUnsupported
	}
	chain, hook := nftChainForDirection(r.Direction)
	// We assume the operator created an inet "controlone" table on first run.
	// If it does not exist, create it lazily.
	_ = runCmd(ctx, "nft", "add", "table", "inet", "controlone")
	_ = runCmd(ctx, "nft", "add", "chain", "inet", "controlone", chain, "{ type filter hook "+hook+" priority -1; }")
	return runCmd(ctx, "nft", "add", "rule", "inet", "controlone", chain, expr)
}

func (b nftablesBackend) Remove(_ context.Context, _ Rule) error {
	// nft requires handle-based delete; we leave removal to the operator for
	// now to avoid yanking unrelated rules. Surface an explicit error.
	return ErrUnsupported
}

func (b nftablesBackend) List(ctx context.Context, _ string) ([]Rule, error) {
	out, err := exec.CommandContext(ctx, "nft", "list", "ruleset").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("nft list: %w (%s)", err, string(out))
	}
	return []Rule{{Comment: string(out)}}, nil
}

func buildNftRule(r Rule) string {
	parts := []string{}
	if r.Source != "" {
		family := "ip"
		if strings.Contains(r.Source, ":") {
			family = "ip6"
		}
		parts = append(parts, family, "saddr", r.Source)
	}
	if r.Dest != "" {
		family := "ip"
		if strings.Contains(r.Dest, ":") {
			family = "ip6"
		}
		parts = append(parts, family, "daddr", r.Dest)
	}
	if r.Port > 0 {
		proto := r.Protocol
		if proto == "" {
			proto = "tcp"
		}
		parts = append(parts, proto, "dport", strconv.Itoa(r.Port))
	}
	switch r.Action {
	case ActionBlock:
		parts = append(parts, "drop")
	case ActionAllow:
		parts = append(parts, "accept")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// --- iptables (legacy) --------------------------------------------------

type iptablesBackend struct{}

func (iptablesBackend) Name() string { return "iptables" }

func (iptablesBackend) Available() bool {
	_, err := exec.LookPath("iptables")
	return err == nil
}

func (b iptablesBackend) Apply(ctx context.Context, r Rule) error {
	args := buildIptablesArgs("-A", r)
	if len(args) == 0 {
		return ErrUnsupported
	}
	return runCmd(ctx, "iptables", args...)
}

func (b iptablesBackend) Remove(ctx context.Context, r Rule) error {
	args := buildIptablesArgs("-D", r)
	return runCmd(ctx, "iptables", args...)
}

func (b iptablesBackend) List(ctx context.Context, tag string) ([]Rule, error) {
	out, err := exec.CommandContext(ctx, "iptables", "-L", "-n", "-v").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("iptables list: %w (%s)", err, string(out))
	}
	rules := []Rule{}
	for _, line := range strings.Split(string(out), "\n") {
		if tag != "" && !strings.Contains(line, tag) {
			continue
		}
		rules = append(rules, Rule{Comment: line})
	}
	return rules, nil
}

func buildIptablesArgs(op string, r Rule) []string {
	chain := "INPUT"
	if r.Direction == DirectionOut {
		chain = "OUTPUT"
	}
	args := []string{op, chain}
	if r.Source != "" {
		args = append(args, "-s", r.Source)
	}
	if r.Dest != "" {
		args = append(args, "-d", r.Dest)
	}
	if r.Port > 0 {
		proto := r.Protocol
		if proto == "" {
			proto = "tcp"
		}
		args = append(args, "-p", proto, "--dport", strconv.Itoa(r.Port))
	}
	if r.Tag != "" || r.Comment != "" {
		args = append(args, "-m", "comment", "--comment", joinComment(r.Tag, r.Comment))
	}
	target := "DROP"
	if r.Action == ActionAllow {
		target = "ACCEPT"
	}
	args = append(args, "-j", target)
	return args
}

func ufwStatusActive(out string) bool {
	return strings.Contains(strings.ToLower(out), "status: active")
}

func nftChainForDirection(direction Direction) (string, string) {
	if direction == DirectionOut {
		return "output", "output"
	}
	return "input", "input"
}

// --- pf (FreeBSD / macOS) — minimal --------------------------------------

type pfBackend struct{}

func (pfBackend) Name() string { return "pf" }

func (pfBackend) Available() bool {
	_, err := exec.LookPath("pfctl")
	return err == nil
}

func (b pfBackend) Apply(ctx context.Context, _ Rule) error {
	// pf operates on a ruleset file rather than ad-hoc additions; production
	// use should template /etc/pf.conf. We surface a helpful error rather
	// than silently doing nothing.
	return fmt.Errorf("pf backend requires ruleset file edits; integrate via /etc/pf.conf")
}

func (b pfBackend) Remove(_ context.Context, _ Rule) error { return ErrUnsupported }
func (b pfBackend) List(_ context.Context, _ string) ([]Rule, error) {
	return nil, ErrUnsupported
}

// --- helpers ------------------------------------------------------------

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func joinComment(tag, comment string) string {
	switch {
	case tag != "" && comment != "":
		return tag + " " + comment
	case tag != "":
		return tag
	default:
		return comment
	}
}
