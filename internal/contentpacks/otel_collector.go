package contentpacks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	defaultOTelExporterID              = "otlp/controlone"
	defaultOTelHealthCheckExtensionID  = "health_check"
	defaultOTelFileStorageExtensionID  = "file_storage/controlone"
	defaultOTelMemoryLimiterProcessor  = "memory_limiter"
	defaultOTelBatchProcessor          = "batch"
	defaultOTelCommonResourceProcessor = "resource/controlone"
	defaultOTelRedactedRawLogBody      = "raw log omitted by collect_parsed"
	OTelCollectModeCollectRaw          = "collect_raw"
	OTelCollectModeCollectParsed       = "collect_parsed"
)

var otelReceiverTypePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type OTelCollectorConfigOptions struct {
	Endpoint                    string
	TenantID                    string
	CollectorID                 string
	Headers                     map[string]string
	InsecureTLS                 bool
	Compression                 string
	Timeout                     string
	MemoryLimitMiB              int
	MemorySpikeLimitMiB         int
	BatchTimeout                string
	BatchSendBatchSize          int
	DisablePersistentStorage    bool
	StorageExtensionID          string
	StorageDirectory            string
	ExporterQueueSize           int
	ExporterQueueConsumers      int
	ExporterRetryMaxElapsedTime string
	SourceApprovalRefs          map[string]string
	AdditionalAttributes        map[string]string
}

type OTelCollectorConfigSource struct {
	Source      ResolvedSource
	Mode        string
	CollectMode string
	ApprovalRef string
}

type OTelCollectorConfigPlan struct {
	Sources  []OTelCollectorSourcePlan `json:"sources"`
	Warnings []string                  `json:"warnings,omitempty"`
	Config   OTelCollectorConfig       `json:"config"`
}

type OTelCollectorSourcePlan struct {
	SourceID            string                          `json:"source_id"`
	PackID              string                          `json:"pack_id,omitempty"`
	PackVersion         string                          `json:"pack_version,omitempty"`
	Mode                string                          `json:"mode"`
	Receiver            string                          `json:"receiver"`
	ReceiverIDs         []string                        `json:"receiver_ids"`
	PipelineID          string                          `json:"pipeline_id"`
	PipelineType        string                          `json:"pipeline_type"`
	ResourceProcessorID string                          `json:"resource_processor_id"`
	CollectMode         string                          `json:"collect_mode,omitempty"`
	ApprovalRef         string                          `json:"approval_ref,omitempty"`
	Warnings            []string                        `json:"warnings,omitempty"`
	EdgeNetworkPolicies []OTelEdgeNetworkPolicyTemplate `json:"edge_network_policies,omitempty"`
}

type OTelEdgeNetworkPolicyTemplate struct {
	Kind               string   `json:"kind"`
	SourceID           string   `json:"source_id"`
	ReceiverID         string   `json:"receiver_id"`
	ListenAddress      string   `json:"listen_address"`
	Transport          string   `json:"transport"`
	AllowlistCIDRs     []string `json:"allowlist_cidrs,omitempty"`
	RateLimitPerSecond int      `json:"rate_limit_per_second,omitempty"`
	RateLimitBurst     int      `json:"rate_limit_burst,omitempty"`
	NftablesRules      []string `json:"nftables_rules,omitempty"`
}

type OTelCollectorConfig struct {
	Extensions map[string]any            `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	Receivers  map[string]map[string]any `json:"receivers" yaml:"receivers"`
	Processors map[string]any            `json:"processors,omitempty" yaml:"processors,omitempty"`
	Exporters  map[string]map[string]any `json:"exporters" yaml:"exporters"`
	Service    OTelServiceConfig         `json:"service" yaml:"service"`
}

type OTelServiceConfig struct {
	Extensions []string                      `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	Pipelines  map[string]OTelPipelineConfig `json:"pipelines" yaml:"pipelines"`
	Telemetry  map[string]map[string]any     `json:"telemetry,omitempty" yaml:"telemetry,omitempty"`
}

type OTelPipelineConfig struct {
	Receivers  []string `json:"receivers" yaml:"receivers"`
	Processors []string `json:"processors,omitempty" yaml:"processors,omitempty"`
	Exporters  []string `json:"exporters" yaml:"exporters"`
}

type otelReceiverBuild struct {
	ID       string
	Type     string
	Config   map[string]any
	Warnings []string
	Policy   *OTelEdgeNetworkPolicyTemplate
}

