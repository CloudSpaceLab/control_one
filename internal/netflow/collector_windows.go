//go:build windows

package netflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// winBackend polls Get-NetTCPConnection via PowerShell. PowerShell.exe ships
// on every supported Windows host, so this works without admin and without
// extra binaries. The CPU cost is non-trivial (~100ms per spawn) so we cap
// the polling interval at 5s by default and keep the JSON projection small.
type winBackend struct {
	opts Options
	log  *zap.Logger
}

func init() {
	registerCollector(50, func(opts Options, log *zap.Logger) Collector {
		if _, err := exec.LookPath("powershell.exe"); err != nil {
			return nil
		}
		return &winBackend{opts: opts, log: log}
	})
}

func (w *winBackend) Name() string { return "windows-powershell" }

type winNetConn struct {
	LocalAddress  string `json:"LocalAddress"`
	LocalPort     int    `json:"LocalPort"`
	RemoteAddress string `json:"RemoteAddress"`
	RemotePort    int    `json:"RemotePort"`
	State         string `json:"State"`
	OwningProcess int    `json:"OwningProcess"`
}

func (w *winBackend) Run(ctx context.Context, out chan<- ConnectionEvent) error {
	t := time.NewTicker(w.opts.PollInterval)
	defer t.Stop()

	prev := map[string]winNetConn{}
	for {
		conns, err := w.snapshot(ctx)
		if err != nil {
			if w.log != nil {
				w.log.Debug("netflow win snapshot", zap.Error(err))
			}
			conns = nil
		}
		now := time.Now().UTC()
		curr := make(map[string]winNetConn, len(conns))
		for _, c := range conns {
			key := winKey(c)
			curr[key] = c
		}
		for k, c := range curr {
			if _, ok := prev[k]; ok {
				continue
			}
			out <- w.event("open", c, now)
		}
		for k, c := range prev {
			if _, ok := curr[k]; ok {
				continue
			}
			out <- w.event("close", c, now)
		}
		prev = curr

		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (w *winBackend) snapshot(ctx context.Context) ([]winNetConn, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-Command",
		"Get-NetTCPConnection | Select-Object LocalAddress,LocalPort,RemoteAddress,RemotePort,State,OwningProcess | ConvertTo-Json -Compress -Depth 2")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	// PowerShell returns a single object when there's one result and an
	// array otherwise. Normalise to array.
	if out[0] == '{' {
		var single winNetConn
		if err := json.Unmarshal(out, &single); err != nil {
			return nil, fmt.Errorf("decode single: %w", err)
		}
		return []winNetConn{single}, nil
	}
	var conns []winNetConn
	if err := json.Unmarshal(out, &conns); err != nil {
		return nil, fmt.Errorf("decode array: %w", err)
	}
	return conns, nil
}

func (w *winBackend) event(kind string, c winNetConn, now time.Time) ConnectionEvent {
	src := net.ParseIP(c.LocalAddress)
	dst := net.ParseIP(c.RemoteAddress)
	ev := ConnectionEvent{
		Kind:     kind,
		PID:      c.OwningProcess,
		SrcIP:    src,
		SrcPort:  uint16(c.LocalPort),
		DstIP:    dst,
		DstPort:  uint16(c.RemotePort),
		Protocol: "tcp",
		State:    c.State,
		StartedAt: now,
	}
	if kind == "close" {
		ev.EndedAt = now
		ev.LastDataAt = now
	}
	_ = strconv.Itoa // future: cache pid → exe via Get-Process
	return ev
}

func winKey(c winNetConn) string {
	return fmt.Sprintf("%s:%d|%s:%d|%d", c.LocalAddress, c.LocalPort, c.RemoteAddress, c.RemotePort, c.OwningProcess)
}
