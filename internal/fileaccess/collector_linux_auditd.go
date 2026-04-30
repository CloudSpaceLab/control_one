//go:build linux

package fileaccess

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// auditdBackend tails /var/log/audit/audit.log and emits FileEvents derived
// from SYSCALL + PATH records. It only activates when the audit log exists
// — no auto-installing audit rules; operators are expected to run
// `auditctl -w /etc/ -p rwxa -k controlone` on their watched paths.
//
// This is the Linux-without-eBPF baseline. Latency: 1-2 s for auditd to flush
// to disk; cost: just file tail, no kernel work beyond the existing audit
// rules.
type auditdBackend struct {
	opts Options
	log  *zap.Logger
	path string
}

func init() {
	registerFileBackend(40, func(opts Options, log *zap.Logger) Collector {
		path := "/var/log/audit/audit.log"
		if _, err := os.Stat(path); err != nil {
			return nil
		}
		return &auditdBackend{opts: opts, log: log, path: path}
	})
}

func (a *auditdBackend) Name() string { return "linux-auditd" }

func (a *auditdBackend) Run(ctx context.Context, out chan<- FileEvent) error {
	f, err := openTail(a.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	pending := map[string]*FileEvent{} // audit msg id → in-flight event
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			a.process(line, pending, out)
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
}

// openTail opens the file in read-only mode and seeks to end so we don't
// re-replay yesterday's rotated content. If the file rotates the agent
// won't follow it transparently — the audit-tail is best-effort and
// reopens on a SIGUSR1 in production builds.
func openTail(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, 2); err != nil { // io.SeekEnd
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// process handles one audit log line. Audit lines look like:
//
//	type=SYSCALL msg=audit(1700000000.123:123): syscall=2 ... pid=1234 ...
//	type=PATH msg=audit(1700000000.123:123): item=0 name="/etc/shadow" nametype=NORMAL
//
// We collect the SYSCALL fields (pid, comm, exe), then attach the PATH name
// to the same audit msg id, and emit on the second PATH line (or on a
// new SYSCALL after).
func (a *auditdBackend) process(line string, pending map[string]*FileEvent, out chan<- FileEvent) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	fields := parseAuditFields(line)
	msgID := fields["msg"]
	if msgID == "" {
		return
	}
	switch fields["type"] {
	case "SYSCALL":
		if pending[msgID] == nil {
			pending[msgID] = &FileEvent{Op: opForSyscall(fields["syscall"])}
		}
		ev := pending[msgID]
		if pid := fields["pid"]; pid != "" {
			if n, ok := atoiUnsafe(pid); ok {
				ev.PID = n
			}
		}
		if comm := strings.Trim(fields["comm"], `"`); comm != "" {
			ev.Process = comm
		}
		if uid := fields["uid"]; uid != "" {
			ev.User = uid
		}
	case "PATH":
		ev, ok := pending[msgID]
		if !ok {
			return
		}
		path := strings.Trim(fields["name"], `"`)
		if path == "" {
			return
		}
		ev.Path = path
		ev.OpCount = 1
		ev.StartedAt = time.Now().UTC()
		ev.EndedAt = ev.StartedAt
		out <- *ev
		delete(pending, msgID)
	case "EOE":
		delete(pending, msgID)
	}
}

func parseAuditFields(line string) map[string]string {
	out := map[string]string{}
	tokens := tokenizeAuditLine(line)
	for _, tk := range tokens {
		eq := strings.IndexByte(tk, '=')
		if eq <= 0 {
			continue
		}
		k := tk[:eq]
		v := tk[eq+1:]
		// msg=audit(1700:123) → keep just the colon-suffixed unique id
		if k == "msg" {
			if i := strings.IndexByte(v, ':'); i >= 0 {
				if j := strings.IndexByte(v, ')'); j > i {
					v = v[i+1 : j]
				}
			}
		}
		out[k] = v
	}
	return out
}

// tokenizeAuditLine splits on space respecting "quoted" values + paren groups.
func tokenizeAuditLine(line string) []string {
	var (
		toks []string
		cur  strings.Builder
		inQ  byte
	)
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQ != 0 {
			cur.WriteByte(c)
			if c == inQ {
				inQ = 0
			}
			continue
		}
		switch c {
		case '"':
			inQ = '"'
			cur.WriteByte(c)
		case ' ':
			if cur.Len() > 0 {
				toks = append(toks, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

func opForSyscall(num string) string {
	switch num {
	case "2", "257": // open / openat
		return "open"
	case "0", "17", "295": // read / pread64
		return "read"
	case "1", "18", "296": // write / pwrite64
		return "write"
	case "87", "263": // unlink / unlinkat
		return "unlink"
	case "82", "264": // rename / renameat
		return "rename"
	}
	return "open"
}

// atoiUnsafe is a fast positive-int parser; returns ok=false on negative.
func atoiUnsafe(s string) (int, bool) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

var _ = filepath.Join // keep filepath imported for future rotation handling