func BuildOTelCollectorConfig(sources []OTelCollectorConfigSource, opts OTelCollectorConfigOptions) (OTelCollectorConfigPlan, error) {
	if len(sources) == 0 {
		return OTelCollectorConfigPlan{}, fmt.Errorf("at least one content-pack source is required")
	}
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		return OTelCollectorConfigPlan{}, fmt.Errorf("OTel exporter endpoint is required")
	}

	plan := OTelCollectorConfigPlan{
		Config: OTelCollectorConfig{
			Extensions: map[string]any{
				defaultOTelHealthCheckExtensionID: map[string]any{},
			},
			Receivers:  map[string]map[string]any{},
			Processors: map[string]any{},
			Exporters: map[string]map[string]any{
				defaultOTelExporterID: otelExporterConfig(opts),
			},
			Service: OTelServiceConfig{
				Extensions: []string{defaultOTelHealthCheckExtensionID},
				Pipelines:  map[string]OTelPipelineConfig{},
			},
		},
	}
	plan.Config.Processors[defaultOTelMemoryLimiterProcessor] = otelMemoryLimiterConfig(opts)
	plan.Config.Processors[defaultOTelBatchProcessor] = otelBatchConfig(opts)
	commonResourceProcessorID := addCommonOTelResourceProcessor(plan.Config.Processors, opts)
	storageExtensionID := ensureOTelPersistentStorage(&plan.Config, opts)

	ordered := append([]OTelCollectorConfigSource(nil), sources...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return compareOTelSourceRequest(ordered[i], ordered[j]) < 0
	})

	for _, request := range ordered {
		source := request.Source.Source
		sourceID := strings.TrimSpace(source.SourceID)
		if sourceID == "" {
			return OTelCollectorConfigPlan{}, fmt.Errorf("content-pack source_id is required")
		}
		approvalRef := strings.TrimSpace(request.ApprovalRef)
		if approvalRef == "" {
			approvalRef = strings.TrimSpace(opts.SourceApprovalRefs[sourceID])
		}
		if (source.ApprovalRequired || requiresApproval(source.RiskClass, source.DataSensitivity)) && approvalRef == "" {
			return OTelCollectorConfigPlan{}, fmt.Errorf("source %s requires approval before OTel collector config can be rendered", sourceID)
		}
		collectMode, err := normalizeOTelCollectMode(request.CollectMode)
		if err != nil {
			return OTelCollectorConfigPlan{}, fmt.Errorf("source %s: %w", sourceID, err)
		}

		recipe, err := selectOTelCollectorRecipe(source, request.Mode)
		if err != nil {
			return OTelCollectorConfigPlan{}, err
		}
		receivers, pipelineType, err := buildOTelReceivers(request.Source, recipe, storageExtensionID)
		if err != nil {
			return OTelCollectorConfigPlan{}, err
		}

		receiverIDs := make([]string, 0, len(receivers))
		receiverType := ""
		sourceWarnings := []string(nil)
		edgeNetworkPolicies := []OTelEdgeNetworkPolicyTemplate(nil)
		for _, receiver := range receivers {
			if _, exists := plan.Config.Receivers[receiver.ID]; exists {
				return OTelCollectorConfigPlan{}, fmt.Errorf("duplicate OTel receiver id %s", receiver.ID)
			}
			plan.Config.Receivers[receiver.ID] = receiver.Config
			receiverIDs = append(receiverIDs, receiver.ID)
			if receiverType == "" {
				receiverType = receiver.Type
			}
			sourceWarnings = append(sourceWarnings, receiver.Warnings...)
			if receiver.Policy != nil {
				edgeNetworkPolicies = append(edgeNetworkPolicies, *receiver.Policy)
			}
		}
		sort.Strings(receiverIDs)

		sourceProcessorID := "resource/controlone.source." + otelComponentName(sourceID)
		if _, exists := plan.Config.Processors[sourceProcessorID]; exists {
			return OTelCollectorConfigPlan{}, fmt.Errorf("duplicate OTel source processor id %s", sourceProcessorID)
		}
		plan.Config.Processors[sourceProcessorID] = map[string]any{
			"attributes": otelResourceAttributes(sourceOTelAttributes(request.Source, recipe.Mode, collectMode, approvalRef)),
		}
		redactionProcessorID, redactionWarnings, err := addOTelRawRedactionProcessor(plan.Config.Processors, sourceID, collectMode)
		if err != nil {
			return OTelCollectorConfigPlan{}, err
		}
		sourceWarnings = append(sourceWarnings, redactionWarnings...)

		pipelineID := pipelineType + "/controlone." + otelComponentName(sourceID)
		if _, exists := plan.Config.Service.Pipelines[pipelineID]; exists {
			return OTelCollectorConfigPlan{}, fmt.Errorf("duplicate OTel pipeline id %s", pipelineID)
		}
		processors := []string{defaultOTelMemoryLimiterProcessor}
		if commonResourceProcessorID != "" {
			processors = append(processors, commonResourceProcessorID)
		}
		processors = append(processors, sourceProcessorID)
		if redactionProcessorID != "" {
			processors = append(processors, redactionProcessorID)
		}
		processors = append(processors, defaultOTelBatchProcessor)
		plan.Config.Service.Pipelines[pipelineID] = OTelPipelineConfig{
			Receivers:  receiverIDs,
			Processors: processors,
			Exporters:  []string{defaultOTelExporterID},
		}
		plan.Sources = append(plan.Sources, OTelCollectorSourcePlan{
			SourceID:            sourceID,
			PackID:              strings.TrimSpace(request.Source.PackID),
			PackVersion:         strings.TrimSpace(request.Source.PackVersion),
			Mode:                strings.TrimSpace(recipe.Mode),
			Receiver:            receiverType,
			ReceiverIDs:         receiverIDs,
			PipelineID:          pipelineID,
			PipelineType:        pipelineType,
			ResourceProcessorID: sourceProcessorID,
			CollectMode:         collectMode,
			ApprovalRef:         approvalRef,
			Warnings:            sourceWarnings,
			EdgeNetworkPolicies: edgeNetworkPolicies,
		})
		plan.Warnings = append(plan.Warnings, sourceWarnings...)
	}

	sort.Strings(plan.Warnings)
	return plan, nil
}

