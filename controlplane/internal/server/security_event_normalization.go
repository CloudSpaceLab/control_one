package server

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	sshdFailurePattern  = regexp.MustCompile(`(?i)failed (?:password|publickey|keyboard-interactive)(?: for (?:invalid user )?)(\S+) from ([0-9a-f:.]+) port (\d+)`)
	sshdSuccessPattern  = regexp.MustCompile(`(?i)accepted (?:password|publickey|keyboard-interactive) for (\S+) from ([0-9a-f:.]+) port (\d+)`)
	commonWebPattern    = regexp.MustCompile(`^(\S+)\s+\S+\s+\S+\s+\[[^]]+\]\s+"([A-Z]+)\s+(\S+)(?:\s+HTTP/[^\"]+)?"\s+(\d{3})\s+(\d+|-)`)
	databaseUserPattern = regexp.MustCompile(`(?i)(?:user|login|role|username)[= :'\"]+([A-Za-z0-9_.@\\-]+)`)
)

// normalizeSecurityEvents converts collector-specific events into the stable
// security.event contract consumed by correlation rules. Original events stay
// in the fan-out so existing analytics and raw evidence remain available.
func normalizeSecurityEvents(tenantID, nodeID uuid.UUID, events []IngestedEvent) []IngestedEvent {
	out := make([]IngestedEvent, 0, len(events))
	for i := range events {
		normalized := normalizeSecurityEvent(tenantID, nodeID, &events[i])
		for j := range normalized {
			if err := validateNormalizedSecurityEvent(&normalized[j]); err != nil {
				out = append(out, normalizationErrorEvent(tenantID, nodeID, &events[i], err))
				break
			}
			out = append(out, normalized[j])
		}
	}
	return out
}

