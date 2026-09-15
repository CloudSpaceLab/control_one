package server

import (
	"strings"
)

// ClassifyTarget examines node labels, OS, and network observations to derive
// a target_type classification. It returns the classified type, confidence (0-100),
// evidence strings, and the source ("server_heuristic").
func ClassifyTarget(labels map[string]any, os string, arch string, publicIP string) (targetType string, confidence int, evidence []string) {
	if labels == nil {
		labels = map[string]any{}
	}

	// Rule 1: If agent already classified with confidence >= 70, preserve it.
	if agentConf := labelInt(labels, "target.classification_confidence", 0); agentConf >= 70 {
		if agentType := labelString(labels, "target.type", ""); agentType != "" && agentType != "unknown" {
			return agentType, agentConf, labelStringSlice(labels, "target.classification_evidence")
		}
	}

	osLower := strings.ToLower(os)

	// Rule 5 (checked early because it's specific): Domain controller role.
	if isDomainController(labels) {
		return "domain_controller", 75, []string{"domain controller role detected"}
	}

	// Rule 2: Battery present + desktop OS → personal_pc.
	if hasBattery(labels) && isDesktopOS(osLower) {
		return "personal_pc", 70, []string{"battery present", "desktop OS: " + os}
	}

	// Rule 3: Server OS → server.
	if isServerOS(osLower) {
		evidence := []string{"server OS: " + os}
		if svc := serviceKeywords(labels); len(svc) > 0 {
			evidence = append(evidence, svc...)
		}
		return "server", 65, evidence
	}

	// Rule 4: Cloud metadata hints → cloud_instance.
	if hasCloudMetadata(labels) {
		evidence := []string{"cloud metadata detected"}
		if cp := labelString(labels, "target.cloud_provider", ""); cp != "" {
			evidence = append(evidence, "cloud_provider: "+cp)
		}
		return "cloud_instance", 60, evidence
	}

	// Rule 6: Server service keywords even without server OS.
	if svc := serviceKeywords(labels); len(svc) > 0 {
		return "server", 60, svc
	}

	// Default: unknown.
	return "unknown", 0, nil
}

// isServerOS returns true if the OS string indicates a server edition.
func isServerOS(osLower string) bool {
	if strings.Contains(osLower, "server") {
		return true
	}
	// Ubuntu/Debian with "server" edition in label is handled by label check in caller.
	return false
}

// isDesktopOS returns true if the OS string indicates a personal/desktop edition.
func isDesktopOS(osLower string) bool {
	// Explicit server check first — anything with "server" is not desktop.
	if strings.Contains(osLower, "server") {
		return false
	}
	desktopHints := []string{"windows 10", "windows 11", "windows 7", "windows 8", "macos", "darwin", "desktop"}
	for _, hint := range desktopHints {
		if strings.Contains(osLower, hint) {
			return true
		}
	}
	// Non-server Linux without server keywords is treated as desktop when battery is present.
	if strings.Contains(osLower, "linux") || strings.Contains(osLower, "ubuntu") || strings.Contains(osLower, "debian") {
		return true
	}
	return false
}

// hasBattery checks labels for battery presence indicators.
func hasBattery(labels map[string]any) bool {
	if v, ok := labels["target.battery_present"]; ok {
		return toBool(v)
	}
	if v, ok := labels["device.battery"]; ok {
		return toBool(v)
	}
	return false
}

// isDomainController checks labels for domain controller role.
func isDomainController(labels map[string]any) bool {
	if v, ok := labels["target.domain_controller"]; ok {
		return toBool(v)
	}
	if v, ok := labels["domain_controller"]; ok {
		return toBool(v)
	}
	return false
}

// hasCloudMetadata checks labels for cloud provider or metadata hints.
func hasCloudMetadata(labels map[string]any) bool {
	if _, ok := labels["target.cloud_provider"]; ok {
		return true
	}
	if v, ok := labels["cloud_metadata"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return true
		}
	}
	return false
}

// serviceKeywords returns evidence strings for known server service labels.
func serviceKeywords(labels map[string]any) []string {
	serverServiceKeywords := []string{"webserver", "database", "cache", "message-queue", "redis", "mysql", "postgres", "nginx", "apache", "kafka", "rabbitmq"}
	var evidence []string
	for _, kw := range serverServiceKeywords {
		for key, val := range labels {
			if strings.Contains(strings.ToLower(key), "service") || strings.Contains(strings.ToLower(key), "role") {
				if s, ok := val.(string); ok && strings.Contains(strings.ToLower(s), kw) {
					evidence = append(evidence, key+"="+s)
				}
			}
		}
	}
	return evidence
}

// toBool interprets various label value types as a boolean.
func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		lower := strings.ToLower(strings.TrimSpace(val))
		return lower == "true" || lower == "1" || lower == "yes"
	case float64:
		return val != 0
	case int:
		return val != 0
	default:
		return false
	}
}

// labelInt extracts an integer value from a labels map, returning fallback if absent or unconvertible.
func labelInt(labels map[string]any, key string, fallback int) int {
	if labels == nil {
		return fallback
	}
	v, ok := labels[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case string:
		if n == "true" {
			return 1
		}
		return fallback
	default:
		return fallback
	}
}