func RenderOTelCollectorConfigYAML(plan OTelCollectorConfigPlan) ([]byte, error) {
	if len(plan.Config.Receivers) == 0 {
		return nil, fmt.Errorf("OTel collector config has no receivers")
	}
	if len(plan.Config.Exporters) == 0 {
		return nil, fmt.Errorf("OTel collector config has no exporters")
	}
	if len(plan.Config.Service.Pipelines) == 0 {
		return nil, fmt.Errorf("OTel collector config has no service pipelines")
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(plan.Config); err != nil {
		return nil, fmt.Errorf("render OTel collector config YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close OTel collector config YAML encoder: %w", err)
	}
	return buf.Bytes(), nil
}

func OTelCollectorConfigVersion(renderedYAML []byte) string {
	if len(renderedYAML) == 0 {
		return ""
	}
	sum := sha256.Sum256(renderedYAML)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selectOTelCollectorRecipe(source SourceProfile, requestedMode string) (CollectorRecipe, error) {
	requestedMode = strings.TrimSpace(requestedMode)
	if requestedMode != "" {
		for _, recipe := range source.CollectorRecipes {
			if strings.TrimSpace(recipe.Mode) == requestedMode {
				return normalizeOTelCollectorRecipe(recipe)
			}
		}
		if sourceHasCollectorMode(source, requestedMode) {
			return normalizeOTelCollectorRecipe(CollectorRecipe{Mode: requestedMode})
		}
		return CollectorRecipe{}, fmt.Errorf("source %s does not advertise collector mode %s", source.SourceID, requestedMode)
	}
	for _, recipe := range source.CollectorRecipes {
		if isOTelRenderableMode(recipe.Mode) {
			return normalizeOTelCollectorRecipe(recipe)
		}
	}
	for _, mode := range source.CollectorModes {
		if isOTelRenderableMode(mode) {
			return normalizeOTelCollectorRecipe(CollectorRecipe{Mode: mode})
		}
	}
	return CollectorRecipe{}, fmt.Errorf("source %s has no OTel-renderable collector recipe", source.SourceID)
}

func normalizeOTelCollectorRecipe(recipe CollectorRecipe) (CollectorRecipe, error) {
	recipe.Mode = strings.TrimSpace(recipe.Mode)
	if !isOTelRenderableMode(recipe.Mode) {
		return CollectorRecipe{}, fmt.Errorf("collector mode %s is not renderable as OTel collector config", recipe.Mode)
	}
	recipe.Receiver = strings.TrimSpace(recipe.Receiver)
	if recipe.Receiver == "" {
		recipe.Receiver = defaultOTelReceiverForMode(recipe.Mode)
	}
	recipe.Receiver = strings.ToLower(strings.TrimSpace(recipe.Receiver))
	if recipe.Mode == CollectorWindowsEvent && recipe.Receiver == "windowseventlog" {
		recipe.Receiver = "windows_event_log"
	}
	if recipe.Receiver == "" {
		return CollectorRecipe{}, fmt.Errorf("collector mode %s requires an explicit OTel receiver", recipe.Mode)
	}
	if !otelReceiverTypePattern.MatchString(recipe.Receiver) {
		return CollectorRecipe{}, fmt.Errorf("OTel receiver %q must contain only lowercase letters, digits, or underscore", recipe.Receiver)
	}
	recipe.Config = cloneAnyMap(recipe.Config)
	return recipe, nil
}

func buildOTelReceivers(source ResolvedSource, recipe CollectorRecipe, storageExtensionID string) ([]otelReceiverBuild, string, error) {
	switch strings.TrimSpace(recipe.Mode) {
	case CollectorOTelFileLog:
		return buildOTelFileLogReceiver(source, recipe, storageExtensionID)
	case CollectorSyslog:
		return buildOTelSyslogReceiver(source, recipe)
	case CollectorWindowsEvent:
		return buildOTelWindowsEventReceivers(source, recipe, storageExtensionID)
	case CollectorWEF:
		return buildOTelWindowsEventReceivers(source, recipe, storageExtensionID)
	case CollectorOTLP:
		return buildOTelOTLPReceiver(source, recipe)
	case CollectorSplunkHEC, CollectorKafka, CollectorPrometheus:
		return buildExplicitOTelReceiver(source, recipe)
	default:
		return nil, "", fmt.Errorf("collector mode %s is not supported by the OTel renderer", recipe.Mode)
	}
}

func buildOTelFileLogReceiver(source ResolvedSource, recipe CollectorRecipe, storageExtensionID string) ([]otelReceiverBuild, string, error) {
	config := cloneOTelConfig(recipe.Config)
	include := stringSliceConfig(config, "include")
	if len(include) == 0 {
		include = metadataStringSlice(source.Source.Metadata, "include", "path", "paths", "log_path", "log_paths")
	}
	include = dedupeSorted(include)
	if len(include) == 0 {
		return nil, "", fmt.Errorf("source %s otel_filelog recipe requires include paths", source.Source.SourceID)
	}
	config["include"] = include
	delete(config, "path")
	delete(config, "paths")
	delete(config, "log_path")
	delete(config, "log_paths")
	if _, ok := config["start_at"]; !ok {
		config["start_at"] = "end"
	}
	if _, ok := config["include_file_path"]; !ok {
		config["include_file_path"] = true
	}
	if _, ok := config["include_file_name"]; !ok {
		config["include_file_name"] = true
	}
	applyOTelReceiverStorage(config, storageExtensionID)
	applyOTelReceiverRetry(config)
	return []otelReceiverBuild{{
		ID:     otelReceiverID(recipe.Receiver, source.Source.SourceID),
		Type:   recipe.Receiver,
		Config: config,
	}}, "logs", nil
}

func buildOTelSyslogReceiver(source ResolvedSource, recipe CollectorRecipe) ([]otelReceiverBuild, string, error) {
	config := cloneOTelConfig(recipe.Config)
	transport := strings.ToLower(strings.TrimSpace(stringConfig(config, "transport")))
	protocol := strings.ToLower(strings.TrimSpace(stringConfig(config, "protocol")))
	if protocol == "tcp" || protocol == "udp" {
		if transport == "" {
			transport = protocol
		}
		protocol = ""
	}
	tlsRequested := false
	if protocol == "tls" || protocol == "tcp_tls" {
		if transport == "" {
			transport = "tcp"
		}
		tlsRequested = true
		protocol = ""
	}
	if transport == "tls" || transport == "tcp_tls" {
		transport = "tcp"
		tlsRequested = true
	}
	if transport == "" {
		if _, ok := config["udp"]; ok {
			transport = "udp"
		} else {
			transport = "tcp"
		}
	}
	if transport != "tcp" && transport != "udp" {
		return nil, "", fmt.Errorf("source %s syslog transport must be tcp or udp", source.Source.SourceID)
	}
	if syslogHasTLSConfig(config, transport) {
		tlsRequested = true
	}
	delete(config, "transport")
	listenAddress := strings.TrimSpace(stringConfig(config, "listen_address"))
	delete(config, "listen_address")
	if listenAddress == "" {
		listenAddress = metadataString(source.Source.Metadata, "listen_address", "syslog_listen_address")
	}
	endpointConfig := otelNestedMapConfig(config, transport)
	if _, hasListenAddress := endpointConfig["listen_address"]; !hasListenAddress {
		if listenAddress == "" {
			listenAddress = defaultSyslogListenAddress(transport, tlsRequested)
		}
		endpointConfig["listen_address"] = listenAddress
	}
	endpointConfig["add_attributes"] = otelBoolDefault(endpointConfig["add_attributes"], true)
	config[transport] = endpointConfig
	removeInactiveSyslogTransport(config, transport)

	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(metadataString(source.Source.Metadata, "protocol", "syslog_protocol")))
	}
	if protocol == "" {
		protocol = "rfc5424"
	}
	if protocol != "rfc3164" && protocol != "rfc5424" {
		return nil, "", fmt.Errorf("source %s syslog protocol must be rfc3164 or rfc5424", source.Source.SourceID)
	}
	config["protocol"] = protocol

	tlsConfig, explicitTLS, err := syslogTLSConfig(config)
	if err != nil {
		return nil, "", fmt.Errorf("source %s syslog TLS config: %w", source.Source.SourceID, err)
	}
	endpointTLSConfig := otelNestedMapConfig(endpointConfig, "tls")
	if len(endpointTLSConfig) > 0 {
		merged := cloneAnyMap(endpointTLSConfig)
		for key, value := range tlsConfig {
			merged[key] = value
		}
		tlsConfig = merged
	}
	if explicitTLS || len(endpointTLSConfig) > 0 {
		tlsRequested = true
	}
	if tlsRequested {
		if transport != "tcp" {
			return nil, "", fmt.Errorf("source %s syslog TLS requires tcp transport", source.Source.SourceID)
		}
		if err := validateSyslogTLSConfig(tlsConfig); err != nil {
			return nil, "", fmt.Errorf("source %s syslog TLS requires %s", source.Source.SourceID, err)
		}
		endpointConfig["tls"] = tlsConfig
	}

	identity := syslogSourceIdentity(source.Source, config)
	allowlist := syslogSourceAllowlist(source.Source, config)
	rateLimit, err := syslogSourceRateLimit(source.Source, config)
	if err != nil {
		return nil, "", fmt.Errorf("source %s syslog rate limit: %w", source.Source.SourceID, err)
	}
	applySyslogReceiverResourcePolicy(config, source.Source.SourceID, transport, protocol, tlsRequested, identity, allowlist, rateLimit)

	warnings := syslogReceiverWarnings(source.Source.SourceID, transport, tlsRequested, tlsConfig, identity, allowlist)
	receiverID := otelReceiverID(recipe.Receiver, source.Source.SourceID)
	policy, policyWarnings := syslogEdgeNetworkPolicyTemplate(source.Source.SourceID, receiverID, transport, fmt.Sprint(endpointConfig["listen_address"]), allowlist, rateLimit)
	warnings = append(warnings, policyWarnings...)
	applyOTelReceiverRetry(config)
	return []otelReceiverBuild{{
		ID:       receiverID,
		Type:     recipe.Receiver,
		Config:   config,
		Warnings: warnings,
		Policy:   policy,
	}}, "logs", nil
}

