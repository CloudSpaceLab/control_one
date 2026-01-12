package wizard

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/access"
	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/hooks"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
	"github.com/CloudSpaceLab/control_one/internal/secrets"
)

const defaultWizardTimeout = 45 * time.Second

var errStepSkipped = errors.New("wizard step skipped")

type skipError struct {
	reason string
}

func (e skipError) Error() string {
	return e.reason
}

func (e skipError) Unwrap() error {
	return errStepSkipped
}

func newSkipError(reason string) error {
	return skipError{reason: reason}
}

type Summary struct {
	CertGenerated bool              `json:"cert_generated"`
	AccessSynced  bool              `json:"access_synced"`
	SecretsSynced bool              `json:"secrets_synced"`
	Provisioned   bool              `json:"provisioned"`
	ComplianceRan bool              `json:"compliance_ran"`
	Notes         map[string]string `json:"notes,omitempty"`
}

type Runner struct {
	log     *zap.Logger
	cfg     *config.Config
	client  *api.Client
	nodeID  string
	summary *Summary
	hooks   hooks.Publisher
}

func NewRunner(log *zap.Logger, cfg *config.Config, client *api.Client, nodeID string, publisher hooks.Publisher) *Runner {
	return &Runner{
		log:    log,
		cfg:    cfg,
		client: client,
		nodeID: nodeID,
		hooks:  publisher,
		summary: &Summary{
			Notes: make(map[string]string),
		},
	}
}

func (r *Runner) Enabled() bool {
	return r.cfg != nil && r.cfg.Wizard.Enabled
}

func (r *Runner) Run(ctx context.Context) error {
	if !r.Enabled() {
		return nil
	}

	if err := r.cfg.EnsureDirectories(); err != nil {
		return fmt.Errorf("ensure directories: %w", err)
	}

	if err := r.ensureCertificates(); err != nil {
		return fmt.Errorf("ensure certificates: %w", err)
	}

	timeout := r.cfg.Wizard.Timeout
	if timeout <= 0 {
		timeout = defaultWizardTimeout
	}

	steps := []struct {
		key     string
		enabled bool
		action  func(context.Context) error
		mark    func()
	}{
		{"access", r.cfg.Wizard.RunAccessSync, r.runAccessSync, func() { r.summary.AccessSynced = true }},
		{"secrets", r.cfg.Wizard.RunSecretsSync, r.runSecretsSync, func() { r.summary.SecretsSynced = true }},
		{"provisioning", r.cfg.Wizard.RunProvisioning, r.runProvisioning, func() { r.summary.Provisioned = true }},
		{"compliance", r.cfg.Wizard.RunCompliance, r.runCompliance, func() { r.summary.ComplianceRan = true }},
	}

	for _, step := range steps {
		if !step.enabled {
			continue
		}
		err := r.runStep(ctx, timeout, step.key, step.action)
		if err == nil {
			step.mark()
			r.publishEvent("wizard.step.success", map[string]any{
				"step":    step.key,
				"node_id": r.resolvedNodeID(),
			})
			continue
		}
		if errors.Is(err, errStepSkipped) {
			r.publishEvent("wizard.step.skipped", map[string]any{
				"step":    step.key,
				"node_id": r.resolvedNodeID(),
				"note":    r.summary.Notes[step.key],
			})
			continue
		}
		r.publishEvent("wizard.step.failed", map[string]any{
			"step":    step.key,
			"node_id": r.resolvedNodeID(),
			"error":   r.summary.Notes[step.key],
		})
	}

	if r.cfg.Wizard.EmitSummary {
		r.emitSummary()
	}

	return nil
}

func (r *Runner) ensureCertificates() error {
	if !r.cfg.Wizard.AutoGenerateCertificates {
		return nil
	}

	certPath := r.cfg.TLS.CertFile
	keyPath := r.cfg.TLS.KeyFile

	if certPath == "" || keyPath == "" {
		return fmt.Errorf("tls cert/key paths must be set for auto-generation")
	}

	if exists(certPath) && exists(keyPath) {
		r.summary.CertGenerated = false
		r.log.Info("wizard certificates already present", zap.String("cert", certPath), zap.String("key", keyPath))
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o750); err != nil {
		return fmt.Errorf("create key dir: %w", err)
	}

	hosts := r.cfg.Wizard.Hosts
	if len(hosts) == 0 {
		hosts = append(hosts, defaultHosts()...)
	}

	certBytes, keyBytes, err := generateSelfSigned(hosts, r.cfg.Wizard.Organization, r.cfg.Wizard.CertValidity)
	if err != nil {
		return err
	}

	if err := os.WriteFile(certPath, certBytes, 0o640); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyBytes, 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	r.summary.CertGenerated = true
	r.log.Info("wizard generated certificates", zap.String("cert", certPath), zap.String("key", keyPath))
	return nil
}

