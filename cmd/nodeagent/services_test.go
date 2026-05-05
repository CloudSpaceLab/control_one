package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

func TestServiceKindForByProcess(t *testing.T) {
	t.Parallel()

	cases := []struct {
		proc string
		path string
		port int
		want string
	}{
		{"nginx", "", 80, "nginx"},
		{"NGINX", "", 80, "nginx"},
		{"postgres", "", 5432, "postgres"},
		{"postgresql", "", 5432, "postgres"},
		{"mysqld", "", 3306, "mysql"},
		{"redis-server", "", 6379, "redis"},
		{"sshd", "", 22, "ssh"},
		// Generic interpreter falls through to a port-hint kind.
		{"node", "", 3000, "http-app"},
		{"python", "", 8443, "https-app"},
		{"java", "", 8080, "http-app"},
		// Unknown process, but well-known port hints.
		{"weird-bin", "/opt/weird-bin", 5432, "postgres"},
		{"", "/usr/sbin/sshd", 22, "ssh"},
		{"random", "", 9999, "unknown"},
	}
	for _, tc := range cases {
		got := serviceKindFor(tc.proc, tc.path, tc.port)
		if got != tc.want {
			t.Errorf("serviceKindFor(%q, %q, %d) = %q, want %q", tc.proc, tc.path, tc.port, got, tc.want)
		}
	}
}

func TestDedupeAndAnnotateAssignsKindAndDedupes(t *testing.T) {
	t.Parallel()

	in := []ServiceInfo{
		{PID: 100, Process: "nginx", ListenAddr: "0.0.0.0", Port: 80},
		{PID: 100, Process: "nginx", ListenAddr: "0.0.0.0", Port: 80}, // dup, dropped
		{PID: 100, Process: "nginx", ListenAddr: "::", Port: 80},      // distinct addr, kept
		{PID: 200, Process: "postgres", ListenAddr: "127.0.0.1", Port: 5432},
	}
	out := dedupeAndAnnotate(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 unique services after dedupe, got %d", len(out))
	}
	for _, svc := range out {
		switch svc.Process {
		case "nginx":
			if svc.ServiceKind != "nginx" {
				t.Errorf("nginx → kind=%q want nginx", svc.ServiceKind)
			}
		case "postgres":
			if svc.ServiceKind != "postgres" {
				t.Errorf("postgres → kind=%q want postgres", svc.ServiceKind)
			}
		}
	}
	// Output must be ordered by port then listen_addr — invariant the
	// server depends on for stable diffs.
	for i := 1; i < len(out); i++ {
		prev, cur := out[i-1], out[i]
		if prev.Port > cur.Port || (prev.Port == cur.Port && prev.ListenAddr > cur.ListenAddr) {
			t.Fatalf("dedupeAndAnnotate not sorted: %+v then %+v", prev, cur)
		}
	}
}

// TestPostServicesPostsExpectedShape exercises the full client → server
// roundtrip against an httptest server, asserting the wire shape matches
// what the controlplane handleNodeServicesIngest expects.
func TestPostServicesPostsExpectedShape(t *testing.T) {
	t.Parallel()

	type recv struct {
		method string
		path   string
		body   servicesPayload
	}
	var got recv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	services := []ServiceInfo{
		{PID: 1, Process: "nginx", ListenAddr: "0.0.0.0", Port: 80, ServiceKind: "nginx"},
	}
	if err := postServices(ctx, client, zap.NewNop(), "node-id-123", services); err != nil {
		t.Fatalf("postServices: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q want POST", got.method)
	}
	if got.path != "/api/v1/nodes/node-id-123/services" {
		t.Errorf("path = %q", got.path)
	}
	if len(got.body.Services) != 1 || got.body.Services[0].Process != "nginx" {
		t.Errorf("body = %+v", got.body)
	}
}