func defaultSyslogListenAddress(transport string, tlsEnabled bool) string {
	if strings.TrimSpace(transport) == "tcp" && tlsEnabled {
		return "0.0.0.0:6514"
	}
	return "0.0.0.0:5140"
}

func syslogHasTLSConfig(config map[string]any, transport string) bool {
	if len(config) == 0 {
		return false
	}
	switch typed := config["tls"].(type) {
	case bool:
		if typed {
			return true
		}
	case string:
		value := strings.TrimSpace(typed)
		if strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || value == "1" {
			return true
		}
	case map[string]any:
		if len(typed) > 0 {
			return true
		}
	case map[string]string:
		if len(typed) > 0 {
			return true
		}
	}
	for _, key := range []string{"cert_file", "key_file", "ca_file", "client_ca_file", "tls_cert_file", "tls_key_file", "tls_ca_file", "tls_client_ca_file"} {
		if strings.TrimSpace(stringConfig(config, key)) != "" {
			return true
		}
	}
	endpointConfig := otelNestedMapConfig(config, transport)
	return len(otelNestedMapConfig(endpointConfig, "tls")) > 0
}

func removeInactiveSyslogTransport(config map[string]any, transport string) {
	switch strings.TrimSpace(transport) {
	case "tcp":
		delete(config, "udp")
	case "udp":
		delete(config, "tcp")
	}
}

func syslogTLSConfig(config map[string]any) (map[string]any, bool, error) {
	if len(config) == 0 {
		return nil, false, nil
	}
	tlsConfig := map[string]any{}
	enabled := false
	if raw, ok := config["tls"]; ok {
		delete(config, "tls")
		switch typed := raw.(type) {
		case nil:
		case bool:
			enabled = typed
		case string:
			enabled = strings.EqualFold(strings.TrimSpace(typed), "true") || strings.EqualFold(strings.TrimSpace(typed), "yes") || strings.TrimSpace(typed) == "1"
		case map[string]any:
			for key, value := range typed {
				if trimmed := strings.TrimSpace(key); trimmed != "" && value != nil {
					tlsConfig[trimmed] = value
				}
			}
			enabled = len(tlsConfig) > 0
		case map[string]string:
			for key, value := range typed {
				if trimmedKey, trimmedValue := strings.TrimSpace(key), strings.TrimSpace(value); trimmedKey != "" && trimmedValue != "" {
					tlsConfig[trimmedKey] = trimmedValue
				}
			}
			enabled = len(tlsConfig) > 0
		default:
			return nil, false, fmt.Errorf("tls must be a bool or map")
		}
	}
	for _, item := range []struct {
		configKey string
		tlsKey    string
	}{
		{"cert_file", "cert_file"},
		{"key_file", "key_file"},
		{"ca_file", "ca_file"},
		{"client_ca_file", "client_ca_file"},
		{"tls_cert_file", "cert_file"},
		{"tls_key_file", "key_file"},
		{"tls_ca_file", "ca_file"},
		{"tls_client_ca_file", "client_ca_file"},
	} {
		if value := strings.TrimSpace(stringConfig(config, item.configKey)); value != "" {
			tlsConfig[item.tlsKey] = value
			enabled = true
		}
		delete(config, item.configKey)
	}
	if enabled && (otelString(tlsConfig["cert_file"]) == "" || otelString(tlsConfig["key_file"]) == "") {
		return nil, true, fmt.Errorf("cert_file and key_file are required")
	}
	if !enabled {
		return nil, false, nil
	}
	return tlsConfig, true, nil
}

func validateSyslogTLSConfig(config map[string]any) error {
	if len(config) == 0 {
		return fmt.Errorf("cert_file and key_file")
	}
	if otelString(config["cert_file"]) == "" || otelString(config["key_file"]) == "" {
		return fmt.Errorf("cert_file and key_file")
	}
	return nil
}

