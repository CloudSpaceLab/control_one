// Package procmon publishes per-process events: spawn, exit, and periodic
// top-K resource usage. It uses gopsutil/v3 so it works unprivileged on
// every platform that supports the package.
//
// Scope:
//
//   * proc.exec  — emitted when a PID first appears between snapshots.
//   * proc.exit  — emitted when a previously-seen PID is gone.
//   * proc.usage — emitted for the top K processes (by combined CPU + mem
//                  pressure) every snapshot interval. Default K=20.
//
// Forensic value: every proc.exec carries the executable's xxhash64 + cmdline
// + user, so an analyst can later answer "which binary made this connection"
// even after the process exits and the file is deleted.
package procmon

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/shirou/gopsutil/v3/process"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/eventstream"
)

// Options tune the collector.
type Options struct {
	// Interval between snapshots. Default 30s.
	Interval time.Duration
	// TopK: how many top processes to emit usage events for. Default 20.
	TopK int
	// HashExecutables: when true, sha-equivalent (xxhash64) hash binaries on
	// first observation. Costs disk reads; off for very busy hosts.
	HashExecutables bool
	// NodeID + TenantID stamped on every emitted event.
	NodeID   string
	TenantID string
}

// Collector snapshots gopsutil process state and publishes events.
type Collector struct {
	opts   Options
	stream *eventstream.Stream
	log    *zap.Logger
	mu     sync.Mutex
	prev   map[int32]processSnapshot
	hashes map[string]string // exe path → cached xxhash hex
}

type processSnapshot struct {
	pid        int32
	ppid       int32
	name       string
	cmdline    string
	user       string
	uid        int32
	gid        int32
	exe        string
	cpuPercent float64
	memRSS     uint64
	numFDs    int32
	numThread int32
	createdMS int64
	exeHash   string
}

// New constructs a Collector. NodeID + TenantID are stamped on every event.
func New(stream *eventstream.Stream, log *zap.Logger, opts Options) *Collector {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	if opts.TopK <= 0 {
		opts.TopK = 20
	}
	return &Collector{
		opts:   opts,
		stream: stream,
		log:    log,
		prev:   make(map[int32]processSnapshot),
		hashes: make(map[string]string),
	}
}

// Run loops on the snapshot interval until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	if c == nil || c.stream == nil {
		return
	}
	t := time.NewTicker(c.opts.Interval)
	defer t.Stop()
	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

func (c *Collector) tick(ctx context.Context) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		if c.log != nil {
			c.log.Warn("procmon list", zap.Error(err))
		}
		return
	}
	now := time.Now().UTC()
	current := make(map[int32]processSnapshot, len(procs))
	for _, p := range procs {
		s := c.snapshot(ctx, p)
		current[s.pid] = s
	}

	c.mu.Lock()
	prev := c.prev
	c.prev = current
	c.mu.Unlock()

	// Diff: new PIDs → proc.exec; gone PIDs → proc.exit.
	for pid, s := range current {
		if _, ok := prev[pid]; ok {
			continue
		}
		c.emitExec(s, now)
	}
	for pid, s := range prev {
		if _, ok := current[pid]; ok {
			continue
		}
		c.emitExit(s, now)
	}

	// Top-K usage.
	c.emitTopK(current, now)
}

func (c *Collector) snapshot(ctx context.Context, p *process.Process) processSnapshot {
	s := processSnapshot{pid: p.Pid}
	if name, err := p.NameWithContext(ctx); err == nil {
		s.name = name
	}
	if cmd, err := p.CmdlineWithContext(ctx); err == nil {
		s.cmdline = truncate(cmd, 2048)
	}
	if ppid, err := p.PpidWithContext(ctx); err == nil {
		s.ppid = ppid
	}
	if u, err := p.UsernameWithContext(ctx); err == nil {
		s.user = u
	}
	if uids, err := p.UidsWithContext(ctx); err == nil && len(uids) > 0 {
		s.uid = uids[0]
	}
	if gids, err := p.GidsWithContext(ctx); err == nil && len(gids) > 0 {
		s.gid = gids[0]
	}
	if exe, err := p.ExeWithContext(ctx); err == nil {
		s.exe = exe
		if c.opts.HashExecutables {
			s.exeHash = c.hashExecutable(exe)
		}
	}
	if cpu, err := p.CPUPercentWithContext(ctx); err == nil {
		s.cpuPercent = cpu
	}
	if mem, err := p.MemoryInfoWithContext(ctx); err == nil && mem != nil {
		s.memRSS = mem.RSS
	}
	if fds, err := p.NumFDsWithContext(ctx); err == nil {
		s.numFDs = fds
	}
	if th, err := p.NumThreadsWithContext(ctx); err == nil {
		s.numThread = th
	}
	if created, err := p.CreateTimeWithContext(ctx); err == nil {
		s.createdMS = created
	}
	return s
}