func (r *Runner) runStep(parent context.Context, timeout time.Duration, key string, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	if err := fn(ctx); err != nil {
		var skipped skipError
		if errors.As(err, &skipped) {
			r.summary.Notes[key] = skipped.Error()
			r.log.Info("wizard step skipped", zap.String("step", key), zap.String("reason", skipped.Error()))
			return errStepSkipped
		}

		r.summary.Notes[key] = err.Error()
		r.log.Warn("wizard step failed", zap.String("step", key), zap.Error(err))
		return err
	}

	r.log.Info("wizard step complete", zap.String("step", key))
	return nil
}

func (r *Runner) runAccessSync(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("api client unavailable")
	}

	manager := access.NewManager(r.log, r.client, access.Options{
		Provider:     access.ProviderType(r.cfg.Access.Provider),
		SyncInterval: r.cfg.Access.SyncInterval,
		DefaultRole:  r.cfg.Access.DefaultRole,
		APIEndpoint:  r.cfg.Access.APIEndpoint,
		NodeID:       r.resolvedNodeID(),
	})
	return manager.Sync(ctx)
}

func (r *Runner) runSecretsSync(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("api client unavailable")
	}

	store := secrets.NewStore(r.log, r.client, secrets.Options{
		Backend:      secrets.BackendType(r.cfg.Secrets.Backend),
		Endpoint:     r.cfg.Secrets.Endpoint,
		Groups:       r.cfg.Secrets.Groups,
		SyncInterval: r.cfg.Secrets.SyncInterval,
		NodeID:       r.resolvedNodeID(),
	})
	return store.Sync(ctx)
}

func (r *Runner) runProvisioning(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("api client unavailable")
	}
	if strings.TrimSpace(r.cfg.Provisioning.Template) == "" {
		return newSkipError("template not configured")
	}

	provider := strings.ToLower(strings.TrimSpace(r.cfg.Provisioning.Provider))
	if provider == "" {
		if p, hints := provisioning.DetectProvider(); p != "unknown" {
			provider = p
			if r.cfg.Provisioning.Metadata == nil {
				r.cfg.Provisioning.Metadata = map[string]string{}
			}
			for k, v := range hints {
				if _, exists := r.cfg.Provisioning.Metadata[k]; !exists {
					r.cfg.Provisioning.Metadata[k] = v
				}
			}
		}
	}

	metadata := map[string]string{
		"wizard":       "true",
		"node_name":    r.cfg.NodeName,
		"generated_by": "control_one_wizard",
	}
	for k, v := range r.cfg.Provisioning.Metadata {
		metadata[k] = v
	}

	autofilled := populateMetadataFromEnv(provider, metadata)
	if len(autofilled) > 0 {
		r.log.Info("provisioning metadata populated from environment", zap.String("provider", provider), zap.Strings("keys", autofilled))
	}

	if provider != "" {
		if missing := missingKeys(requiredKeysForProvider(provider), metadata); len(missing) > 0 {
			r.log.Warn("provisioning metadata missing required keys for provider",
				zap.String("provider", provider),
				zap.Strings("missing", missing))
		}
	}

	engine := provisioning.NewEngine(r.log, r.client, provisioning.Options{
		Template:        r.cfg.Provisioning.Template,
		Provider:        provider,
		Baselines:       r.cfg.Provisioning.Baselines,
		AutoRemediation: r.cfg.Provisioning.AutoRemediation,
	})

	if err := engine.ApplyTemplate(ctx, r.resolvedNodeID(), metadata); err != nil {
		return err
	}

	return engine.RunBaselines(ctx, r.resolvedNodeID())
}

func (r *Runner) runCompliance(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("api client unavailable")
	}
	if len(r.cfg.Compliance.RuleSets) == 0 {
		return newSkipError("no compliance rule sets configured")
	}

	engine := compliance.NewEngine(r.log, r.client, compliance.Options{
		Region:         r.cfg.Compliance.Region,
		RuleSets:       r.cfg.Compliance.RuleSets,
		Certifications: r.cfg.Compliance.Certifications,
		AutoApply:      r.cfg.Compliance.AutoApplyTemplates,
	})
	_, err := engine.Evaluate(ctx, r.resolvedNodeID(), map[string]string{})
	return err
}

