package offlinebundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

const ContentTypePrivateAccessProviderManifest = "private_access_provider_manifest"

type PrivateAccessProviderManifest struct {
	SchemaVersion int                                 `json:"schema_version"`
	Provider      privateaccess.ProviderKind          `json:"provider"`
	Name          string                              `json:"name"`
	DisplayName   string                              `json:"display_name,omitempty"`
	Version       string                              `json:"version"`
	GeneratedAt   string                              `json:"generated_at,omitempty"`
	Description   string                              `json:"description,omitempty"`
	Capabilities  []string                            `json:"capabilities,omitempty"`
	HTTPImport    PrivateAccessHTTPImportManifest     `json:"http_import,omitempty"`
	Policy        PrivateAccessProviderPolicyManifest `json:"policy,omitempty"`
	Artifacts     []PrivateAccessProviderArtifact     `json:"artifacts,omitempty"`
	Metadata      map[string]any                      `json:"metadata,omitempty"`
}

type PrivateAccessHTTPImportManifest struct {
	Endpoints              map[string]string `json:"endpoints,omitempty"`
	AuthorizationSchemes   []string          `json:"authorization_schemes,omitempty"`
	DefaultIntervalSeconds int               `json:"default_interval_seconds,omitempty"`
	TLSRequired            bool              `json:"tls_required,omitempty"`
}

type PrivateAccessProviderPolicyManifest struct {
	Guardrails []string                      `json:"guardrails,omitempty"`
	Templates  []PrivateAccessPolicyTemplate `json:"templates,omitempty"`
}

type PrivateAccessPolicyTemplate struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Type              string         `json:"type,omitempty"`
	Description       string         `json:"description,omitempty"`
	SourceGroups      []string       `json:"source_groups,omitempty"`
	DestinationGroups []string       `json:"destination_groups,omitempty"`
	RoutingPeerGroups []string       `json:"routing_peer_groups,omitempty"`
	Protocols         []string       `json:"protocols,omitempty"`
	Ports             []string       `json:"ports,omitempty"`
	Guards            []string       `json:"guards,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type PrivateAccessProviderArtifact struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256,omitempty"`
	Description string `json:"description,omitempty"`
}

type ActivePrivateAccessProviderManifest struct {
	Manifest       PrivateAccessProviderManifest `json:"manifest"`
	ActivePath     string                        `json:"active_path"`
	ContentReceipt ContentReceipt                `json:"content_receipt"`
}

func LoadActivePrivateAccessProviderManifests(rootDir string) ([]ActivePrivateAccessProviderManifest, error) {
	activeRoot := filepath.Join(strings.TrimSpace(rootDir), "active", cleanName(ContentTypePrivateAccessProviderManifest))
	matches, err := filepath.Glob(filepath.Join(activeRoot, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	out := make([]ActivePrivateAccessProviderManifest, 0, len(matches))
	for _, activePath := range matches {
		if strings.HasSuffix(activePath, ".receipt.json") {
			continue
		}
		manifest, err := loadPrivateAccessProviderManifest(activePath)
		if err != nil {
			return nil, err
		}
		out = append(out, ActivePrivateAccessProviderManifest{
			Manifest:       *manifest,
			ActivePath:     activePath,
			ContentReceipt: readContentReceipt(activePath + ".receipt.json"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manifest.Provider != out[j].Manifest.Provider {
			return out[i].Manifest.Provider < out[j].Manifest.Provider
		}
		if out[i].Manifest.Name != out[j].Manifest.Name {
			return out[i].Manifest.Name < out[j].Manifest.Name
		}
		if out[i].Manifest.Version != out[j].Manifest.Version {
			return out[i].Manifest.Version > out[j].Manifest.Version
		}
		return out[i].ActivePath < out[j].ActivePath
	})
	return out, nil
}

func loadPrivateAccessProviderManifest(path string) (*PrivateAccessProviderManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest PrivateAccessProviderManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse private-access provider manifest %s: %w", path, err)
	}
	if err := ValidatePrivateAccessProviderManifest(manifest); err != nil {
		return nil, fmt.Errorf("invalid private-access provider manifest %s: %w", path, err)
	}
	return &manifest, nil
}

func ValidatePrivateAccessProviderManifest(manifest PrivateAccessProviderManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported private-access provider manifest schema_version %d", manifest.SchemaVersion)
	}
	if !privateaccess.ValidProvider(manifest.Provider) {
		return fmt.Errorf("unsupported private-access provider %q", manifest.Provider)
	}
	if strings.TrimSpace(manifest.Name) == "" || strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("private-access provider manifest name and version required")
	}
	if len(manifest.HTTPImport.Endpoints) == 0 {
		return fmt.Errorf("private-access provider manifest http_import.endpoints required")
	}
	for key, value := range manifest.HTTPImport.Endpoints {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return fmt.Errorf("private-access provider manifest endpoint key and path required")
		}
		if !strings.HasPrefix(value, "/") {
			return fmt.Errorf("private-access provider manifest endpoint %s must be a relative API path", key)
		}
	}
	if manifest.HTTPImport.DefaultIntervalSeconds < 0 {
		return fmt.Errorf("private-access provider manifest default_interval_seconds must be non-negative")
	}
	if len(manifest.Policy.Templates) == 0 {
		return fmt.Errorf("private-access provider manifest policy.templates required")
	}
	seenTemplates := map[string]struct{}{}
	for _, template := range manifest.Policy.Templates {
		id := strings.TrimSpace(template.ID)
		if id == "" || strings.TrimSpace(template.Name) == "" {
			return fmt.Errorf("private-access provider manifest policy template id and name required")
		}
		if _, ok := seenTemplates[id]; ok {
			return fmt.Errorf("duplicate private-access provider policy template %s", id)
		}
		seenTemplates[id] = struct{}{}
	}
	return nil
}

func validateKnownContentPayload(content ContentFile, data []byte) error {
	switch strings.TrimSpace(content.Type) {
	case ContentTypePrivateAccessProviderManifest:
		var manifest PrivateAccessProviderManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("parse private-access provider manifest %s: %w", content.Path, err)
		}
		if err := ValidatePrivateAccessProviderManifest(manifest); err != nil {
			return fmt.Errorf("invalid private-access provider manifest %s: %w", content.Path, err)
		}
	case ContentTypeRemediationPack:
		return validateRemediationPackPayload(content, data)
	case ContentTypeAIInvestigationPack:
		return validateAIInvestigationPackPayload(content, data)
	}
	return nil
}
