// Package networkcollector implements site-local, read-only SNMP collection.
package networkcollector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type Target struct {
	ID               string                                    `yaml:"target_id"`
	Address          string                                    `yaml:"address"`
	Port             int                                       `yaml:"port"`
	Username         string                                    `yaml:"username"`
	AuthProtocol     string                                    `yaml:"auth_protocol"`
	AuthSecretEnv    string                                    `yaml:"auth_secret_env"`
	PrivSecretEnv    string                                    `yaml:"priv_secret_env"`
	TrapCommunityEnv string                                    `yaml:"trap_community_env"`
	Sources          map[string]networkdevice.ManagementConfig `yaml:"sources"`
}
type Config struct {
	ControlPlaneURL   string   `yaml:"control_plane_url"`
	TenantID          string   `yaml:"tenant_id"`
	CollectorID       string   `yaml:"collector_id"`
	TokenEnv          string   `yaml:"token_env"`
	CAFile            string   `yaml:"ca_file"`
	IntervalSeconds   int      `yaml:"interval_seconds"`
	AllowedCIDRs      []string `yaml:"allowed_cidrs"`
	Targets           []Target `yaml:"targets"`
	TrapListenAddress string   `yaml:"trap_listen_address"`
}
type binding struct {
	ID            string `json:"id"`
	TargetID      string `json:"target_id"`
	SourceType    string `json:"source_type"`
	SenderAddress string `json:"sender_address"`
}
type Collector struct {
	cfg      Config
	client   *api.Client
	token    string
	targets  map[string]Target
	pending  map[string]networkdevice.SourceReport
	poll     func(context.Context, string, int, networkdevice.Credential) (string, map[string]float64)
	queued   atomic.Int64
	assigned atomic.Int64
	degraded atomic.Bool
	traps    chan trapDatagram
	dropped  atomic.Int64
}