func syslogSourceIdentity(source SourceProfile, config map[string]any) string {
	keys := []string{
		"source_identity",
		"device_identity",
		"device_id",
		"expected_device_id",
		"expected_hostname",
		"expected_host",
		"hostname",
		"host",
	}
	for _, key := range keys {
		if value := strings.TrimSpace(stringConfig(config, key)); value != "" {
			removeConfigKeys(config, keys...)
			return value
		}
	}
	removeConfigKeys(config, keys...)
	return metadataString(source.Metadata, keys...)
}

func syslogSourceAllowlist(source SourceProfile, config map[string]any) []string {
	keys := []string{
		"source_allowlist_cidrs",
		"allowlist_cidrs",
		"allowed_cidrs",
		"source_allowlist",
		"allowed_source_ips",
		"expected_source_ips",
		"allowed_peers",
		"peer_allowlist",
	}
	var values []string
	for _, key := range keys {
		values = append(values, stringSliceConfig(config, key)...)
	}
	removeConfigKeys(config, keys...)
	values = append(values, metadataStringSlice(source.Metadata, keys...)...)
	return dedupeSorted(values)
}

type syslogRateLimit struct {
	PerSecond int
	Burst     int
}

func syslogSourceRateLimit(source SourceProfile, config map[string]any) (syslogRateLimit, error) {
	rateKeys := []string{
		"source_rate_limit_per_second",
		"rate_limit_per_second",
		"syslog_rate_limit_per_second",
		"max_events_per_second",
		"max_packets_per_second",
		"max_connections_per_second",
	}
	burstKeys := []string{
		"source_rate_limit_burst",
		"rate_limit_burst",
		"syslog_rate_limit_burst",
		"max_burst",
		"burst",
	}
	rate, err := firstPositiveIntConfig(config, source.Metadata, rateKeys...)
	if err != nil {
		removeConfigKeys(config, append(rateKeys, burstKeys...)...)
		return syslogRateLimit{}, err
	}
	burst, err := firstPositiveIntConfig(config, source.Metadata, burstKeys...)
	if err != nil {
		removeConfigKeys(config, append(rateKeys, burstKeys...)...)
		return syslogRateLimit{}, err
	}
	removeConfigKeys(config, append(rateKeys, burstKeys...)...)
	if rate <= 0 {
		return syslogRateLimit{}, nil
	}
	if burst <= 0 {
		burst = rate * 2
	}
	return syslogRateLimit{PerSecond: rate, Burst: burst}, nil
}

func firstPositiveIntConfig(config map[string]any, metadata map[string]string, keys ...string) (int, error) {
	for _, key := range keys {
		if value, ok := config[key]; ok && value != nil {
			parsed, err := positiveIntValue(value)
			if err != nil {
				return 0, fmt.Errorf("%s must be a positive integer", key)
			}
			return parsed, nil
		}
	}
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return 0, fmt.Errorf("%s must be a positive integer", key)
			}
			return parsed, nil
		}
	}
	return 0, nil
}

func positiveIntValue(value any) (int, error) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return 0, fmt.Errorf("empty integer")
	}
	parsed, err := strconv.Atoi(text)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid positive integer")
	}
	return parsed, nil
}

func applySyslogReceiverResourcePolicy(config map[string]any, sourceID, transport, protocol string, tlsEnabled bool, identity string, allowlist []string, rateLimit syslogRateLimit) {
	resource := otelNestedMapConfig(config, "resource")
	resource["control_one.syslog.source_id"] = strings.TrimSpace(sourceID)
	resource["control_one.syslog.transport"] = strings.TrimSpace(transport)
	resource["control_one.syslog.protocol"] = strings.TrimSpace(protocol)
	if tlsEnabled {
		resource["control_one.syslog.tls"] = "true"
	} else {
		resource["control_one.syslog.tls"] = "false"
	}
	if strings.TrimSpace(identity) != "" {
		resource["control_one.syslog.source_identity"] = strings.TrimSpace(identity)
	}
	if len(allowlist) > 0 {
		resource["control_one.syslog.allowlist_cidrs"] = strings.Join(allowlist, ",")
	}
	if rateLimit.PerSecond > 0 {
		resource["control_one.syslog.rate_limit_per_second"] = strconv.Itoa(rateLimit.PerSecond)
		resource["control_one.syslog.rate_limit_burst"] = strconv.Itoa(rateLimit.Burst)
	}
	config["resource"] = resource
}

func syslogEdgeNetworkPolicyTemplate(sourceID, receiverID, transport, listenAddress string, allowlist []string, rateLimit syslogRateLimit) (*OTelEdgeNetworkPolicyTemplate, []string) {
	if len(allowlist) == 0 && rateLimit.PerSecond <= 0 {
		return nil, nil
	}
	policy := &OTelEdgeNetworkPolicyTemplate{
		Kind:               "nftables",
		SourceID:           strings.TrimSpace(sourceID),
		ReceiverID:         strings.TrimSpace(receiverID),
		ListenAddress:      strings.TrimSpace(listenAddress),
		Transport:          strings.TrimSpace(transport),
		AllowlistCIDRs:     append([]string(nil), allowlist...),
		RateLimitPerSecond: rateLimit.PerSecond,
		RateLimitBurst:     rateLimit.Burst,
	}
	port, warnings := syslogListenPort(listenAddress)
	if port == "" {
		return policy, append(warnings, fmt.Sprintf("source %s syslog edge policy has no parseable listen port", strings.TrimSpace(sourceID)))
	}
	policy.NftablesRules = syslogNftablesRules(transport, port, allowlist, rateLimit)
	return policy, warnings
}

func syslogListenPort(listenAddress string) (string, []string) {
	listenAddress = strings.TrimSpace(listenAddress)
	if listenAddress == "" {
		return "", nil
	}
	_, port, err := net.SplitHostPort(listenAddress)
	if err == nil && strings.TrimSpace(port) != "" {
		return strings.TrimSpace(port), nil
	}
	index := strings.LastIndex(listenAddress, ":")
	if index >= 0 && index+1 < len(listenAddress) {
		port = strings.TrimSpace(listenAddress[index+1:])
		if _, err := strconv.Atoi(port); err == nil {
			return port, nil
		}
	}
	return "", []string{fmt.Sprintf("syslog listen_address %q is not host:port; edge network policy needs a concrete port", listenAddress)}
}

