//go:build linux

package netflow

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// procBackend polls /proc/net/{tcp,tcp6,udp,udp6} every PollInterval. For
// each currently-established (TCP) or recently-active (UDP) socket it
// resolves the owning PID by walking /proc/[pid]/fd/ until it finds a
// matching socket inode. State diffs across snapshots produce open / close
// / state_change events with the same shape eBPF emits.
//
// Bytes accounting on Linux without eBPF is best-effort: /proc/[pid]/io has
// rchar/wchar but those are *all* I/O, not per-socket. We export 0 unless
// /proc/net/tcp's tx_queue/rx_queue gives us something useful.
type procBackend struct {
	opts Options
	log  *zap.Logger
}

func init() {
	registerCollector(50, func(opts Options, log *zap.Logger) Collector {
		// Always available on Linux; eBPF backend (priority 100) wins when
		// it succeeds.
		return &procBackend{opts: opts, log: log}
	})
}

func (p *procBackend) Name() string { return "linux-proc" }

func (p *procBackend) Run(ctx context.Context, out chan<- ConnectionEvent) error {
	t := time.NewTicker(p.opts.PollInterval)
	defer t.Stop()

	prev := map[procSockKey]procSockState{}
	for {
		curr, err := p.snapshot()
		if err != nil {
			if p.log != nil {
				p.log.Debug("procnet snapshot", zap.Error(err))
			}
			curr = map[procSockKey]procSockState{}
		}
		now := time.Now().UTC()
		// Open: present now, absent before.
		for k, s := range curr {
			if _, ok := prev[k]; ok {
				continue
			}
			out <- p.connEvent("open", k, s, now)
		}
		// Close: present before, absent now.
		for k, s := range prev {
			if _, ok := curr[k]; ok {
				continue
			}
			out <- p.connEvent("close", k, s, now)
		}
		prev = curr
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

type procSockKey struct {
	proto      string
	srcIP      string
	srcPort    uint16
	dstIP      string
	dstPort    uint16
}

type procSockState struct {
	state      string
	inode      uint64
	pid        int
	user       string
	process    string
	startedAt  time.Time
}

// snapshot reads /proc/net for tcp + tcp6 + udp + udp6 and resolves PIDs.
func (p *procBackend) snapshot() (map[procSockKey]procSockState, error) {
	out := map[procSockKey]procSockState{}
	now := time.Now().UTC()
	for _, file := range []struct {
		path  string
		proto string
	}{
		{"/proc/net/tcp", "tcp"},
		{"/proc/net/tcp6", "tcp"},
		{"/proc/net/udp", "udp"},
		{"/proc/net/udp6", "udp"},
	} {
		entries, err := readProcNet(file.path, file.proto)
		if err != nil {
			continue
		}
		for _, e := range entries {
			out[procSockKey{
				proto:   file.proto,
				srcIP:   e.srcIP,
				srcPort: e.srcPort,
				dstIP:   e.dstIP,
				dstPort: e.dstPort,
			}] = procSockState{
				state:     e.state,
				inode:     e.inode,
				startedAt: now,
			}
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	// Resolve inodes → PIDs by walking /proc/*/fd/.
	resolveProcInodes(out)
	return out, nil
}

func (p *procBackend) connEvent(kind string, k procSockKey, s procSockState, now time.Time) ConnectionEvent {
	srcIP := net.ParseIP(k.srcIP)
	dstIP := net.ParseIP(k.dstIP)
	direction := "outbound"
	if srcIP != nil && (srcIP.IsUnspecified() || srcIP.IsPrivate() || srcIP.IsLoopback()) && !isExternal(dstIP) {
		direction = "inbound"
	}
	ev := ConnectionEvent{
		Kind:      kind,
		PID:       s.pid,
		Process:   s.process,
		User:      s.user,
		SrcIP:     srcIP,
		SrcPort:   k.srcPort,
		DstIP:     dstIP,
		DstPort:   k.dstPort,
		Protocol:  k.proto,
		State:     s.state,
		Direction: direction,
	}
	if kind == "open" {
		ev.StartedAt = now
	} else {
		ev.StartedAt = s.startedAt
		ev.EndedAt = now
		ev.LastDataAt = now
	}
	return ev
}

// procNetEntry mirrors one line of /proc/net/tcp.
type procNetEntry struct {
	srcIP   string
	srcPort uint16
	dstIP   string
	dstPort uint16
	state   string
	inode   uint64
}

func readProcNet(path, proto string) ([]procNetEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	out := []procNetEntry{}
	sc := bufio.NewScanner(f)
	header := true
	for sc.Scan() {
		if header {
			header = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		// fields[1]=local_address fields[2]=rem_address fields[3]=state
		// fields[9]=inode
		srcIP, srcPort, ok := parseHexIPPort(fields[1])
		if !ok {
			continue
		}
		dstIP, dstPort, ok := parseHexIPPort(fields[2])
		if !ok {
			continue
		}
		stateName := tcpStateName(fields[3])
		_ = proto
		inode, _ := strconv.ParseUint(fields[9], 10, 64)
		out = append(out, procNetEntry{
			srcIP:   srcIP,
			srcPort: srcPort,
			dstIP:   dstIP,
			dstPort: dstPort,
			state:   stateName,
			inode:   inode,
		})
	}
	return out, nil
}

// parseHexIPPort parses the "<HEX_IP>:<HEX_PORT>" notation /proc/net uses.
// IPs are little-endian for IPv4 and big-endian word-flipped for IPv6.
func parseHexIPPort(s string) (string, uint16, bool) {
	if i := strings.IndexByte(s, ':'); i > 0 {
		ipHex := s[:i]
		portHex := s[i+1:]
		port64, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil {
			return "", 0, false
		}
		raw, err := hex.DecodeString(ipHex)
		if err != nil {
			return "", 0, false
		}
		switch len(raw) {
		case 4:
			ip := net.IPv4(raw[3], raw[2], raw[1], raw[0])
			return ip.String(), uint16(port64), true
		case 16:
			// /proc/net/tcp6 stores the address as 4 little-endian uint32s.
			ip := make(net.IP, 16)
			for i := 0; i < 4; i++ {
				ip[i*4+0] = raw[i*4+3]
				ip[i*4+1] = raw[i*4+2]
				ip[i*4+2] = raw[i*4+1]
				ip[i*4+3] = raw[i*4+0]
			}
			return ip.String(), uint16(port64), true
		}
	}
	return "", 0, false
}

func tcpStateName(hex string) string {
	switch hex {
	case "01":
		return "ESTABLISHED"
	case "02":
		return "SYN_SENT"
	case "03":
		return "SYN_RECV"
	case "04":
		return "FIN_WAIT1"
	case "05":
		return "FIN_WAIT2"
	case "06":
		return "TIME_WAIT"
	case "07":
		return "CLOSE"
	case "08":
		return "CLOSE_WAIT"
	case "09":
		return "LAST_ACK"
	case "0A":
		return "LISTEN"
	case "0B":
		return "CLOSING"
	default:
		return "STATE_" + hex
	}
}

// resolveProcInodes walks /proc/*/fd/* once and back-fills the PID for every
// inode in the supplied map. Single-pass keeps the cost O(open FDs on
// host), which is the cheapest correct approach.
func resolveProcInodes(socks map[procSockKey]procSockState) {
	inodeIndex := make(map[uint64][]*procSockState, len(socks))
	keys := make([]procSockKey, 0, len(socks))
	for k, s := range socks {
		st := s
		inodeIndex[s.inode] = append(inodeIndex[s.inode], &st)
		socks[k] = st
		keys = append(keys, k)
	}

	procs, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, ent := range procs {
		if !ent.IsDir() {
			continue
		}
		name := ent.Name()
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", name, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			// links look like "socket:[12345]"
			if !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inodeStr := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			inode, err := strconv.ParseUint(inodeStr, 10, 64)
			if err != nil {
				continue
			}
			matches, ok := inodeIndex[inode]
			if !ok {
				continue
			}
			procName := readProcComm(pid)
			user := readProcUser(pid)
			for _, m := range matches {
				m.pid = pid
				m.process = procName
				m.user = user
			}
		}
	}
	// Replace updated state in the result map.
	for _, k := range keys {
		s := socks[k]
		if list, ok := inodeIndex[s.inode]; ok && len(list) > 0 {
			s.pid = list[0].pid
			s.process = list[0].process
			s.user = list[0].user
		}
		socks[k] = s
	}
}

func readProcComm(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readProcUser(pid int) string {
	// /proc/[pid]/status has Uid: real eff saved fs
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}
