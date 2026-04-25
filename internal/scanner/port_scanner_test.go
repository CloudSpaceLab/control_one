package scanner

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPortScannerOpenCase(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port

	sc := NewPortScanner("127.0.0.1", 500*time.Millisecond, 4)
	results := sc.Run(context.Background(), []PortRule{{ID: "r1", Port: port, Protocol: "tcp", ExpectedState: "open"}})
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if !results[0].Matched {
		t.Fatalf("rule should match: %+v", results[0])
	}
	if results[0].Observed != "open" {
		t.Fatalf("observed want open, got %s", results[0].Observed)
	}
}

func TestPortScannerClosedCase(t *testing.T) {
	// Use a likely-free high port.
	port := findFreePort(t)
	sc := NewPortScanner("127.0.0.1", 300*time.Millisecond, 4)
	results := sc.Run(context.Background(), []PortRule{{ID: "r1", Port: port, Protocol: "tcp", ExpectedState: "closed"}})
	if !results[0].Matched {
		t.Fatalf("expected match (closed), got %+v", results[0])
	}
}

func TestPortScannerMismatch(t *testing.T) {
	port := findFreePort(t)
	sc := NewPortScanner("127.0.0.1", 300*time.Millisecond, 4)
	results := sc.Run(context.Background(), []PortRule{{ID: "r1", Port: port, Protocol: "tcp", ExpectedState: "open"}})
	if results[0].Matched {
		t.Fatal("expected mismatch")
	}
	if !strings.Contains(results[0].Error, "expected open") {
		t.Fatalf("unexpected error: %q", results[0].Error)
	}
}

func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
}