func normalizeSecurityEvent(tenantID, nodeID uuid.UUID, ev *IngestedEvent) []IngestedEvent {
	if ev == nil || ev.Type == "security.event" {
		return nil
	}
	switch {
	case ev.Type == "web.request" || ev.Type == "web.error":
		extra := structuredWebFields(ev)
		return []IngestedEvent{newNormalizedSecurityEvent(tenantID, nodeID, ev, "web.request", "web", "request", outcomeForHTTPStatus(anyInt(extra["status_code"])), extra)}
	case strings.HasPrefix(ev.Type, "conn."):
		return []IngestedEvent{newNormalizedSecurityEvent(tenantID, nodeID, ev, "network.connection", "network", "connection", "unknown", nil)}
	case ev.Type == "db.query":
		if databaseAuthenticationFailed(ev) {
			return authenticationEvents(tenantID, nodeID, ev, "database.authentication_failure", detailStringAny(ev, "user_name", "username", "database_user"), "database")
		}
		return nil
	case ev.Type != "log.line":
		return nil
	}

	program := strings.ToLower(detailStringAny(ev, "program", "provider", "event.provider"))
	source := strings.ToLower(detailStringAny(ev, "source", "collector_type", "event.dataset"))
	message := strings.TrimSpace(ev.Message)
	if strings.Contains(program, "sshd") || strings.Contains(source, "sshd") || strings.Contains(source, "auth.log") || strings.Contains(source, "secure") {
		if match := sshdFailurePattern.FindStringSubmatch(message); len(match) == 4 {
			port, _ := strconv.Atoi(match[3])
			extra := map[string]any{"src_ip": match[2], "src_port": port, "dst_port": 22, "protocol": "tcp", "user_name": match[1]}
			return authenticationEvents(tenantID, nodeID, ev, "ssh.authentication_failure", match[1], "sshd", extra)
		}
		if match := sshdSuccessPattern.FindStringSubmatch(message); len(match) == 4 {
			port, _ := strconv.Atoi(match[3])
			extra := map[string]any{"src_ip": match[2], "src_port": port, "dst_port": 22, "protocol": "tcp", "user_name": match[1]}
			return authenticationEvents(tenantID, nodeID, ev, "authentication.success", match[1], "sshd", extra)
		}
		if containsAnyText(strings.ToLower(message), "failed password", "failed publickey", "accepted password", "accepted publickey", "authentication failure") {
			return []IngestedEvent{normalizationErrorEvent(tenantID, nodeID, ev, errors.New("unrecognized ssh authentication record"))}
		}
	}

	eventCode := detailStringAny(ev, "event_code", "event.code", "EventID", "event_id", "id")
	if eventCode == "4625" {
		return authenticationEvents(tenantID, nodeID, ev, "windows.authentication_failure", windowsUser(ev), "windows_security", windowsAuthenticationFields(ev))
	}
	if eventCode == "4624" {
		return authenticationEvents(tenantID, nodeID, ev, "authentication.success", windowsUser(ev), "windows_security", windowsAuthenticationFields(ev))
	}

	if isWebSource(program, source) {
		if extra, ok := webFieldsFromEvent(ev); ok {
			if _, hasPort := extra["dst_port"]; !hasPort && detailInt(ev.Details, "dst_port") == 0 {
				extra["dst_port"] = inferredWebPort(source, ev)
			}
			return []IngestedEvent{newNormalizedSecurityEvent(tenantID, nodeID, ev, "web.request", "web", "request", outcomeForHTTPStatus(anyInt(extra["status_code"])), extra)}
		}
		return []IngestedEvent{normalizationErrorEvent(tenantID, nodeID, ev, errors.New("web record is missing a valid client IP, method, path, or status"))}
	}

	if isDatabaseSource(program, source) && databaseAuthenticationFailed(ev) {
		user := detailStringAny(ev, "user_name", "username", "database_user")
		if user == "" {
			if match := databaseUserPattern.FindStringSubmatch(message); len(match) == 2 {
				user = strings.Trim(match[1], "'\"")
			}
		}
		return authenticationEvents(tenantID, nodeID, ev, "database.authentication_failure", user, firstNonEmpty(program, "database"))
	}

	if strings.Contains(program, "finacle") || strings.Contains(source, "finacle") {
		lower := strings.ToLower(message)
		if containsAnyText(lower, "authentication failed", "login failed", "invalid credentials") {
			return authenticationEvents(tenantID, nodeID, ev, "database.authentication_failure", detailStringAny(ev, "user_name", "username", "finacle_uid"), "finacle")
		}
		if containsAnyText(lower, "failed", "unavailable", "timeout", "error") {
			return []IngestedEvent{newNormalizedSecurityEvent(tenantID, nodeID, ev, "finacle.operation_failure", "banking", "operation", "failure", nil)}
		}
	}
	return nil
}

func authenticationEvents(tenantID, nodeID uuid.UUID, ev *IngestedEvent, specificType, user, provider string, extras ...map[string]any) []IngestedEvent {
	extra := map[string]any{"auth_result": "failure"}
	if strings.HasSuffix(specificType, ".success") {
		extra["auth_result"] = "success"
	}
	if user != "" {
		extra["user_name"] = user
	}
	for _, values := range extras {
		for key, value := range values {
			extra[key] = value
		}
	}
	primary := newNormalizedSecurityEvent(tenantID, nodeID, ev, specificType, "authentication", "login", fmt.Sprint(extra["auth_result"]), extra)
	primary.Details["event_provider"] = provider
	if specificType == "authentication.failure" || specificType == "authentication.success" {
		return []IngestedEvent{primary}
	}
	genericType := "authentication.failure"
	if fmt.Sprint(extra["auth_result"]) == "success" {
		genericType = "authentication.success"
	}
	generic := newNormalizedSecurityEvent(tenantID, nodeID, ev, genericType, "authentication", "login", fmt.Sprint(extra["auth_result"]), extra)
	generic.Details["event_provider"] = provider
	return []IngestedEvent{primary, generic}
}

