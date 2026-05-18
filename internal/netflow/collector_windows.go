//go:build windows

package netflow

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"

	"go.uber.org/zap"
	"golang.org/x/sys/windows"
)

const (
	afInet  = 2
	afInet6 = 23

	tcpTableOwnerPIDAll = 5
	udpTableOwnerPID    = 1
)

var (
	modIphlpapi             = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCPTable = modIphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUDPTable = modIphlpapi.NewProc("GetExtendedUdpTable")
)

// winBackend uses IP Helper APIs instead of spawning PowerShell/netstat.
type winBackend struct {
	opts Options
	log  *zap.Logger
}

func init() {
	registerCollector(50, func(opts Options, log *zap.Logger) Collector {
		return &winBackend{opts: opts, log: log}
	})
}

func (w *winBackend) Name() string { return "windows-iphlpapi" }

type winNetConn struct {
	LocalAddress  net.IP
	LocalPort     uint16
	RemoteAddress net.IP
	RemotePort    uint16
	State         string
	OwningProcess int
	Protocol      string
}

type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

type mibTCP6RowOwnerPID struct {
	LocalAddr     [16]byte
	LocalScopeID  uint32
	LocalPort     uint32
	RemoteAddr    [16]byte
	RemoteScopeID uint32
	RemotePort    uint32
	State         uint32
	OwningPID     uint32
}

type mibUDPRowOwnerPID struct {
	LocalAddr uint32
	LocalPort uint32
	OwningPID uint32
}

type mibUDP6RowOwnerPID struct {
	LocalAddr    [16]byte
	LocalScopeID uint32
	LocalPort    uint32
	OwningPID    uint32
}