func Load(path string) (Config, error) {
	var cfg Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	d := yaml.NewDecoder(io.LimitReader(f, 1<<20))
	d.KnownFields(true)
	if err = d.Decode(&cfg); err != nil {
		return cfg, errors.New("invalid network collector configuration")
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return cfg, errors.New("expected one configuration document")
	}
	return cfg, nil
}
func New(cfg Config) (*Collector, error) {
	u, err := url.Parse(cfg.ControlPlaneURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid control plane URL")
	}
	if _, err = uuid.Parse(cfg.TenantID); err != nil {
		return nil, errors.New("tenant UUID required")
	}
	if cfg.CollectorID == "" || len(cfg.CollectorID) > 255 || strings.ContainsAny(cfg.CollectorID, "\r\n\x00") {
		return nil, errors.New("collector ID required")
	}
	if cfg.IntervalSeconds == 0 {
		cfg.IntervalSeconds = 30
	}
	if cfg.IntervalSeconds < 5 || cfg.IntervalSeconds > 3600 {
		return nil, errors.New("interval must be 5..3600 seconds")
	}
	if len(cfg.AllowedCIDRs) == 0 {
		return nil, errors.New("explicit destination allowlist required")
	}
	for _, s := range cfg.AllowedCIDRs {
		if _, _, err = net.ParseCIDR(s); err != nil {
			return nil, errors.New("invalid allowed CIDR")
		}
	}
	token := os.Getenv(cfg.TokenEnv)
	if token == "" {
		return nil, errors.New("collector token environment variable is missing")
	}
	if len(cfg.Targets) > 1000 {
		return nil, errors.New("at most 1000 local targets supported")
	}
	targets := map[string]Target{}
	for _, t := range cfg.Targets {
		id, err := uuid.Parse(t.ID)
		if err != nil || id == uuid.Nil {
			return nil, errors.New("invalid target UUID")
		}
		t.ID = id.String()
		if _, ok := targets[t.ID]; ok {
			return nil, errors.New("duplicate local target")
		}
		if t.Address == "" || t.Port < 0 || t.Port > 65535 {
			return nil, errors.New("invalid local destination")
		}
		c := credential(t)
		if t.Username != "" && c.Validate("snmpv3") != nil {
			return nil, errors.New("local SNMPv3 credential configuration is invalid")
		}
		for source, adapter := range t.Sources {
			if err := adapter.Validate(source); err != nil {
				return nil, fmt.Errorf("invalid local %s configuration: %w", source, err)
			}
		}
		targets[t.ID] = t
	}
	client, err := api.NewClient(strings.TrimRight(cfg.ControlPlaneURL, "/"), "", "", cfg.CAFile, "")
	if err != nil {
		return nil, err
	}
	if cfg.TrapListenAddress != "" {
		if _, err := net.ResolveUDPAddr("udp", cfg.TrapListenAddress); err != nil {
			return nil, errors.New("invalid trap listen address")
		}
	}
	return &Collector{cfg: cfg, client: client, token: token, targets: targets, pending: map[string]networkdevice.SourceReport{}, poll: networkdevice.PollSNMP, traps: make(chan trapDatagram, 256)}, nil
}
func credential(t Target) networkdevice.Credential {
	return networkdevice.Credential{Username: t.Username, AuthProtocol: t.AuthProtocol, AuthSecret: os.Getenv(t.AuthSecretEnv), PrivProtocol: "AES", PrivSecret: os.Getenv(t.PrivSecretEnv)}
}
func (c *Collector) request(ctx context.Context, method, action string, payload any) ([]byte, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	path := "/api/v1/content-packs/collectors/" + url.PathEscape(c.cfg.CollectorID) + "/" + action
	sep := "?"
	if strings.Contains(action, "?") {
		sep = "&"
	}
	path += sep + "tenant_id=" + url.QueryEscape(c.cfg.TenantID)
	resp, err := c.client.DoWithHeaders(ctx, method, path, body, map[string]string{"Authorization": "Bearer " + c.token, "X-ControlOne-Collector-ID": c.cfg.CollectorID})
	if err != nil {
		return nil, errors.New("collector API unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("collector API rejected request (%d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if len(data) > 1<<20 {
		return nil, errors.New("collector response exceeds limit")
	}
	return data, err
}
func (c *Collector) bindings(ctx context.Context) ([]binding, error) {
	out := []binding{}
	offset := 0
	for page := 0; page < 11; page++ {
		raw, err := c.request(ctx, http.MethodGet, fmt.Sprintf("network-bindings?offset=%d", offset), nil)
		if err != nil {
			return nil, err
		}
		var p struct {
			Data []binding `json:"data"`
			Next *int      `json:"next_offset"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil, errors.New("invalid binding response")
		}
		out = append(out, p.Data...)
		if len(out) > 1000 {
			return nil, errors.New("too many assigned bindings")
		}
		if p.Next == nil {
			return out, nil
		}
		if *p.Next <= offset {
			return nil, errors.New("invalid binding pagination")
		}
		offset = *p.Next
	}
	return nil, errors.New("binding pagination exceeded limit")
}

// Sync polls only current tenant/collector assignments. The local configuration
// is authoritative for destinations and credentials; API data never supplies them.
func (c *Collector) Sync(ctx context.Context) error {
	bindings, err := c.bindings(ctx)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, b := range bindings {
		if b.SourceType == "snmp_poll" || b.SourceType == "snmp_trap" || b.SourceType == "ssh_config" || b.SourceType == "netconf" || b.SourceType == "restconf" || b.SourceType == "vendor_api" {
			active[b.ID] = true
		}
	}
	for id := range c.pending {
		if !active[id] {
			delete(c.pending, id)
		}
	}
	c.queued.Store(int64(len(c.pending)))
	c.assigned.Store(int64(len(active)))
	c.drainTraps(bindings)
	for _, b := range bindings {
		if b.SourceType == "snmp_trap" || !active[b.ID] {
			continue
		}
		if _, err := uuid.Parse(b.ID); err != nil {
			return errors.New("invalid binding UUID")
		}
		report := networkdevice.SourceReport{BindingID: b.ID, State: "not_configured"}
		t, ok := c.targets[b.TargetID]
		if !ok {
			report.State = "policy_blocked"
		} else {
			resolveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			ip, err := networkdevice.Resolve(resolveCtx, t.Address, c.cfg.AllowedCIDRs)
			cancel()
			if err != nil {
				report.State = err.Error()
			} else if b.SourceType == "snmp_poll" && credential(t).Validate("snmpv3") != nil {
				report.State = "auth_failed"
			} else if b.SourceType == "snmp_poll" {
				report.State, report.Metrics = c.poll(ctx, ip, t.Port, credential(t))
			} else if adapter, ok := t.Sources[b.SourceType]; ok {
				report.State, report.Records = networkdevice.PollManagement(ctx, t.Address, ip, b.SourceType, adapter)
			} else {
				report.State = "policy_blocked"
			}
		}
		report.ObservedAt = time.Now().UTC()
		if report.State == "ready" || report.State == "partial" {
			contact := report.ObservedAt
			report.LastContactAt = &contact
		}
		c.pending[b.ID] = report
		c.queued.Store(int64(len(c.pending)))
	}
	var sendErr error
	for id, report := range c.pending {
		report.QueueDepth = int64(len(c.pending))
		report.LagMillis = time.Since(report.ObservedAt).Milliseconds()
		if _, err := c.request(ctx, http.MethodPost, "network-reports", map[string]any{"reports": []networkdevice.SourceReport{report}}); err != nil {
			sendErr = err
			continue
		}
		delete(c.pending, id)
		c.queued.Store(int64(len(c.pending)))
	}
	c.degraded.Store(sendErr != nil)
	heartbeatErr := c.heartbeat(ctx)
	if sendErr != nil {
		return sendErr
	}
	return heartbeatErr
}
func (c *Collector) heartbeat(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status := "healthy"
	if c.degraded.Load() {
		status = "degraded"
	}
	_, err := c.request(ctx, http.MethodPost, "heartbeat", map[string]any{"status": status, "health": map[string]any{"network_collector": map[string]any{"queue_depth": c.queued.Load() + int64(len(c.traps)), "dropped_traps": c.dropped.Load(), "assigned_sources": c.assigned.Load(), "interval_seconds": c.cfg.IntervalSeconds}}})
	return err
}
func (c *Collector) Run(ctx context.Context, onError func(error)) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.cfg.TrapListenAddress != "" {
		listener, err := net.ListenPacket("udp", c.cfg.TrapListenAddress)
		if err != nil {
			if onError != nil {
				onError(errors.New("trap listener unavailable"))
			}
			return
		}
		defer listener.Close()
		stopped := context.AfterFunc(ctx, func() { _ = listener.Close() })
		defer stopped()
		trapDone := make(chan struct{})
		go func() { defer close(trapDone); c.receiveTraps(ctx, listener) }()
		defer func() { _ = listener.Close(); <-trapDone }()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = c.heartbeat(ctx)
			}
		}
	}()
	defer func() { cancel(); <-done }()
	ticker := time.NewTicker(time.Duration(c.cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		if err := c.Sync(ctx); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