func newNormalizedSecurityEvent(tenantID, nodeID uuid.UUID, source *IngestedEvent, eventType, category, action, outcome string, extra map[string]any) IngestedEvent {
	normalizedSource := normalizedEventSource(source)
	details := map[string]any{
		"event_type": eventType, "event_category": category, "event_action": action, "outcome": outcome,
		"original_event_type": source.Type, "source_event_id": source.EventID, "source": normalizedSource,
		"node_id": nodeID.String(), "timestamp": source.TS.UTC().Format(time.RFC3339Nano),
	}
	copyEventField(details, "src_ip", source.SrcIP)
	copyEventField(details, "src_port", source.SrcPort)
	copyEventField(details, "dst_ip", source.DstIP)
	copyEventField(details, "dst_port", source.DstPort)
	copyEventField(details, "user_name", source.UserName)
	copyEventField(details, "protocol", strings.ToLower(source.Protocol))
	copyEventField(details, "bytes_in", source.BytesIn)
	copyEventField(details, "bytes_out", source.BytesOut)
	for _, key := range []string{"src_ip", "src_port", "dst_ip", "dst_port", "user_name", "protocol", "direction", "bytes_in", "bytes_out", "status_code", "http_method", "path", "hostname"} {
		if value, ok := eventDetailValue(source, key); ok {
			details[key] = value
		}
	}
	copyEventAlias(details, source, "src_ip", "source_ip", "client_ip", "remote_ip")
	copyEventAlias(details, source, "dst_ip", "destination_ip", "server_ip")
	copyEventAlias(details, source, "src_port", "source_port")
	copyEventAlias(details, source, "dst_port", "destination_port", "server_port")
	copyEventAlias(details, source, "user_name", "username", "user")
	copyEventAlias(details, source, "auth_result", "auth_result", "outcome", "result")
	for key, value := range extra {
		if value != nil && fmt.Sprint(value) != "" {
			details[key] = value
		}
	}
	normalizeCorrelationFieldTypes(details)
	if _, ok := details["user_name"]; !ok {
		if value, ok := details["username"]; ok {
			details["user_name"] = value
		}
	}
	details["normalized"] = canonicalSecurityFields(details, category, action, outcome, source)
	severity := source.Severity
	if severity == "" {
		severity = "medium"
	}
	return IngestedEvent{
		Type: "security.event", TS: source.TS, TenantID: tenantID.String(), NodeID: nodeID.String(),
		Severity: severity, CorrelationID: source.CorrelationID, Message: source.Message, Details: details,
		DedupKey: strings.TrimSpace(source.DedupKey) + ":normalized:" + eventType,
	}
}

func normalizeCorrelationFieldTypes(details map[string]any) {
	for _, key := range []string{"src_port", "dst_port", "status_code"} {
		if value, ok := details[key]; ok {
			details[key] = anyInt(value)
		}
	}
	for _, key := range []string{"bytes_in", "bytes_out"} {
		if value, ok := details[key]; ok {
			details[key] = anyInt64(value)
		}
	}
	for _, key := range []string{"protocol", "auth_result", "outcome"} {
		if value, ok := details[key]; ok {
			details[key] = strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
		}
	}
}

func validateNormalizedSecurityEvent(ev *IngestedEvent) error {
	if ev == nil || ev.Type != "security.event" {
		return errors.New("normalized event must use security.event envelope")
	}
	details := ev.Details
	for _, field := range []string{"event_type", "source", "node_id", "timestamp", "outcome"} {
		if strings.TrimSpace(fmt.Sprint(details[field])) == "" {
			return fmt.Errorf("required normalized field %s is missing", field)
		}
	}
	if ev.TS.IsZero() {
		return errors.New("required normalized field timestamp is invalid")
	}
	eventType := fmt.Sprint(details["event_type"])
	required := map[string][]string{
		"ssh.authentication_failure":      {"src_ip", "dst_port", "protocol", "user_name", "auth_result"},
		"windows.authentication_failure":  {"user_name", "auth_result"},
		"database.authentication_failure": {"user_name", "auth_result"},
		"web.request":                     {"src_ip", "dst_port", "protocol", "http_method", "path", "status_code"},
		"network.connection":              {"src_ip", "dst_ip", "dst_port", "protocol"},
	}
	for _, field := range required[eventType] {
		value, ok := details[field]
		if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" || fmt.Sprint(value) == "0" {
			return fmt.Errorf("%s requires %s", eventType, field)
		}
	}
	for _, field := range []string{"src_ip", "dst_ip"} {
		if value, ok := details[field]; ok && net.ParseIP(strings.TrimSpace(fmt.Sprint(value))) == nil {
			return fmt.Errorf("%s must be a valid IP address", field)
		}
	}
	return nil
}

