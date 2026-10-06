package networkdevice

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// ConfigurationSnapshot is the bounded, sanitized payload accepted from an
// assigned edge collector. ContentHash is always recomputed by the receiver.
type ConfigurationSnapshot struct {
	Adapter        string `json:"adapter"`
	AdapterVersion string `json:"adapter_version"`
	Format         string `json:"format"`
	Content        string `json:"content"`
	ContentHash    string `json:"content_hash"`
	Sanitized      bool   `json:"sanitized"`
}

// NormalizeConfigurationSnapshot removes common secret fields and normalizes
// content before hashing or persistence. Collector adapters also scrub their
// configured credential values before sending; the control plane stores
// encrypted credential references and cannot retrieve those values itself.
func NormalizeConfigurationSnapshot(adapter, adapterVersion, format, content string) (ConfigurationSnapshot, error) {
	adapter = strings.TrimSpace(adapter)
	adapterVersion = strings.TrimSpace(adapterVersion)
	format = strings.ToLower(strings.TrimSpace(format))
	if adapter == "" || len(adapter) > 128 || adapterVersion == "" || len(adapterVersion) > 64 || strings.ContainsAny(adapter+adapterVersion, "\r\n\x00") {
		return ConfigurationSnapshot{}, fmt.Errorf("invalid snapshot adapter metadata")
	}
	if format != "text" && format != "json" && format != "xml" {
		return ConfigurationSnapshot{}, fmt.Errorf("unsupported snapshot format")
	}
	if len(content) == 0 || len(content) > 16384 {
		return ConfigurationSnapshot{}, fmt.Errorf("snapshot must contain 1 to 16384 bytes")
	}
	clean, err := sanitizeConfigurationContent(format, content)
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	if clean == "" || len(clean) > 16384 {
		return ConfigurationSnapshot{}, fmt.Errorf("sanitized snapshot must contain 1 to 16384 bytes")
	}
	sum := sha256.Sum256([]byte(clean))
	return ConfigurationSnapshot{Adapter: adapter, AdapterVersion: adapterVersion, Format: format, Content: clean, ContentHash: hex.EncodeToString(sum[:]), Sanitized: true}, nil
}

func sanitizeConfigurationContent(format, content string) (string, error) {
	if format == "json" {
		var value any
		if err := json.Unmarshal([]byte(content), &value); err != nil {
			return "", fmt.Errorf("invalid JSON snapshot")
		}
		redactConfigurationJSON(value)
		clean, err := json.Marshal(value)
		return string(clean), err
	}
	if format == "xml" {
		decoder := xml.NewDecoder(strings.NewReader(content))
		var out bytes.Buffer
		encoder := xml.NewEncoder(&out)
		depth := 0
		for {
			tok, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("invalid XML snapshot")
			}
			switch el := tok.(type) {
			case xml.StartElement:
				if depth > 0 {
					depth++
					continue
				}
				if sensitiveField(el.Name.Local) {
					depth = 1
					continue
				}
				for i := range el.Attr {
					if sensitiveField(el.Attr[i].Name.Local) {
						el.Attr[i].Value = "[redacted]"
					}
				}
				sort.Slice(el.Attr, func(i, j int) bool {
					if el.Attr[i].Name.Space == el.Attr[j].Name.Space {
						return el.Attr[i].Name.Local < el.Attr[j].Name.Local
					}
					return el.Attr[i].Name.Space < el.Attr[j].Name.Space
				})
				tok = el
			case xml.EndElement:
				if depth > 0 {
					depth--
					continue
				}
			case xml.CharData:
				if strings.TrimSpace(string(el)) == "" {
					continue
				}
			}
			if depth == 0 {
				if err := encoder.EncodeToken(tok); err != nil {
					return "", err
				}
			}
		}
		if err := encoder.Flush(); err != nil {
			return "", err
		}
		return strings.TrimSpace(out.String()), nil
	}
	text := strings.ReplaceAll(strings.ReplaceAll(strings.ToValidUTF8(content, ""), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	private := false
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "BEGIN ") && strings.Contains(upper, "PRIVATE KEY") {
			private = true
		}
		if private || sensitiveField(strings.ToLower(strings.TrimSpace(line))) {
			lines[i] = "[redacted]"
		} else {
			lines[i] = line
		}
		if strings.Contains(upper, "END ") && strings.Contains(upper, "PRIVATE KEY") {
			private = false
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func redactConfigurationJSON(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if sensitiveField(key) {
				v[key] = "[redacted]"
			} else {
				redactConfigurationJSON(child)
			}
		}
	case []any:
		for _, child := range v {
			redactConfigurationJSON(child)
		}
	}
}

