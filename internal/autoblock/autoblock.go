// Package autoblock connects threat intel + rule triggers to the host
// firewall. It runs on the agent (per-node enforcement) and optionally on the
// control plane (network-wide bans pushed via WireGuard or LB ACLs).
//
// Design:
//
//   - A Source supplies "candidates" — IPs the pipeline considers blocking.
//     Sources include the threat-intel snapshot subscriber and the rule
//     trigger event bus.
//   - The pipeline applies a Policy (allowlist + minimum score) to filter
//     candidates and turns approved ones into firewall.Rule entries.
//   - Decisions are recorded to an audit log so operators can review who/why
//     was banned.
//   - Optional fail2ban escalation: repeated failures from the same IP can
//     escalate to a fail2ban jail ban for ban-time enforcement that survives
//     firewall rule churn.
package autoblock

import (
	"context"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/firewall"
)

// Candidate is one IP / CIDR being considered for action.
type Candidate struct {
	IP        string
	CIDR      string
	Source    string // "threat-intel:abuseipdb" | "rule-trigger:<rule_id>" | etc.
	Score     int
	Reason    string
	Observed  time.Time
}

// Policy controls when the pipeline acts.
type Policy struct {
	// MinScore is the threshold a candidate must meet before block. Range 0-100.
	// Default 75.
	MinScore int
	// Allowlist contains CIDRs that must never be blocked, regardless of score.
	Allowlist []string
	// CooldownSeconds debounces re-application of the same IP. Default 300.
	CooldownSeconds int
	// Fail2BanJail, when set + Fail2BanController is available, escalates
	// the IP into the named jail in addition to the firewall rule.
	Fail2BanJail string
	// MaxBlocksPerHour caps total blocks/hour to protect against feed errors.
	// Default 1000.
	MaxBlocksPerHour int
}

// AuditEntry records one decision (block / suppress / allow).
type AuditEntry struct {
	IP       string
	Decision string
	Reason   string
	Source   string
	At       time.Time
}

// Pipeline orchestrates candidate intake → decision → firewall apply.
type Pipeline struct {
	policy    Policy
	fw        *firewall.Manager
	jail      *firewall.Fail2BanController
	log       *zap.Logger
	allowNets []*net.IPNet
	mu        sync.Mutex
	cooldown  map[string]time.Time
	hourly    []time.Time
	audit     []AuditEntry
}

// New returns a ready Pipeline. fw must already be Detect()ed.
func New(p Policy, fw *firewall.Manager, log *zap.Logger) *Pipeline {
	if p.MinScore <= 0 {
		p.MinScore = 75
	}
	if p.CooldownSeconds <= 0 {
		p.CooldownSeconds = 300
	}
	if p.MaxBlocksPerHour <= 0 {
		p.MaxBlocksPerHour = 1000
	}
	allow := []*net.IPNet{}
	for _, cidr := range p.Allowlist {
		if !contains(cidr, "/") {
			cidr = cidr + "/32"
		}
		_, n, err := net.ParseCIDR(cidr)
		if err == nil {
			allow = append(allow, n)
		}
	}
	pl := &Pipeline{
		policy:    p,
		fw:        fw,
		log:       log,
		allowNets: allow,
		cooldown:  make(map[string]time.Time),
	}
	if p.Fail2BanJail != "" {
		ctrl := firewall.Fail2BanController{}
		if ctrl.Available() {
			pl.jail = &ctrl
		}
	}
	return pl
}

// Process applies policy to a candidate and, if approved, pushes a firewall
// rule. Returns the recorded AuditEntry.
func (p *Pipeline) Process(ctx context.Context, c Candidate) AuditEntry {
	entry := AuditEntry{IP: c.IP, Source: c.Source, At: time.Now().UTC(), Reason: c.Reason}
	target := c.IP
	if target == "" {
		target = c.CIDR
	}
	if target == "" {
		entry.Decision = "skipped"
		entry.Reason = "empty target"
		p.record(entry)
		return entry
	}

	// Allowlist always wins.
	if p.allowed(target) {
		entry.Decision = "skipped"
		entry.Reason = "allowlisted"
		p.record(entry)
		return entry
	}

	if c.Score < p.policy.MinScore {
		entry.Decision = "skipped"
		entry.Reason = "below score threshold"
		p.record(entry)
		return entry
	}

	p.mu.Lock()
	if last, ok := p.cooldown[target]; ok && time.Since(last) < time.Duration(p.policy.CooldownSeconds)*time.Second {
		p.mu.Unlock()
		entry.Decision = "suppressed"
		entry.Reason = "cooldown"
		p.record(entry)
		return entry
	}
	if !p.canBlockNow() {
		p.mu.Unlock()
		entry.Decision = "rate-limited"
		entry.Reason = "hourly cap reached"
		p.record(entry)
		return entry
	}
	p.cooldown[target] = time.Now()
	p.hourly = append(p.hourly, time.Now())
	p.mu.Unlock()

	rule := firewall.Rule{
		Source:    c.IP,
		Direction: firewall.DirectionIn,
		Action:    firewall.ActionBlock,
		Tag:       "controlone:autoblock:" + c.Source,
		Comment:   c.Reason,
	}
	if c.IP == "" && c.CIDR != "" {
		rule.Source = c.CIDR
	}
	if err := p.fw.Apply(ctx, rule); err != nil {
		entry.Decision = "error"
		entry.Reason = err.Error()
		if p.log != nil {
			p.log.Warn("autoblock apply", zap.Error(err), zap.String("target", target))
		}
		p.record(entry)
		return entry
	}
	if p.jail != nil && c.IP != "" {
		if err := p.jail.Ban(ctx, p.policy.Fail2BanJail, c.IP); err != nil && p.log != nil {
			p.log.Warn("autoblock fail2ban", zap.Error(err))
		}
	}
	entry.Decision = "blocked"
	p.record(entry)
	return entry
}

// Audit returns a snapshot of recent decisions (last 1000).
func (p *Pipeline) Audit() []AuditEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]AuditEntry, len(p.audit))
	copy(out, p.audit)
	return out
}

func (p *Pipeline) record(e AuditEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.audit = append(p.audit, e)
	if len(p.audit) > 1000 {
		p.audit = p.audit[len(p.audit)-1000:]
	}
}

func (p *Pipeline) allowed(target string) bool {
	host := target
	if i := indexOf(target, "/"); i >= 0 {
		host = target[:i]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range p.allowNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (p *Pipeline) canBlockNow() bool {
	cutoff := time.Now().Add(-time.Hour)
	pruned := p.hourly[:0]
	for _, t := range p.hourly {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	p.hourly = pruned
	return len(p.hourly) < p.policy.MaxBlocksPerHour
}

// --- tiny helpers (kept local to avoid pulling strings.* into hot path) --
func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