func normalizationErrorEvent(tenantID, nodeID uuid.UUID, source *IngestedEvent, cause error) IngestedEvent {
	sourceName := normalizedEventSource(source)
	details := map[string]any{"event_type": "security.normalization_error", "source": sourceName, "node_id": nodeID.String(), "timestamp": source.TS.UTC().Format(time.RFC3339Nano), "outcome": "failure", "error": cause.Error(), "original_event_type": source.Type, "source_event_id": source.EventID}
	return IngestedEvent{Type: "security.event", TS: source.TS, TenantID: tenantID.String(), NodeID: nodeID.String(), Severity: "warning", Parser: source.Parser, ParserStatus: "error", Message: "Security event normalization failed: " + cause.Error(), Details: details, DedupKey: strings.TrimSpace(source.DedupKey) + ":normalization-error"}
}

func normalizedEventSource(ev *IngestedEvent) string {
	program := strings.ToLower(detailStringAny(ev, "program", "provider", "event.provider"))
	source := strings.ToLower(detailStringAny(ev, "source", "collector_type", "event.dataset"))
	combined := program + " " + source + " " + strings.ToLower(ev.Collector)
	switch {
	case strings.Contains(combined, "sshd") || strings.Contains(combined, "auth.log") || strings.Contains(combined, "/secure"):
		return "linux.sshd"
	case strings.Contains(combined, "windows-security") || strings.Contains(combined, "security-auditing") || source == "security":
		return "windows.security"
	case strings.Contains(combined, "nginx"):
		return "web.nginx"
	case strings.Contains(combined, "apache") || strings.Contains(combined, "httpd"):
		return "web.apache"
	case strings.Contains(combined, "iis") || strings.Contains(combined, "w3svc"):
		return "web.iis"
	case strings.Contains(combined, "waf"):
		return "web.waf"
	case strings.Contains(combined, "haproxy") || strings.Contains(combined, "traefik") || strings.Contains(combined, "proxy"):
		return "web.reverse_proxy"
	case strings.Contains(combined, "finacle"):
		return "finacle.monitoring"
	case isDatabaseSource(program, source):
		return "database." + firstNonEmpty(strings.Fields(program)...)
	case strings.HasPrefix(ev.Type, "conn."):
		return "network.connection_collector"
	case strings.HasPrefix(ev.Type, "web."):
		return "web.collector"
	default:
		return firstNonEmpty(strings.TrimSpace(ev.Collector), "unknown")
	}
}

func copyEventAlias(dst map[string]any, ev *IngestedEvent, normalized string, aliases ...string) {
	if value, exists := dst[normalized]; exists && strings.TrimSpace(fmt.Sprint(value)) != "" {
		return
	}
	for _, alias := range aliases {
		if value, ok := eventDetailValue(ev, alias); ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			dst[normalized] = value
			return
		}
	}
}

func canonicalSecurityFields(details map[string]any, category, action, outcome string, source *IngestedEvent) map[string]any {
	fields := map[string]any{
		"event": map[string]any{"kind": "event", "category": category, "action": action, "outcome": outcome, "provider": firstNonEmpty(detailStringAny(source, "program", "provider"), source.Collector), "dataset": detailStringAny(source, "source", "collector_type")},
	}
	if value := details["src_ip"]; validIPValue(value) {
		fields["source"] = map[string]any{"ip": value}
	}
	if value := details["dst_ip"]; validIPValue(value) {
		fields["destination"] = map[string]any{"ip": value}
	}
	if value, ok := details["src_port"]; ok {
		putNested(fields, "source", "port", value)
	}
	if value, ok := details["dst_port"]; ok {
		putNested(fields, "destination", "port", value)
	}
	if value, ok := details["user_name"]; ok {
		fields["user"] = map[string]any{"name": value}
	}
	if value, ok := details["protocol"]; ok {
		fields["network"] = map[string]any{"protocol": value}
	}
	if host := firstNonEmpty(detailStringAny(source, "hostname"), source.NodeID); host != "" {
		fields["host"] = map[string]any{"hostname": host}
	}
	return fields
}

