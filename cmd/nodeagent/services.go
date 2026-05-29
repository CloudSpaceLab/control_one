package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
)

// ServiceInfo is one listening TCP service the agent observed locally. It
// mirrors the controlplane's nodeServiceItem JSON shape — keep them in sync.
// Probe fields are reserved for a future opt-in localhost HTTP probe and
// stay nil today.
type ServiceInfo struct {
	PID              int     `json:"pid"`
	Process          string  `json:"process"`
	BinaryPath       string  `json:"binary_path,omitempty"`
	ListenAddr       string  `json:"listen_addr"`
	Port             int     `json:"port"`
	ServiceKind      string  `json:"service_kind"`
	ProbeStatus      *int    `json:"probe_status,omitempty"`
	ProbeServer      *string `json:"probe_server,omitempty"`
	ProbeTitle       *string `json:"probe_title,omitempty"`
	ProbeContentType *string `json:"probe_content_type,omitempty"`
}

// servicesPayload is the request body for POST /api/v1/nodes/<id>/services.
// Empty Services means "no listeners" — the server clears the table for this
// node, so the absence of a service is itself a signal.
type servicesPayload struct {
	Services           []ServiceInfo                 `json:"services"`
	ConnectorProposals []connectordiscovery.Proposal `json:"connector_proposals,omitempty"`
}

// collectServices returns every listening TCP service on the host. It never
// returns an error: a probe failure on one platform is not a reason to skip
// the cycle entirely. Empty result is a legitimate value (no listeners).
// The actual enumeration lives in the build-tagged services_<os>.go files.
func collectServices(log *zap.Logger) []ServiceInfo {
	raw, err := collectPlatformServices()
	if err != nil {
		log.Debug("service collection partial failure", zap.Error(err))
	}
	return dedupeAndAnnotate(raw)
}

// normalizeAddr trims surrounding whitespace and the IPv6 brackets so that
// "[::]" and "::" hash to the same dedupe key, but keeps IPv4 and IPv6
// wildcards distinct — a dual-stack listener legitimately has two rows the
// graph wants to keep separate.
func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "[::]" {
		return "::"
	}
	return addr
}

// dedupeAndAnnotate collapses duplicate port+address entries. Two entries are
// considered the same service when they share a normalized listen address and
// port — PID is intentionally excluded so master/worker process families
// (e.g. nginx, gunicorn) don't produce duplicate rows. Assigns service_kind
// via the process-name heuristic and orders by port for stable diffs.
func dedupeAndAnnotate(in []ServiceInfo) []ServiceInfo {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]ServiceInfo, 0, len(in))
	for _, svc := range in {
		key := fmt.Sprintf("%s|%d", normalizeAddr(svc.ListenAddr), svc.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		if svc.ServiceKind == "" {
			svc.ServiceKind = serviceKindFor(svc.Process, svc.BinaryPath, svc.Port)
		}
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].ListenAddr < out[j].ListenAddr
	})
	return out
}

// genericInterpreterKind returns "http-app" or "https-app" for ports that
// carry an obvious protocol hint, otherwise empty so the caller falls back to
// the language-specific kind. Used for node/python/java/etc. where the
// process name alone doesn't tell us what protocol it serves.
func genericInterpreterKind(port int) string {
	switch port {
	case 80, 8000, 8080, 5000, 3000, 3001, 4000:
		return "http-app"
	case 443, 8443:
		return "https-app"
	}
	return ""
}