func syslogNftablesRules(transport, port string, allowlist []string, rateLimit syslogRateLimit) []string {
	transport = strings.TrimSpace(transport)
	if transport == "" {
		transport = "tcp"
	}
	base := transport + " dport " + strings.TrimSpace(port)
	rules := []string(nil)
	ipv4, ipv6 := splitSyslogAllowlistFamilies(allowlist)
	if len(allowlist) > 0 {
		rules = append(rules, syslogNftablesAllowlistRules(base, "ip saddr", ipv4, rateLimit)...)
		rules = append(rules, syslogNftablesAllowlistRules(base, "ip6 saddr", ipv6, rateLimit)...)
		rules = append(rules, base+" drop")
		return rules
	}
	if rateLimit.PerSecond > 0 {
		rules = append(rules, syslogNftablesRateLimitRule(base, "", nil, rateLimit))
		rules = append(rules, base+" accept")
	}
	return rules
}

func splitSyslogAllowlistFamilies(allowlist []string) ([]string, []string) {
	var ipv4 []string
	var ipv6 []string
	for _, value := range allowlist {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, ":") {
			ipv6 = append(ipv6, value)
			continue
		}
		ipv4 = append(ipv4, value)
	}
	return ipv4, ipv6
}

func syslogNftablesAllowlistRules(base, selector string, values []string, rateLimit syslogRateLimit) []string {
	if len(values) == 0 {
		return nil
	}
	rules := []string(nil)
	if rateLimit.PerSecond > 0 {
		rules = append(rules, syslogNftablesRateLimitRule(base, selector, values, rateLimit))
	}
	rules = append(rules, strings.TrimSpace(base+" "+selector+" { "+strings.Join(values, ", ")+" } accept"))
	return rules
}

func syslogNftablesRateLimitRule(base, selector string, values []string, rateLimit syslogRateLimit) string {
	match := strings.TrimSpace(base)
	if selector != "" && len(values) > 0 {
		match = strings.TrimSpace(match + " " + selector + " { " + strings.Join(values, ", ") + " }")
	}
	return fmt.Sprintf("%s limit rate over %d/second burst %d packets drop", match, rateLimit.PerSecond, rateLimit.Burst)
}

func syslogReceiverWarnings(sourceID, transport string, tlsEnabled bool, tlsConfig map[string]any, identity string, allowlist []string) []string {
	var warnings []string
	sourceID = strings.TrimSpace(sourceID)
	if transport == "udp" {
		warnings = append(warnings, fmt.Sprintf("source %s syslog uses UDP; deploy behind network allowlists and prefer TCP/TLS for critical bank devices", sourceID))
	}
	if !tlsEnabled {
		warnings = append(warnings, fmt.Sprintf("source %s syslog has no TLS config; production edge collectors should use TLS/mTLS or enforced network controls", sourceID))
	} else if otelString(tlsConfig["client_ca_file"]) == "" {
		warnings = append(warnings, fmt.Sprintf("source %s syslog TLS has no client_ca_file; client identity is not mTLS-verified by the receiver", sourceID))
	}
	if strings.TrimSpace(identity) == "" && len(allowlist) == 0 {
		warnings = append(warnings, fmt.Sprintf("source %s syslog has no source_identity or source allowlist metadata", sourceID))
	}
	return warnings
}

func buildOTelWindowsEventReceivers(source ResolvedSource, recipe CollectorRecipe, storageExtensionID string) ([]otelReceiverBuild, string, error) {
	config := cloneOTelConfig(recipe.Config)
	channels := stringSliceConfig(config, "channels")
	if len(channels) == 0 {
		channels = stringSliceConfig(config, "channel")
	}
	if len(channels) == 0 {
		channels = metadataStringSlice(source.Source.Metadata, "channels", "channel", "event_channels", "event_channel")
	}
	if len(channels) == 0 && strings.TrimSpace(recipe.Mode) == CollectorWEF {
		channels = []string{"ForwardedEvents"}
	}
	channels = dedupeSorted(channels)
	if len(channels) == 0 {
		return nil, "", fmt.Errorf("source %s windows_eventlog recipe requires at least one channel", source.Source.SourceID)
	}
	delete(config, "channels")
	delete(config, "event_channels")
	out := make([]otelReceiverBuild, 0, len(channels))
	for _, channel := range channels {
		receiverConfig := cloneOTelConfig(config)
		receiverConfig["channel"] = channel
		if _, ok := receiverConfig["start_at"]; !ok {
			receiverConfig["start_at"] = "end"
		}
		applyOTelReceiverStorage(receiverConfig, storageExtensionID)
		applyOTelReceiverRetry(receiverConfig)
		receiverID := otelReceiverID(recipe.Receiver, source.Source.SourceID)
		if len(channels) > 1 {
			receiverID += "." + otelComponentName(channel)
		}
		out = append(out, otelReceiverBuild{
			ID:     receiverID,
			Type:   recipe.Receiver,
			Config: receiverConfig,
		})
	}
	return out, "logs", nil
}

func buildOTelOTLPReceiver(source ResolvedSource, recipe CollectorRecipe) ([]otelReceiverBuild, string, error) {
	config := cloneOTelConfig(recipe.Config)
	if _, ok := config["protocols"]; !ok {
		config["protocols"] = map[string]any{
			"grpc": map[string]any{"endpoint": "0.0.0.0:4317"},
			"http": map[string]any{"endpoint": "0.0.0.0:4318"},
		}
	}
	return []otelReceiverBuild{{
		ID:     otelReceiverID(recipe.Receiver, source.Source.SourceID),
		Type:   recipe.Receiver,
		Config: config,
	}}, "logs", nil
}