func eventDetailValue(ev *IngestedEvent, key string) (any, bool) {
	if ev == nil || ev.Details == nil {
		return nil, false
	}
	if value, ok := ev.Details[key]; ok {
		return value, true
	}
	if fields, ok := ev.Details["fields"].(map[string]any); ok {
		if value, ok := fields[key]; ok {
			return value, true
		}
		for candidate, value := range fields {
			if strings.EqualFold(candidate, key) {
				return value, true
			}
		}
	}
	for candidate, value := range ev.Details {
		if strings.EqualFold(candidate, key) {
			return value, true
		}
	}
	return nil, false
}

func detailStringAny(ev *IngestedEvent, keys ...string) string {
	for _, key := range keys {
		if value, ok := eventDetailValue(ev, key); ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "<nil>" && text != "" {
				return text
			}
		}
	}
	return ""
}

func windowsUser(ev *IngestedEvent) string {
	return detailStringAny(ev, "TargetUserName", "target_user_name", "user_name", "SubjectUserName")
}

func windowsAuthenticationFields(ev *IngestedEvent) map[string]any {
	out := map[string]any{}
	if sourceIP := detailStringAny(ev, "IpAddress", "SourceNetworkAddress", "src_ip", "source.ip"); net.ParseIP(sourceIP) != nil {
		out["src_ip"] = sourceIP
	}
	if port := detailStringAny(ev, "IpPort", "SourcePort", "src_port", "source.port"); port != "" && port != "-" {
		if value, err := strconv.Atoi(port); err == nil {
			out["src_port"] = value
		}
	}
	if logonType := detailStringAny(ev, "LogonType", "logon_type"); logonType != "" {
		out["logon_type"] = logonType
	}
	return out
}

func webFieldsFromEvent(ev *IngestedEvent) (map[string]any, bool) {
	if ev == nil {
		return nil, false
	}
	method := detailStringAny(ev, "http_method", "method", "cs-method")
	path := detailStringAny(ev, "path", "uri", "request_uri", "cs-uri-stem")
	status := detailStringAny(ev, "status_code", "status", "sc-status")
	sourceIP := detailStringAny(ev, "src_ip", "remote_ip", "client_ip", "c-ip")
	if method != "" && path != "" && status != "" && net.ParseIP(sourceIP) != nil {
		out := map[string]any{"src_ip": sourceIP, "http_method": strings.ToUpper(method), "path": path, "status_code": anyInt(status), "protocol": "tcp"}
		if bytesOut := detailStringAny(ev, "bytes_out", "body_bytes_sent", "sc-bytes"); bytesOut != "" {
			out["bytes_out"] = anyInt64(bytesOut)
		}
		return out, true
	}
	if match := commonWebPattern.FindStringSubmatch(strings.TrimSpace(ev.Message)); len(match) == 6 {
		return map[string]any{"src_ip": match[1], "http_method": match[2], "path": match[3], "status_code": anyInt(match[4]), "bytes_out": anyInt64(strings.ReplaceAll(match[5], "-", "0")), "protocol": "tcp"}, true
	}
	// Default IIS W3C fields: date time s-ip cs-method cs-uri-stem
	// cs-uri-query s-port cs-username c-ip cs(User-Agent) cs(Referer)
	// sc-status sc-substatus sc-win32-status time-taken.
	parts := strings.Fields(ev.Message)
	if len(parts) >= 15 && strings.Contains(parts[0], "-") && strings.Contains(parts[1], ":") {
		if statusCode, err := strconv.Atoi(parts[11]); err == nil && net.ParseIP(parts[8]) != nil {
			return map[string]any{"src_ip": parts[8], "http_method": parts[3], "path": parts[4], "dst_port": anyInt(parts[6]), "status_code": statusCode, "protocol": "tcp"}, true
		}
	}
	return nil, false
}

