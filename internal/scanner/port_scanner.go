package scanner

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// PortRule mirrors the control-plane port_monitoring_rules row for scanning.
type PortRule struct {
	ID            string
	Name          string
	Port          int
	Protocol      string
	ExpectedState string // "open" or "closed"
	Severity      string
}

// PortScanResult reports the observed state of a single rule.
type PortScanResult struct {
	RuleID    string
	Name      string
	Port      int
	Protocol  string
	Expected  string
	Observed  string
	Matched   bool
	Severity  string
	Error     string
	CheckedAt time.Time
}

// PortScanner probes local ports to evaluate PortRules. Probes are concurrent
// but bounded by maxConcurrent. Target is "127.0.0.1" by default.
type PortScanner struct {
	target        string
	timeout       time.Duration
	maxConcurrent int
}

// NewPortScanner returns a ready-to-use scanner. maxConcurrent defaults to 16.
func NewPortScanner(target string, timeout time.Duration, maxConcurrent int) *PortScanner {
	if target == "" {
		target = "127.0.0.1"
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 16
	}
	return &PortScanner{target: target, timeout: timeout, maxConcurrent: maxConcurrent}
}

// Run probes each rule and returns a slice aligned with rules.
func (p *PortScanner) Run(ctx context.Context, rules []PortRule) []PortScanResult {
	out := make([]PortScanResult, len(rules))
	sem := make(chan struct{}, p.maxConcurrent)
	doneCh := make(chan int, len(rules))
	for i, r := range rules {
		i, r := i, r
		sem <- struct{}{}
		go func() {
			defer func() {
				<-sem
				doneCh <- i
			}()
			out[i] = p.probe(ctx, r)
		}()
	}
	for range rules {
		<-doneCh
	}
	return out
}

func (p *PortScanner) probe(ctx context.Context, r PortRule) PortScanResult {
	res := PortScanResult{
		RuleID:    r.ID,
		Name:      r.Name,
		Port:      r.Port,
		Protocol:  r.Protocol,
		Expected:  r.ExpectedState,
		Severity:  r.Severity,
		CheckedAt: time.Now().UTC(),
	}
	network := r.Protocol
	if network == "" {
		network = "tcp"
	}
	addr := net.JoinHostPort(p.target, strconv.Itoa(r.Port))

	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	d := net.Dialer{Timeout: p.timeout}
	conn, err := d.DialContext(probeCtx, network, addr)
	observed := "closed"
	if err == nil {
		observed = "open"
		_ = conn.Close()
	} else {
		// UDP is best-effort: a closed port usually returns no error from Dial
		// because there is no handshake. We report "unknown" for UDP probe failures.
		if network == "udp" {
			observed = "unknown"
			res.Error = err.Error()
		}
	}
	res.Observed = observed
	res.Matched = observed == r.ExpectedState
	if !res.Matched && res.Error == "" && observed != "unknown" {
		res.Error = fmt.Sprintf("expected %s, observed %s", r.ExpectedState, observed)
	}
	return res
}