// ConfigurationDiff returns a deterministic line-multiset diff. Sorting makes
// output stable even when a configuration contains repeated statements.
func ConfigurationDiff(before, after string) (added, removed []string) {
	added, removed = []string{}, []string{}
	oldLines := configurationLines(before)
	newLines := configurationLines(after)
	oldCount, newCount := map[string]int{}, map[string]int{}
	for _, line := range oldLines {
		oldCount[line]++
	}
	for _, line := range newLines {
		newCount[line]++
	}
	for line, count := range newCount {
		for i := oldCount[line]; i < count; i++ {
			added = append(added, line)
		}
	}
	for line, count := range oldCount {
		for i := newCount[line]; i < count; i++ {
			removed = append(removed, line)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func configurationLines(content string) []string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

type ConfigurationFinding struct {
	ID                 string   `json:"id"`
	Status             string   `json:"status"`
	Severity           string   `json:"severity,omitempty"`
	Title              string   `json:"title"`
	Evidence           []string `json:"evidence,omitempty"`
	SnapshotID         string   `json:"snapshot_id,omitempty"`
	EvidenceRef        string   `json:"evidence_ref,omitempty"`
	RelatedEvidenceRef string   `json:"related_evidence_ref,omitempty"`
}

var configRulePatterns = []struct {
	id, title, severity string
	pattern             *regexp.Regexp
}{
	{"telnet_management", "Telnet management enabled", "high", regexp.MustCompile(`(?i)^\s*(?:ip\s+)?telnet\s+server\b|^\s*transport\s+input\b.*\btelnet\b`)},
	{"http_management", "Cleartext HTTP management enabled", "high", regexp.MustCompile(`(?i)^\s*(?:ip\s+http\s+server|http-server\s+enable|web-management\s+http)\b`)},
	{"snmp_weak_community", "SNMPv1/v2c community configured", "high", regexp.MustCompile(`(?i)^\s*(?:snmp-server\s+community|snmp-server\s+host\s+\S+\s+version\s+(?:1|2|2c)\b)`)},
}

// EvaluateConfigurationPosture only reports exact evidence matched by the
// small read-only rule set. Everything else is explicitly unsupported.
func EvaluateConfigurationPosture(snapshotID, content string) []ConfigurationFinding {
	return EvaluateConfigurationPostureForSnapshot(snapshotID, "text", content)
}

func EvaluateConfigurationPostureForSnapshot(snapshotID, format, content string) []ConfigurationFinding {
	findings := make([]ConfigurationFinding, 0, len(configRulePatterns)+8)
	if format != "text" {
		for _, rule := range configRulePatterns {
			findings = append(findings, ConfigurationFinding{ID: rule.id, Status: "unsupported", Title: rule.title + " (text parser required)", SnapshotID: snapshotID})
		}
		findings = append(findings, unsupportedConfigurationChecks(snapshotID)...)
		return findings
	}
	lines := strings.Split(content, "\n")
	for _, rule := range configRulePatterns {
		matched := []string{}
		for n, line := range lines {
			if rule.pattern.MatchString(line) {
				matched = append(matched, fmt.Sprintf("line:%d", n+1))
			}
		}
		finding := ConfigurationFinding{ID: rule.id, Title: rule.title, SnapshotID: snapshotID}
		if len(matched) == 0 {
			finding.Status = "not_observed"
		} else {
			finding.Status = "finding"
			finding.Severity = rule.severity
			finding.Evidence = matched
			finding.EvidenceRef = "network_configuration_snapshots:" + snapshotID
		}
		findings = append(findings, finding)
	}
	findings = append(findings, unsupportedConfigurationChecks(snapshotID)...)
	return findings
}

func unsupportedConfigurationChecks(snapshotID string) []ConfigurationFinding {
	return []ConfigurationFinding{
		{ID: "remote_admin_public_exposure", Status: "unsupported", Title: "Public management exposure requires network path context", SnapshotID: snapshotID},
		{ID: "management_acl_strength", Status: "unsupported", Title: "Management ACL strength requires complete policy and network context", SnapshotID: snapshotID},
		{ID: "firmware_freshness", Status: "unsupported", Title: "Firmware support age requires a vendor/version advisory source", SnapshotID: snapshotID},
		{ID: "insecure_protocol_cipher", Status: "unsupported", Title: "Protocol and cipher assessment requires a vendor-aware parser", SnapshotID: snapshotID},
		{ID: "unnecessary_management_service", Status: "unsupported", Title: "Unused management service assessment requires intended-service policy", SnapshotID: snapshotID},
		{ID: "ha_degradation", Status: "unsupported", Title: "High-availability health requires live peer state", SnapshotID: snapshotID},
		{ID: "interface_link_instability", Status: "unsupported", Title: "Interface stability requires time-series counter evidence", SnapshotID: snapshotID},
		{ID: "routing_neighbor_churn", Status: "unsupported", Title: "Routing neighbor stability requires time-series protocol evidence", SnapshotID: snapshotID},
	}
}