func (w *winBackend) Run(ctx context.Context, out chan<- ConnectionEvent) error {
	t := time.NewTicker(w.opts.PollInterval)
	defer t.Stop()

	prev := map[string]winNetConn{}
	for {
		conns, err := w.snapshot()
		if err != nil && w.log != nil {
			w.log.Debug("netflow windows native snapshot", zap.Error(err))
		}
		now := time.Now().UTC()
		curr := make(map[string]winNetConn, len(conns))
		for _, c := range conns {
			curr[winKey(c)] = c
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

func (w *winBackend) snapshot() ([]winNetConn, error) {
	var out []winNetConn
	var firstErr error
	for _, family := range []uint32{afInet, afInet6} {
		tcp, err := snapshotTCP(family)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		out = append(out, tcp...)
		udp, err := snapshotUDP(family)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		out = append(out, udp...)
	}
	return out, firstErr
}

func snapshotTCP(family uint32) ([]winNetConn, error) {
	buf, err := callSizedTable(procGetExtendedTCPTable, family, tcpTableOwnerPIDAll)
	if err != nil {
		return nil, err
	}
	if len(buf) < 4 {
		return nil, nil
	}
	count := binary.LittleEndian.Uint32(buf[:4])
	if family == afInet {
		return parseTCP4Rows(buf, count), nil
	}
	return parseTCP6Rows(buf, count), nil
}

func snapshotUDP(family uint32) ([]winNetConn, error) {
	buf, err := callSizedTable(procGetExtendedUDPTable, family, udpTableOwnerPID)
	if err != nil {
		return nil, err
	}
	if len(buf) < 4 {
		return nil, nil
	}
	count := binary.LittleEndian.Uint32(buf[:4])
	if family == afInet {
		return parseUDP4Rows(buf, count), nil
	}
	return parseUDP6Rows(buf, count), nil
}

func callSizedTable(proc *windows.LazyProc, family, tableClass uint32) ([]byte, error) {
	var size uint32
	r1, _, callErr := proc.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(family),
		uintptr(tableClass),
		0,
	)
	if r1 != uintptr(syscall.ERROR_INSUFFICIENT_BUFFER) && r1 != 0 {
		return nil, callErr
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	r1, _, callErr = proc.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(family),
		uintptr(tableClass),
		0,
	)
	if r1 != 0 {
		return nil, callErr
	}
	return buf, nil
}

func parseTCP4Rows(buf []byte, count uint32) []winNetConn {
	rowSize := unsafe.Sizeof(mibTCPRowOwnerPID{})
	limit := boundedWindowsTableRows(buf, rowSize, count)
	out := make([]winNetConn, 0, limit)
	for i := 0; i < limit; i++ {
		row := windowsTableRowAt[mibTCPRowOwnerPID](buf, rowSize, i)
		out = append(out, winNetConn{
			LocalAddress:  ipv4FromDWORD(row.LocalAddr),
			LocalPort:     ntohs32(row.LocalPort),
			RemoteAddress: ipv4FromDWORD(row.RemoteAddr),
			RemotePort:    ntohs32(row.RemotePort),
			State:         tcpState(row.State),
			OwningProcess: int(row.OwningPID),
			Protocol:      "tcp",
		})
	}
	return out
}

func parseTCP6Rows(buf []byte, count uint32) []winNetConn {
	rowSize := unsafe.Sizeof(mibTCP6RowOwnerPID{})
	limit := boundedWindowsTableRows(buf, rowSize, count)
	out := make([]winNetConn, 0, limit)
	for i := 0; i < limit; i++ {
		row := windowsTableRowAt[mibTCP6RowOwnerPID](buf, rowSize, i)
		out = append(out, winNetConn{
			LocalAddress:  append(net.IP(nil), row.LocalAddr[:]...),
			LocalPort:     ntohs32(row.LocalPort),
			RemoteAddress: append(net.IP(nil), row.RemoteAddr[:]...),
			RemotePort:    ntohs32(row.RemotePort),
			State:         tcpState(row.State),
			OwningProcess: int(row.OwningPID),
			Protocol:      "tcp6",
		})
	}
	return out
}

func parseUDP4Rows(buf []byte, count uint32) []winNetConn {
	rowSize := unsafe.Sizeof(mibUDPRowOwnerPID{})
	limit := boundedWindowsTableRows(buf, rowSize, count)
	out := make([]winNetConn, 0, limit)
	for i := 0; i < limit; i++ {
		row := windowsTableRowAt[mibUDPRowOwnerPID](buf, rowSize, i)
		out = append(out, winNetConn{
			LocalAddress:  ipv4FromDWORD(row.LocalAddr),
			LocalPort:     ntohs32(row.LocalPort),
			State:         "LISTEN",
			OwningProcess: int(row.OwningPID),
			Protocol:      "udp",
		})
	}
	return out
}

func parseUDP6Rows(buf []byte, count uint32) []winNetConn {
	rowSize := unsafe.Sizeof(mibUDP6RowOwnerPID{})
	limit := boundedWindowsTableRows(buf, rowSize, count)
	out := make([]winNetConn, 0, limit)
	for i := 0; i < limit; i++ {
		row := windowsTableRowAt[mibUDP6RowOwnerPID](buf, rowSize, i)
		out = append(out, winNetConn{
			LocalAddress:  append(net.IP(nil), row.LocalAddr[:]...),
			LocalPort:     ntohs32(row.LocalPort),
			State:         "LISTEN",
			OwningProcess: int(row.OwningPID),
			Protocol:      "udp6",
		})
	}
	return out
}

func boundedWindowsTableRows(buf []byte, rowSize uintptr, count uint32) int {
	if len(buf) <= 4 || rowSize == 0 {
		return 0
	}
	maxRows := (len(buf) - 4) / int(rowSize)
	if count < uint32(maxRows) {
		return int(count)
	}
	return maxRows
}

func windowsTableRowAt[T any](buf []byte, rowSize uintptr, index int) *T {
	offset := 4 + index*int(rowSize)
	return (*T)(unsafe.Pointer(unsafe.Add(unsafe.Pointer(unsafe.SliceData(buf)), offset)))
}

func (w *winBackend) event(kind string, c winNetConn, now time.Time) ConnectionEvent {
	ev := ConnectionEvent{
		Kind:      kind,
		PID:       c.OwningProcess,
		SrcIP:     c.LocalAddress,
		SrcPort:   c.LocalPort,
		DstIP:     c.RemoteAddress,
		DstPort:   c.RemotePort,
		Protocol:  c.Protocol,
		State:     c.State,
		StartedAt: now,
	}
	if kind == "close" {
		ev.EndedAt = now
		ev.LastDataAt = now
	}
	return ev
}

func winKey(c winNetConn) string {
	return fmt.Sprintf("%s:%d|%s:%d|%s|%d", c.LocalAddress, c.LocalPort, c.RemoteAddress, c.RemotePort, c.Protocol, c.OwningProcess)
}

func ipv4FromDWORD(addr uint32) net.IP {
	return net.IPv4(byte(addr), byte(addr>>8), byte(addr>>16), byte(addr>>24))
}

func ntohs32(port uint32) uint16 {
	return windows.Ntohs(uint16(port))
}

func tcpState(state uint32) string {
	switch state {
	case 1:
		return "CLOSED"
	case 2:
		return "LISTEN"
	case 3:
		return "SYN_SENT"
	case 4:
		return "SYN_RECEIVED"
	case 5:
		return "ESTABLISHED"
	case 6:
		return "FIN_WAIT_1"
	case 7:
		return "FIN_WAIT_2"
	case 8:
		return "CLOSE_WAIT"
	case 9:
		return "CLOSING"
	case 10:
		return "LAST_ACK"
	case 11:
		return "TIME_WAIT"
	case 12:
		return "DELETE_TCB"
	default:
		return "UNKNOWN"
	}
}