func (r *Runner) emitSummary() {
	summaryJSON, err := json.MarshalIndent(r.summary, "", "  ")
	if err != nil {
		return
	}

	r.log.Info("wizard completed", zap.ByteString("summary", summaryJSON))
	r.publishEvent("wizard.run.summary", map[string]any{
		"node_id":   r.resolvedNodeID(),
		"summary":   r.summary,
		"timestamp": time.Now().UTC(),
	})
}

func (r *Runner) resolvedNodeID() string {
	if r.nodeID != "" {
		return r.nodeID
	}
	if r.cfg != nil && r.cfg.NodeName != "" {
		return r.cfg.NodeName
	}
	return ""
}

func (r *Runner) publishEvent(eventID string, payload map[string]any) {
	if r.hooks == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := r.hooks.PublishEvent(ctx, &hooks.Event{
		EventID:   eventID,
		Source:    "wizard",
		Subject:   r.resolvedNodeID(),
		Payload:   payload,
		Metadata:  map[string]string{"component": "wizard"},
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		r.log.Debug("wizard hook publish failed", zap.String("event", eventID), zap.Error(err))
	}
}

var providerEnvFallbacks = map[string]map[string][]string{
	"vmware": {
		"cluster":    {"VMWARE_CLUSTER"},
		"datacenter": {"VMWARE_DATACENTER"},
		"datastore":  {"VMWARE_DATASTORE"},
		"network":    {"VMWARE_NETWORK"},
	},
	"libvirt": {
		"pool":    {"LIBVIRT_POOL"},
		"network": {"LIBVIRT_NETWORK"},
		"image":   {"LIBVIRT_IMAGE", "LIBVIRT_IMAGE_PATH"},
	},
	"aws": {
		"region":      {"AWS_REGION", "AWS_DEFAULT_REGION"},
		"vpc_id":      {"AWS_VPC_ID"},
		"subnet_id":   {"AWS_SUBNET_ID"},
		"iam_profile": {"AWS_IAM_INSTANCE_PROFILE"},
	},
	"azure": {
		"subscription_id": {"AZURE_SUBSCRIPTION_ID"},
		"resource_group":  {"AZURE_RESOURCE_GROUP"},
		"vnet":            {"AZURE_VNET"},
		"subnet":          {"AZURE_SUBNET"},
	},
	"gcp": {
		"project": {"GOOGLE_CLOUD_PROJECT", "GCP_PROJECT"},
		"zone":    {"GOOGLE_CLOUD_ZONE", "GCP_ZONE"},
		"network": {"GCP_NETWORK"},
	},
}

func populateMetadataFromEnv(provider string, metadata map[string]string) []string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}
	fallbacks, ok := providerEnvFallbacks[provider]
	if !ok {
		return nil
	}
	populated := make([]string, 0, len(fallbacks))
	for key, envVars := range fallbacks {
		if strings.TrimSpace(metadata[key]) != "" {
			continue
		}
		for _, envKey := range envVars {
			if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
				metadata[key] = val
				populated = append(populated, key)
				break
			}
		}
	}
	return populated
}

func requiredKeysForProvider(provider string) []string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "vmware":
		return []string{"cluster", "datacenter", "datastore", "network"}
	case "libvirt":
		return []string{"pool", "network", "image"}
	case "aws":
		return []string{"region", "vpc_id", "subnet_id"}
	case "azure":
		return []string{"subscription_id", "resource_group", "vnet", "subnet"}
	case "gcp":
		return []string{"project", "zone", "network"}
	default:
		return nil
	}
}

