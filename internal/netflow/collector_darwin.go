//go:build darwin

package netflow

import (
	"bufio"
	"context"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// darwinBackend wraps `lsof -i -n -P` with periodic polling. Works
// unprivileged for processes owned by the same user; falls back to whatever
// lsof can read otherwise. nettop / iflight upgrade can land later.
type darwinBackend struct {
	opts Options
	log  *zap.Logger
}

func init() {
	registerCollector(50, func(opts Options, log *zap.Logger) Collector {
		if _, err := exec.LookPath("lsof"); err != nil {
			return nil
		}
		return &darwinBackend{opts: opts, log: log}
	})
}

func (d *darwinBackend) Name() string { return "darwin-lsof" }

func (d *darwinBackend) Run(ctx context.Context, out chan<- ConnectionEvent) error {
	t := time.NewTicker(d.opts.PollInterval)
	defer t.Stop()
	prev := map[string]ConnectionEvent{}
	for {
		curr, err := d.snapshot(ctx)
		if err != nil && d.log != nil {
			d.log.Debug("lsof snapshot", zap.Error(err))
		}
		now := time.Now().UTC()
		for k, ev := range curr {
			if _, ok := prev[k]; ok {
				continue
			}
			ev.Kind = "open"
			ev.StartedAt = now
			out <- ev
		}
		for k, ev := range prev {
			if _, ok := curr[k]; ok {
				continue
			}
			ev.Kind = "close"
			ev.EndedAt = now
			ev.LastDataAt = now
			out <- ev
		}
		prev = curr
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (d *darwinBackend) snapshot(ctx context.Context) (map[string]ConnectionEvent, error) {
	cmd := exec.CommandContext(ctx, "lsof", "-i", "-n", "-P", "-F", "pcuP0n")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cmd.Wait() }()
	out := map[string]ConnectionEvent{}
	sc := bufio.NewScanner(stdout)
	var cur ConnectionEvent
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			if cur.PID != 0 {
				key := lsofKey(cur)
				out[key] = cur
			}
			cur = ConnectionEvent{}
			cur.PID, _ = strconv.Atoi(line[1:])
		case 'c':
			cur.Process = line[1:]
		case 'u':
			cur.User = line[1:]
		case 'P':
			cur.Protocol = strings.ToLower(line[1:])
		case 'n':
			parseLsofName(line[1:], &cur)
		}
	}
	if cur.PID != 0 {
		out[lsofKey(cur)] = cur
	}
	return out, nil
}

// parseLsofName parses lsof's name field "src:port->dst:port" or "*:port".
func parseLsofName(name string, ev *ConnectionEvent) {
	if i := strings.Index(name, "->"); i > 0 {
		left, right := name[:i], name[i+2:]
		ev.SrcIP, ev.SrcPort = parseAddrPort(left)
		ev.DstIP, ev.DstPort = parseAddrPort(right)
		ev.State = "ESTABLISHED"
	} else {
		ev.SrcIP, ev.SrcPort = parseAddrPort(name)
		ev.State = "LISTEN"
	}
}

func parseAddrPort(s string) (net.IP, uint16) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		// IPv6 literal without brackets, or "*" wildcard.
		if i := strings.LastIndexByte(s, ':'); i > 0 {
			host = s[:i]
			port = s[i+1:]
		} else {
			return nil, 0
		}
	}
	p, _ := strconv.ParseUint(port, 10, 16)
	if host == "*" {
		return net.IPv4zero, uint16(p)
	}
	return net.ParseIP(host), uint16(p)
}

func lsofKey(ev ConnectionEvent) string {
	return ev.Protocol + "|" + ipString(ev.SrcIP) + ":" + strconv.Itoa(int(ev.SrcPort)) + "|" + ipString(ev.DstIP) + ":" + strconv.Itoa(int(ev.DstPort)) + "|" + strconv.Itoa(ev.PID)
}