func structuredWebFields(ev *IngestedEvent) map[string]any {
	out := map[string]any{"protocol": "tcp"}
	for output, candidates := range map[string][]string{
		"src_ip":      {"src_ip", "source_ip", "remote_ip", "client_ip", "c-ip"},
		"dst_ip":      {"dst_ip", "destination_ip", "server_ip", "s-ip"},
		"http_method": {"http_method", "method", "cs-method"},
		"path":        {"path", "uri", "request_uri", "cs-uri-stem"},
		"status_code": {"status_code", "status", "sc-status"},
		"dst_port":    {"dst_port", "destination_port", "port", "server_port", "s-port"},
		"bytes_in":    {"bytes_in", "request_bytes", "cs-bytes"},
		"bytes_out":   {"bytes_out", "body_bytes_sent", "sc-bytes"},
	} {
		if value := detailStringAny(ev, candidates...); value != "" {
			out[output] = value
		}
	}
	if ev.SrcIP != "" {
		out["src_ip"] = ev.SrcIP
	}
	if ev.DstIP != "" {
		out["dst_ip"] = ev.DstIP
	}
	if ev.DstPort != 0 {
		out["dst_port"] = ev.DstPort
	}
	if ev.BytesIn != 0 {
		out["bytes_in"] = ev.BytesIn
	}
	if ev.BytesOut != 0 {
		out["bytes_out"] = ev.BytesOut
	}
	for _, key := range []string{"status_code", "dst_port", "bytes_in", "bytes_out"} {
		if value, ok := out[key]; ok {
			out[key] = anyInt64(value)
		}
	}
	if value, ok := out["status_code"]; ok {
		out["status_code"] = int(value.(int64))
	}
	if value, ok := out["dst_port"]; ok {
		out["dst_port"] = int(value.(int64))
	}
	return out
}
func databaseAuthenticationFailed(ev *IngestedEvent) bool {
	text := strings.ToLower(ev.Message + " " + detailStringAny(ev, "error", "message", "outcome", "auth_result"))
	return containsAnyText(text, "password authentication failed", "access denied for user", "login failed for user", "ora-01017", "authentication failed", "invalid credentials")
}
func isDatabaseSource(program, source string) bool {
	return containsAnyText(program+" "+source, "postgres", "mysql", "mariadb", "sqlserver", "mssql", "oracle", "database", "finacle")
}
func isWebSource(program, source string) bool {
	return containsAnyText(program+" "+source, "nginx", "apache", "httpd", "iis", "haproxy", "traefik", "reverse_proxy", "waf", "access.log")
}
func containsAnyText(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
func anyInt(value any) int {
	number, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
	return number
}
func anyInt64(value any) int64 {
	number, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
	return number
}
func inferredWebPort(source string, ev *IngestedEvent) int {
	if strings.Contains(source, "https") || strings.EqualFold(detailStringAny(ev, "scheme"), "https") {
		return 443
	}
	return 80
}
func outcomeForHTTPStatus(status int) string {
	if status >= 400 {
		return "failure"
	}
	if status > 0 {
		return "success"
	}
	return "unknown"
}
func copyEventField(dst map[string]any, key string, value any) {
	if value != nil && fmt.Sprint(value) != "" && fmt.Sprint(value) != "0" {
		dst[key] = value
	}
}
func validIPValue(value any) bool { return net.ParseIP(strings.TrimSpace(fmt.Sprint(value))) != nil }
func putNested(fields map[string]any, object, key string, value any) {
	nested, _ := fields[object].(map[string]any)
	if nested == nil {
		nested = map[string]any{}
		fields[object] = nested
	}
	nested[key] = value
}