func missingKeys(required []string, metadata map[string]string) []string {
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if strings.TrimSpace(metadata[key]) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func generateSelfSigned(hosts []string, organization string, validity time.Duration) ([]byte, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: bigIntRandom(),
		Subject: pkix.Name{
			Organization: []string{organization},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal ecdsa key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM, nil
}

func defaultHosts() []string {
	host, _ := os.Hostname()
	return []string{strings.TrimSpace(host), "127.0.0.1", "::1"}
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func bigIntRandom() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}

// TemplateWizard extends the wizard with template management capabilities
type TemplateWizard struct {
	*Runner
}

func NewTemplateWizard(log *zap.Logger, cfg *config.Config, client *api.Client, nodeID string, publisher hooks.Publisher) *TemplateWizard {
	return &TemplateWizard{
		Runner: NewRunner(log, cfg, client, nodeID, publisher),
	}
}

// ExecuteTemplate executes a template with the given parameters
func (tw *TemplateWizard) ExecuteTemplate(ctx context.Context, templateID string, templateType, targetType string, targetID *string, parameters map[string]any) error {
	if tw.client == nil {
		return fmt.Errorf("api client unavailable")
	}

	// Create template execution request
	executionReq := map[string]any{
		"template_id":   templateID,
		"template_type": templateType,
		"target_type":   targetType,
		"parameters":    parameters,
		"dry_run":       false,
	}

	if targetID != nil {
		executionReq["target_id"] = *targetID
	}

	// Marshal request body
	body, err := json.Marshal(executionReq)
	if err != nil {
		return fmt.Errorf("marshal execution request: %w", err)
	}

	// Execute template via API
	resp, err := tw.client.Do(ctx, "POST", fmt.Sprintf("/api/v1/templates/%s/execute", templateID), body)
	if err != nil {
		return fmt.Errorf("execute template: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("execute template failed with status %d", resp.StatusCode)
	}

	// Read response
	var execution map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&execution); err != nil {
		return fmt.Errorf("decode execution response: %w", err)
	}

	tw.log.Info("template executed successfully",
		zap.String("template_id", templateID),
		zap.String("template_type", templateType),
		zap.String("target_type", targetType),
		zap.String("execution_id", execution["id"].(string)),
		zap.String("status", execution["status"].(string)))

	// Publish event
	tw.publishEvent("wizard.template.executed", map[string]any{
		"template_id":   templateID,
		"template_type": templateType,
		"target_type":   targetType,
		"execution_id":  execution["id"],
		"node_id":       tw.resolvedNodeID(),
	})

	return nil
}

// ListTemplates lists available templates with optional filtering
func (tw *TemplateWizard) ListTemplates(ctx context.Context, templateType string) ([]map[string]any, error) {
	if tw.client == nil {
		return nil, fmt.Errorf("api client unavailable")
	}

	// Build query parameters
	path := "/api/v1/templates"
	if templateType != "" {
		path = fmt.Sprintf("/api/v1/templates?type=%s", templateType)
	}

	// Make request
	resp, err := tw.client.Do(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list templates failed with status %d", resp.StatusCode)
	}

	// Read response
	var response struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode templates response: %w", err)
	}

	return response.Data, nil
}

// CreateTenantFromTemplate creates a new tenant using a config template
func (tw *TemplateWizard) CreateTenantFromTemplate(ctx context.Context, templateID string, tenantName string, parameters map[string]any) (string, error) {
	if tw.client == nil {
		return "", fmt.Errorf("api client unavailable")
	}

	// First create the tenant
	tenantReq := map[string]any{
		"name": tenantName,
	}

	body, err := json.Marshal(tenantReq)
	if err != nil {
		return "", fmt.Errorf("marshal tenant request: %w", err)
	}

	resp, err := tw.client.Do(ctx, "POST", "/api/v1/tenants", body)
	if err != nil {
		return "", fmt.Errorf("create tenant: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create tenant failed with status %d", resp.StatusCode)
	}

	var tenant map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&tenant); err != nil {
		return "", fmt.Errorf("decode tenant response: %w", err)
	}

	tenantID := tenant["id"].(string)

	// Then execute the template on the new tenant
	err = tw.ExecuteTemplate(ctx, templateID, "config", "tenant", &tenantID, parameters)
	if err != nil {
		// Clean up the tenant if template execution fails
		_, _ = tw.client.Do(ctx, "DELETE", fmt.Sprintf("/api/v1/tenants/%s", tenantID), nil)
		return "", fmt.Errorf("execute template for tenant: %w", err)
	}

	tw.log.Info("tenant created from template",
		zap.String("tenant_id", tenantID),
		zap.String("tenant_name", tenantName),
		zap.String("template_id", templateID))

	return tenantID, nil
}

// ProvisionNodeFromTemplate provisions a node using a job template
func (tw *TemplateWizard) ProvisionNodeFromTemplate(ctx context.Context, templateID string, nodeName string, parameters map[string]any) error {
	if tw.client == nil {
		return fmt.Errorf("api client unavailable")
	}

	// Create a target for the node (this could be a placeholder or actual node registration)
	targetID := nodeName // For now, use node name as target ID

	// Execute the provisioning template
	err := tw.ExecuteTemplate(ctx, templateID, "job", "node", &targetID, parameters)
	if err != nil {
		return fmt.Errorf("provision node from template: %w", err)
	}

	tw.log.Info("node provisioned from template",
		zap.String("node_name", nodeName),
		zap.String("template_id", templateID))

	return nil
}