// serviceKindFor maps the observed process to a coarse fingerprint the
// knowledge-graph uses for grouping. The match is intentionally loose — the
// process name alone is the strongest signal, and a stray match here only
// affects how the row is *labelled* in the graph, not whether it appears.
func serviceKindFor(process, binaryPath string, port int) string {
	name := strings.ToLower(strings.TrimSpace(process))
	bin := ""
	if binaryPath != "" {
		bin = strings.ToLower(filepath.Base(binaryPath))
	}
	if name == "" {
		name = bin
	}

	switch {
	// Web servers / proxies
	case strings.Contains(name, "nginx"):
		return "nginx"
	case strings.Contains(name, "apache"), strings.Contains(name, "httpd"):
		return "apache"
	case strings.Contains(name, "caddy"):
		return "caddy"
	case strings.Contains(name, "envoy"):
		return "envoy"
	case strings.Contains(name, "haproxy"):
		return "haproxy"
	case strings.Contains(name, "traefik"):
		return "traefik"

	// Databases
	case strings.Contains(name, "postgres"):
		return "postgres"
	case strings.Contains(name, "mysqld"), strings.Contains(name, "mariadb"):
		return "mysql"
	case strings.Contains(name, "mongod"):
		return "mongodb"
	case strings.Contains(name, "redis"):
		return "redis"
	case strings.Contains(name, "memcached"):
		return "memcached"
	case strings.Contains(name, "clickhouse"):
		return "clickhouse"
	case strings.Contains(name, "cassandra"):
		return "cassandra"

	// Message brokers
	case strings.Contains(name, "rabbitmq"), strings.Contains(name, "beam.smp"):
		return "rabbitmq"
	case strings.Contains(name, "kafka"):
		return "kafka"
	case strings.Contains(name, "nats"):
		return "nats"

	// Search
	case strings.Contains(name, "elastic"):
		return "elasticsearch"
	case strings.Contains(name, "opensearch"):
		return "opensearch"

	// Infrastructure
	case strings.Contains(name, "sshd"):
		return "ssh"
	case strings.Contains(name, "docker"):
		return "docker"
	case strings.Contains(name, "containerd"):
		return "containerd"
	case strings.Contains(name, "kubelet"):
		return "kubernetes"
	case strings.Contains(name, "systemd-resolved"), strings.Contains(name, "named"), strings.Contains(name, "dnsmasq"):
		return "dns"

	// Node.js / Next.js
	case strings.Contains(name, "next-server"), strings.Contains(bin, "next-server"),
		strings.Contains(name, "next") && (port == 3000 || port == 3001):
		return "nextjs"
	case strings.Contains(name, "node"), strings.Contains(name, "bun"):
		// Generic interpreter: prefer the port-hint kind so the graph groups
		// it with other HTTP-ish workloads. Fall back to "nodejs" only when
		// the port carries no obvious protocol hint.
		if k := genericInterpreterKind(port); k != "" {
			return k
		}
		return "nodejs"

	// PHP
	case strings.Contains(name, "php-fpm"), strings.Contains(name, "php"):
		return "php"

	// Ruby web servers
	case strings.Contains(name, "puma"):
		return "puma"
	case strings.Contains(name, "unicorn"):
		return "unicorn"
	case strings.Contains(name, "passenger"):
		return "passenger"

	// Python web servers
	case strings.Contains(name, "gunicorn"):
		return "gunicorn"
	case strings.Contains(name, "uvicorn"):
		return "uvicorn"
	case strings.Contains(name, "daphne"), strings.Contains(name, "hypercorn"):
		return "python-asgi"
	case strings.Contains(name, "python"), strings.Contains(name, "python3"):
		if k := genericInterpreterKind(port); k != "" {
			return k
		}
		return "python-app"

	// JVM
	case strings.Contains(name, "java"):
		switch port {
		case 9200, 9300:
			return "elasticsearch"
		case 2181:
			return "zookeeper"
		}
		if k := genericInterpreterKind(port); k != "" {
			return k
		}
		return "java-app"

	// .NET
	case strings.Contains(name, "dotnet"):
		return "dotnet"

	// Goravel / Go apps — binary path heuristic
	case strings.Contains(name, "goravel"):
		return "goravel"
	}

	// Port-based fallback
	switch port {
	case 22:
		return "ssh"
	case 53:
		return "dns"
	case 80, 8080:
		return "http"
	case 443, 8443:
		return "https"
	case 3306:
		return "mysql"
	case 5432:
		return "postgres"
	case 6379:
		return "redis"
	case 27017:
		return "mongodb"
	case 9200, 9300:
		return "elasticsearch"
	case 5672, 15672:
		return "rabbitmq"
	case 9092:
		return "kafka"
	case 4222:
		return "nats"
	case 2375, 2376:
		return "docker"
	case 6443:
		return "kubernetes"
	case 2181:
		return "zookeeper"
	case 8123, 9000:
		return "clickhouse"
	}
	return "unknown"
}

func runServiceCollector(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, interval time.Duration, logSources func() []config.LogSourceConfig) {
	logger := log.Named("services")
	logger.Info("starting service collector",
		zap.String("node_id", nodeID),
		zap.Duration("interval", interval),
	)

	// Stagger the first run so heartbeat + telemetry + services don't all
	// fire at second 0 after enrollment.
	first := time.NewTimer(15 * time.Second)
	defer first.Stop()

	tick := func() {
		services := collectServices(logger)
		cacheConnectorServices(services)
		var currentLogSources []config.LogSourceConfig
		if logSources != nil {
			currentLogSources = logSources()
		}
		proposals := connectorProposalsFromServices(services, currentLogSources)
		if err := postServices(ctx, client, logger, nodeID, services, proposals); err != nil {
			logger.Debug("post services failed", zap.Error(err))
			return
		}
		logger.Debug("services posted", zap.Int("count", len(services)), zap.Int("connector_proposals", len(proposals)))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("service collector stopped")
			return
		case <-first.C:
			tick()
		case <-ticker.C:
			tick()
		}
	}
}

func postServices(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, services []ServiceInfo, proposals []connectordiscovery.Proposal) error {
	body, err := json.Marshal(servicesPayload{Services: services, ConnectorProposals: proposals})
	if err != nil {
		return fmt.Errorf("marshal services: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := client.Do(callCtx, http.MethodPost, "/api/v1/nodes/"+nodeID+"/services", body)
	if err != nil {
		return fmt.Errorf("post services: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("services status %d: %s", resp.StatusCode, string(snippet))
	}
	return nil
}
