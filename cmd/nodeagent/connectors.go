package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
)

var connectorServiceSnapshot = struct {
	mu       sync.Mutex
	services []ServiceInfo
}{}

var connectorPolicySnapshot = struct {
	mu     sync.RWMutex
	policy connectordiscovery.AutoConnectPolicy
}{}

type approvedConnectorLogSourcesResponse struct {
	NodeID      string                          `json:"node_id"`
	GeneratedAt string                          `json:"generated_at"`
	Sources     []approvedConnectorLogSourceDTO `json:"sources"`
}

type approvedConnectorLogSourceDTO struct {
	ProposalRecordID string            `json:"proposal_record_id"`
	ProposalID       string            `json:"proposal_id"`
	SourceID         string            `json:"source_id,omitempty"`
	Program          string            `json:"program"`
	Type             string            `json:"type"`
	CollectMode      string            `json:"collect_mode,omitempty"`
	Paths            []string          `json:"paths"`
	Formatter        string            `json:"formatter,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

func cacheConnectorServices(services []ServiceInfo) {
	connectorServiceSnapshot.mu.Lock()
	defer connectorServiceSnapshot.mu.Unlock()
	connectorServiceSnapshot.services = append([]ServiceInfo(nil), services...)
}

func setConnectorAutoConnectPolicy(policy connectordiscovery.AutoConnectPolicy) {
	connectorPolicySnapshot.mu.Lock()
	defer connectorPolicySnapshot.mu.Unlock()
	policy.AutoConnectPrograms = append([]string(nil), policy.AutoConnectPrograms...)
	policy.ApprovalRequiredPrograms = append([]string(nil), policy.ApprovalRequiredPrograms...)
	policy.BlockedPrograms = append([]string(nil), policy.BlockedPrograms...)
	connectorPolicySnapshot.policy = policy
}

func currentConnectorAutoConnectPolicy() connectordiscovery.AutoConnectPolicy {
	connectorPolicySnapshot.mu.RLock()
	defer connectorPolicySnapshot.mu.RUnlock()
	policy := connectorPolicySnapshot.policy
	policy.AutoConnectPrograms = append([]string(nil), policy.AutoConnectPrograms...)
	policy.ApprovalRequiredPrograms = append([]string(nil), policy.ApprovalRequiredPrograms...)
	policy.BlockedPrograms = append([]string(nil), policy.BlockedPrograms...)
	return policy
}

func fetchApprovedConnectorLogSources(ctx context.Context, client *api.Client, log *zap.Logger, nodeID string, configured []config.LogSourceConfig) []config.LogSourceConfig {
	if client == nil || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := client.Do(callCtx, http.MethodGet, "/api/v1/nodes/"+nodeID+"/log-sources/approved", nil)
	if err != nil {
		log.Debug("fetch approved connector log sources failed", zap.Error(err))
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Debug("approved connector log sources unavailable", zap.Int("status", resp.StatusCode), zap.String("body", string(snippet)))
		return nil
	}
	var payload approvedConnectorLogSourcesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Debug("decode approved connector log sources failed", zap.Error(err))
		return nil
	}
	return approvedConnectorLogSourcesFromDTOs(log, payload.Sources, configured)
}

func approvedConnectorLogSourcesFromDTOs(log *zap.Logger, items []approvedConnectorLogSourceDTO, configured []config.LogSourceConfig) []config.LogSourceConfig {
	if len(items) == 0 {
		return nil
	}
	seen := logSourceProgramSet(configured)
	out := make([]config.LogSourceConfig, 0, len(items))
	for _, item := range items {
		source, ok := approvedConnectorLogSourceConfig(item)
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(source.Program))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		config.NormalizeLogSourceConfig(&source)
		out = append(out, source)
		log.Info("loaded approved connector log source",
			zap.String("program", source.Program),
			zap.String("formatter", source.Formatter),
			zap.Strings("paths", source.Paths),
			zap.String("proposal_id", item.ProposalID))
	}
	return out
}

func approvedConnectorLogSourceConfig(item approvedConnectorLogSourceDTO) (config.LogSourceConfig, bool) {
	program := strings.ToLower(strings.TrimSpace(item.Program))
	if program == "" {
		return config.LogSourceConfig{}, false
	}
	sourceType := strings.ToLower(strings.TrimSpace(item.Type))
	if sourceType == "" {
		sourceType = connectordiscovery.CollectorTypeFile
	}
	if sourceType != connectordiscovery.CollectorTypeFile {
		return config.LogSourceConfig{}, false
	}
	paths := normalizeConnectorLogSourceStrings(item.Paths, 64)
	if len(paths) == 0 {
		return config.LogSourceConfig{}, false
	}
	formatter := strings.ToLower(strings.TrimSpace(item.Formatter))
	if formatter == "" {
		formatter = "generic"
	}
	collectMode := config.NormalizeLogCollectMode(item.CollectMode)
	labels := normalizeConnectorLogSourceLabels(item.Labels)
	if item.ProposalRecordID != "" {
		labels["control_one.source_proposal_id"] = strings.TrimSpace(item.ProposalRecordID)
	}
	if item.ProposalID != "" {
		labels["control_one.source_proposal_external_id"] = strings.TrimSpace(item.ProposalID)
	}
	if item.SourceID != "" {
		labels["control_one.content_pack_source_id"] = strings.TrimSpace(item.SourceID)
	}
	labels["control_one.collect_mode"] = collectMode
	return config.LogSourceConfig{
		Program:     program,
		Type:        sourceType,
		Paths:       paths,
		Formatter:   formatter,
		CollectMode: collectMode,
		Labels:      labels,
	}, true
}

func connectorProposalsFromServices(services []ServiceInfo, existing []config.LogSourceConfig) []connectordiscovery.Proposal {
	return connectordiscovery.DiscoverLocal(connectordiscovery.Options{
		GOOS:             runtime.GOOS,
		Services:         connectorDiscoveryServices(services),
		ExistingPrograms: logSourcePrograms(existing),
		AutoConnect:      currentConnectorAutoConnectPolicy(),
	})
}

func connectorDiscoveryLogSources(log *zap.Logger, configured []config.LogSourceConfig, services []ServiceInfo) []config.LogSourceConfig {
	proposals := connectorProposalsFromServices(services, configured)
	sources := connectordiscovery.AutoLogSources(proposals)
	if len(sources) == 0 {
		log.Debug("no auto-discovered log sources eligible")
		return nil
	}
	for _, source := range sources {
		log.Info("auto-discovered local log source",
			zap.String("program", source.Program),
			zap.String("formatter", source.Formatter),
			zap.Strings("paths", source.Paths))
	}
	return sources
}

func connectorDiscoveryServices(services []ServiceInfo) []connectordiscovery.Service {
	out := make([]connectordiscovery.Service, 0, len(services))
	for _, svc := range services {
		out = append(out, connectordiscovery.Service{
			Process:     svc.Process,
			BinaryPath:  svc.BinaryPath,
			ServiceKind: svc.ServiceKind,
			Port:        svc.Port,
		})
	}
	return out
}

func logSourcePrograms(sources []config.LogSourceConfig) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		program := strings.TrimSpace(source.Program)
		if program != "" {
			out = append(out, program)
		}
	}
	return out
}

func logSourceProgramSet(sources []config.LogSourceConfig) map[string]struct{} {
	out := make(map[string]struct{}, len(sources))
	for _, program := range logSourcePrograms(sources) {
		program = strings.ToLower(strings.TrimSpace(program))
		if program != "" {
			out[program] = struct{}{}
		}
	}
	return out
}

func normalizeConnectorLogSourceStrings(values []string, limit int) []string {
	if limit <= 0 {
		limit = len(values)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeConnectorLogSourceLabels(labels map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range labels {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}