func buildExplicitOTelReceiver(source ResolvedSource, recipe CollectorRecipe) ([]otelReceiverBuild, string, error) {
	config := cloneOTelConfig(recipe.Config)
	if len(config) == 0 {
		return nil, "", fmt.Errorf("source %s %s recipe requires explicit receiver config", source.Source.SourceID, recipe.Mode)
	}
	pipelineType := "logs"
	if recipe.Mode == CollectorPrometheus {
		pipelineType = "metrics"
	}
	return []otelReceiverBuild{{
		ID:     otelReceiverID(recipe.Receiver, source.Source.SourceID),
		Type:   recipe.Receiver,
		Config: config,
	}}, pipelineType, nil
}

func cloneOTelConfig(config map[string]any) map[string]any {
	out := cloneAnyMap(config)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func otelNestedMapConfig(config map[string]any, key string) map[string]any {
	if len(config) == 0 {
		return map[string]any{}
	}
	switch typed := config[key].(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for itemKey, value := range typed {
			if trimmedKey := strings.TrimSpace(itemKey); trimmedKey != "" {
				out[trimmedKey] = value
			}
		}
		return out
	default:
		return map[string]any{}
	}
}

func otelBoolDefault(value any, defaultValue bool) any {
	if value == nil {
		return defaultValue
	}
	return value
}

func otelString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func removeConfigKeys(config map[string]any, keys ...string) {
	for _, key := range keys {
		delete(config, key)
	}
}

func ensureOTelPersistentStorage(config *OTelCollectorConfig, opts OTelCollectorConfigOptions) string {
	if config == nil || opts.DisablePersistentStorage {
		return ""
	}
	extensionID := strings.TrimSpace(opts.StorageExtensionID)
	if extensionID == "" {
		extensionID = defaultOTelFileStorageExtensionID
	}
	config.Extensions[extensionID] = otelFileStorageConfig(opts)
	if !stringSliceContains(config.Service.Extensions, extensionID) {
		config.Service.Extensions = append(config.Service.Extensions, extensionID)
		sort.Strings(config.Service.Extensions)
	}
	return extensionID
}

func otelFileStorageConfig(opts OTelCollectorConfigOptions) map[string]any {
	config := map[string]any{
		"create_directory": true,
	}
	if dir := strings.TrimSpace(opts.StorageDirectory); dir != "" {
		config["directory"] = dir
	}
	return config
}

func applyOTelReceiverStorage(config map[string]any, storageExtensionID string) {
	if len(config) == 0 || strings.TrimSpace(storageExtensionID) == "" {
		return
	}
	if _, ok := config["storage"]; !ok {
		config["storage"] = storageExtensionID
	}
}

func applyOTelReceiverRetry(config map[string]any) {
	if len(config) == 0 {
		return
	}
	if _, ok := config["retry_on_failure"]; !ok {
		config["retry_on_failure"] = map[string]any{
			"enabled":          true,
			"initial_interval": "1s",
			"max_interval":     "30s",
			"max_elapsed_time": "0s",
		}
	}
}

func otelExporterConfig(opts OTelCollectorConfigOptions) map[string]any {
	config := map[string]any{
		"endpoint": strings.TrimSpace(opts.Endpoint),
	}
	if len(opts.Headers) > 0 {
		headers := map[string]any{}
		for _, key := range sortedStringMapKeys(opts.Headers) {
			headerKey := strings.TrimSpace(key)
			value := strings.TrimSpace(opts.Headers[key])
			if headerKey != "" && value != "" {
				headers[headerKey] = value
			}
		}
		if len(headers) > 0 {
			config["headers"] = headers
		}
	}
	if opts.InsecureTLS {
		config["tls"] = map[string]any{"insecure": true}
	}
	if compression := strings.TrimSpace(opts.Compression); compression != "" {
		config["compression"] = compression
	}
	if timeout := strings.TrimSpace(opts.Timeout); timeout != "" {
		config["timeout"] = timeout
	}
	if _, ok := config["retry_on_failure"]; !ok {
		maxElapsed := strings.TrimSpace(opts.ExporterRetryMaxElapsedTime)
		if maxElapsed == "" {
			maxElapsed = "0s"
		}
		config["retry_on_failure"] = map[string]any{
			"enabled":          true,
			"initial_interval": "1s",
			"max_interval":     "30s",
			"max_elapsed_time": maxElapsed,
		}
	}
	if _, ok := config["sending_queue"]; !ok {
		queueSize := opts.ExporterQueueSize
		if queueSize <= 0 {
			queueSize = 10000
		}
		consumers := opts.ExporterQueueConsumers
		if consumers <= 0 {
			consumers = 4
		}
		queue := map[string]any{
			"enabled":       true,
			"num_consumers": consumers,
			"queue_size":    queueSize,
		}
		if storageID := strings.TrimSpace(opts.StorageExtensionID); storageID != "" && !opts.DisablePersistentStorage {
			queue["storage"] = storageID
		} else if !opts.DisablePersistentStorage {
			queue["storage"] = defaultOTelFileStorageExtensionID
		}
		config["sending_queue"] = queue
	}
	return config
}

func otelMemoryLimiterConfig(opts OTelCollectorConfigOptions) map[string]any {
	limit := opts.MemoryLimitMiB
	if limit <= 0 {
		limit = 512
	}
	spike := opts.MemorySpikeLimitMiB
	if spike <= 0 {
		spike = 128
	}
	return map[string]any{
		"check_interval":  "1s",
		"limit_mib":       limit,
		"spike_limit_mib": spike,
	}
}

func otelBatchConfig(opts OTelCollectorConfigOptions) map[string]any {
	timeout := strings.TrimSpace(opts.BatchTimeout)
	if timeout == "" {
		timeout = "1s"
	}
	size := opts.BatchSendBatchSize
	if size <= 0 {
		size = 8192
	}
	return map[string]any{
		"send_batch_size": size,
		"timeout":         timeout,
	}
}

func addCommonOTelResourceProcessor(processors map[string]any, opts OTelCollectorConfigOptions) string {
	attrs := map[string]string{}
	if tenantID := strings.TrimSpace(opts.TenantID); tenantID != "" {
		attrs["c1.tenant_id"] = tenantID
	}
	if collectorID := strings.TrimSpace(opts.CollectorID); collectorID != "" {
		attrs["c1.collector_id"] = collectorID
	}
	for key, value := range opts.AdditionalAttributes {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			attrs[key] = value
		}
	}
	if len(attrs) == 0 {
		return ""
	}
	processors[defaultOTelCommonResourceProcessor] = map[string]any{
		"attributes": otelResourceAttributes(attrs),
	}
	return defaultOTelCommonResourceProcessor
}