func (c *Collector) hashExecutable(path string) string {
	c.mu.Lock()
	if h, ok := c.hashes[path]; ok {
		c.mu.Unlock()
		return h
	}
	c.mu.Unlock()
	h, err := xxhashFile(path)
	if err != nil {
		return ""
	}
	c.mu.Lock()
	c.hashes[path] = h
	c.mu.Unlock()
	return h
}

func (c *Collector) emitExec(s processSnapshot, ts time.Time) {
	c.stream.Publish(eventstream.Event{
		Type:        "proc.exec",
		TS:          ts,
		NodeID:      c.opts.NodeID,
		TenantID:    c.opts.TenantID,
		PID:         int64(s.pid),
		ProcessName: s.name,
		UserName:    s.user,
		Severity:    "info",
		DedupKey:    fmt.Sprintf("proc.exec:%d:%d", s.pid, s.createdMS),
		Details: map[string]any{
			"ppid":     int64(s.ppid),
			"uid":      int64(s.uid),
			"gid":      int64(s.gid),
			"cmdline":  s.cmdline,
			"exe_path": s.exe,
			"exe_hash": s.exeHash,
		},
	})
}

func (c *Collector) emitExit(s processSnapshot, ts time.Time) {
	c.stream.Publish(eventstream.Event{
		Type:        "proc.exit",
		TS:          ts,
		NodeID:      c.opts.NodeID,
		TenantID:    c.opts.TenantID,
		PID:         int64(s.pid),
		ProcessName: s.name,
		UserName:    s.user,
		Severity:    "info",
		DedupKey:    fmt.Sprintf("proc.exit:%d:%d", s.pid, s.createdMS),
		Details: map[string]any{
			"exited_at": ts.Format("2006-01-02 15:04:05.000"),
		},
	})
}

func (c *Collector) emitTopK(current map[int32]processSnapshot, ts time.Time) {
	type ranked struct {
		s     processSnapshot
		score float64
	}
	scored := make([]ranked, 0, len(current))
	for _, s := range current {
		// Combined score: weight CPU% directly + memRSS in MiB / 100 to keep
		// CPU dominant on busy boxes while memory still pulls into top-K
		// when it matters.
		score := s.cpuPercent + float64(s.memRSS)/(100*1024*1024)
		scored = append(scored, ranked{s: s, score: score})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if len(scored) > c.opts.TopK {
		scored = scored[:c.opts.TopK]
	}
	for _, r := range scored {
		c.stream.Publish(eventstream.Event{
			Type:        "proc.usage",
			TS:          ts,
			NodeID:      c.opts.NodeID,
			TenantID:    c.opts.TenantID,
			PID:         int64(r.s.pid),
			ProcessName: r.s.name,
			UserName:    r.s.user,
			Severity:    "info",
			DedupKey:    fmt.Sprintf("proc.usage:%d:%d", r.s.pid, ts.Unix()),
			Details: map[string]any{
				"cpu_percent":  r.s.cpuPercent,
				"mem_rss":      int64(r.s.memRSS),
				"num_fds":      int64(r.s.numFDs),
				"num_threads":  int64(r.s.numThread),
			},
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// xxhashFile streams the file through xxhash64 and returns the hex-encoded
// digest. Returns "" on read error.
func xxhashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := xxhash.New()
	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return fmt.Sprintf("%016x", h.Sum64()), nil
}

// fnvHash is an alternative if xxhash is unavailable; kept for safety.
func fnvHash(b []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}