func addOTelRawRedactionProcessor(processors map[string]any, sourceID, collectMode string) (string, []string, error) {
	if normalizeOTelCollectModeNoError(collectMode) != OTelCollectModeCollectParsed {
		return "", nil, nil
	}
	processorID := "transform/controlone.source." + otelComponentName(sourceID) + ".redact_raw"
	if _, exists := processors[processorID]; exists {
		return "", nil, fmt.Errorf("duplicate OTel raw redaction processor id %s", processorID)
	}
	processors[processorID] = map[string]any{
		"error_mode": "ignore",
		"log_statements": []map[string]any{{
			"context": "log",
			"statements": []string{
				`set(attributes["control_one.collect_mode"], "collect_parsed")`,
				`set(attributes["control_one.raw_message_retained"], "false")`,
				fmt.Sprintf("set(body, %q)", defaultOTelRedactedRawLogBody),
			},
		}},
	}
	return processorID, []string{
		fmt.Sprintf("source %s collect_parsed uses the OTel transform processor to overwrite log body before export; ensure the receiver recipe extracts required fields first", sourceID),
	}, nil
}

func sourceOTelAttributes(source ResolvedSource, mode, collectMode, approvalRef string) map[string]string {
	attrs := map[string]string{
		"c1.source_id":      strings.TrimSpace(source.Source.SourceID),
		"c1.collector_mode": strings.TrimSpace(mode),
	}
	if normalized := normalizeOTelCollectModeNoError(collectMode); normalized != "" {
		attrs["c1.collect_mode"] = normalized
		if normalized == OTelCollectModeCollectParsed {
			attrs["c1.raw_message_retained"] = "false"
		}
	}
	if packID := strings.TrimSpace(source.PackID); packID != "" {
		attrs["c1.pack_id"] = packID
	}
	if packVersion := strings.TrimSpace(source.PackVersion); packVersion != "" {
		attrs["c1.pack_version"] = packVersion
	}
	if len(source.Parsers) > 0 {
		parserIDs := make([]string, 0, len(source.Parsers))
		for _, parser := range source.Parsers {
			if parserID := strings.TrimSpace(parser.ParserID); parserID != "" {
				parserIDs = append(parserIDs, parserID)
			}
		}
		sort.Strings(parserIDs)
		if len(parserIDs) > 0 {
			attrs["c1.parser_ids"] = strings.Join(parserIDs, ",")
		}
	}
	if approvalRef != "" {
		attrs["c1.approval_ref"] = approvalRef
	}
	return attrs
}

func normalizeOTelCollectMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", OTelCollectModeCollectRaw:
		return OTelCollectModeCollectRaw, nil
	case OTelCollectModeCollectParsed:
		return OTelCollectModeCollectParsed, nil
	default:
		return "", fmt.Errorf("unsupported OTel collect_mode %q", value)
	}
}

func normalizeOTelCollectModeNoError(value string) string {
	normalized, err := normalizeOTelCollectMode(value)
	if err != nil {
		return ""
	}
	return normalized
}

func otelResourceAttributes(values map[string]string) []map[string]any {
	keys := sortedStringMapKeys(values)
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		attrKey := strings.TrimSpace(key)
		value := strings.TrimSpace(values[key])
		if attrKey == "" || value == "" {
			continue
		}
		out = append(out, map[string]any{
			"action": "upsert",
			"key":    attrKey,
			"value":  value,
		})
	}
	return out
}

func sourceHasCollectorMode(source SourceProfile, mode string) bool {
	mode = strings.TrimSpace(mode)
	for _, candidate := range source.CollectorModes {
		if strings.TrimSpace(candidate) == mode {
			return true
		}
	}
	return false
}

func isOTelRenderableMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case CollectorOTelFileLog, CollectorSyslog, CollectorWindowsEvent, CollectorWEF, CollectorSplunkHEC, CollectorKafka, CollectorOTLP, CollectorPrometheus:
		return true
	default:
		return false
	}
}

func defaultOTelReceiverForMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case CollectorOTelFileLog:
		return "filelog"
	case CollectorSyslog:
		return "syslog"
	case CollectorWindowsEvent:
		return "windows_event_log"
	case CollectorWEF:
		return "windows_event_log"
	case CollectorSplunkHEC:
		return "splunk_hec"
	case CollectorKafka:
		return "kafka"
	case CollectorOTLP:
		return "otlp"
	case CollectorPrometheus:
		return "prometheus"
	default:
		return ""
	}
}

func otelReceiverID(receiver, sourceID string) string {
	return strings.TrimSpace(receiver) + "/controlone." + otelComponentName(sourceID)
}

func otelComponentName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
		if valid {
			b.WriteRune(r)
			lastUnderscore = r == '_'
			continue
		}
		if unicode.IsSpace(r) || r == '/' || r == '\\' || r == ':' {
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "source"
	}
	return out
}

func metadataString(metadata map[string]string, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func metadataStringSlice(metadata map[string]string, keys ...string) []string {
	if len(metadata) == 0 {
		return nil
	}
	var out []string
	for _, key := range keys {
		value := strings.TrimSpace(metadata[key])
		if value == "" {
			continue
		}
		out = append(out, splitDelimitedStrings(value)...)
	}
	return out
}

func splitDelimitedStrings(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func compareOTelSourceRequest(a, b OTelCollectorConfigSource) int {
	aKey := []string{a.Source.Source.SourceID, a.Source.PackID, a.Source.PackVersion, a.Mode, normalizeOTelCollectModeNoError(a.CollectMode)}
	bKey := []string{b.Source.Source.SourceID, b.Source.PackID, b.Source.PackVersion, b.Mode, normalizeOTelCollectModeNoError(b.CollectMode)}
	for i := range aKey {
		if aKey[i] < bKey[i] {
			return -1
		}
		if aKey[i] > bKey[i] {
			return 1
		}
	}
	return 0
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.TrimSpace(keys[i]) < strings.TrimSpace(keys[j])
	})
	return keys
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
