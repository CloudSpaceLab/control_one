package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ---- response types ---------------------------------------------------------

type dlpRuleResponse struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	PIIType   string `json:"pii_type"`
	Regex     string `json:"regex"`
	Severity  string `json:"severity"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

func newDLPRuleResponse(r storage.DataClassificationRule) dlpRuleResponse {
	return dlpRuleResponse{
		ID:        r.ID.String(),
		TenantID:  r.TenantID.String(),
		Name:      r.Name,
		PIIType:   r.PIIType,
		Regex:     r.Regex,
		Severity:  r.Severity,
		Enabled:   r.Enabled,
		CreatedAt: formatTime(r.CreatedAt),
	}
}

type columnClassificationResponse struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id"`
	NodeID          string  `json:"node_id"`
	DatabaseName    string  `json:"database_name"`
	SchemaName      string  `json:"schema_name"`
	TableName       string  `json:"table_name"`
	ColumnName      string  `json:"column_name"`
	PIIType         *string `json:"pii_type,omitempty"`
	Encrypted       *bool   `json:"encrypted,omitempty"`
	EncryptionKind  *string `json:"encryption_kind,omitempty"`
	MinValueLength  *int    `json:"min_value_length,omitempty"`
	MaxValueLength  *int    `json:"max_value_length,omitempty"`
	SampleCount     *int    `json:"sample_count,omitempty"`
	LastScannedAt   *string `json:"last_scanned_at,omitempty"`
}

func newColumnClassificationResponse(cc storage.ColumnClassification) columnClassificationResponse {
	out := columnClassificationResponse{
		ID:             cc.ID.String(),
		TenantID:       cc.TenantID.String(),
		NodeID:         cc.NodeID.String(),
		DatabaseName:   cc.DatabaseName,
		SchemaName:     cc.SchemaName,
		TableName:      cc.TableName,
		ColumnName:     cc.ColumnName,
		PIIType:        cc.PIIType,
		Encrypted:      cc.Encrypted,
		EncryptionKind: cc.EncryptionKind,
		MinValueLength: cc.MinValueLength,
		MaxValueLength: cc.MaxValueLength,
		SampleCount:    cc.SampleCount,
	}
	if cc.LastScannedAt != nil {
		s := formatTime(*cc.LastScannedAt)
		out.LastScannedAt = &s
	}
	return out
}

type piiFindingResponse struct {
	ID                     string  `json:"id"`
	TenantID               string  `json:"tenant_id"`
	ColumnClassificationID *string `json:"column_classification_id,omitempty"`
	RuleID                 *string `json:"rule_id,omitempty"`
	Severity               string  `json:"severity"`
	Details                *string `json:"details,omitempty"`
	ResolvedAt             *string `json:"resolved_at,omitempty"`
	ResolvedBy             *string `json:"resolved_by,omitempty"`
	CreatedAt              string  `json:"created_at"`
}

func newPIIFindingResponse(f storage.PIIFinding) piiFindingResponse {
	out := piiFindingResponse{
		ID:        f.ID.String(),
		TenantID:  f.TenantID.String(),
		Severity:  f.Severity,
		Details:   f.Details,
		CreatedAt: formatTime(f.CreatedAt),
	}
	if f.ColumnClassificationID != nil {
		s := f.ColumnClassificationID.String()
		out.ColumnClassificationID = &s
	}
	if f.RuleID != nil {
		s := f.RuleID.String()
		out.RuleID = &s
	}
	if f.ResolvedAt != nil {
		s := formatTime(*f.ResolvedAt)
		out.ResolvedAt = &s
	}
	if f.ResolvedBy != nil {
		s := f.ResolvedBy.String()
		out.ResolvedBy = &s
	}
	return out
}

// ---- DLP rules collection ---------------------------------------------------

func (s *Server) handleDLPRulesCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		tenantIDStr := r.URL.Query().Get("tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		rules, err := s.store.ListDataClassificationRules(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("list dlp rules", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		items := make([]dlpRuleResponse, 0, len(rules))
		for _, rule := range rules {
			items = append(items, newDLPRuleResponse(rule))
		}
		writeJSON(w, http.StatusOK, paginatedResponse[dlpRuleResponse]{
			Data:       items,
			Pagination: newPaginationMeta(len(items), len(items), 0, len(items)),
		})

	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator)
		if !ok {
			return
		}
		var req struct {
			TenantID string `json:"tenant_id"`
			Name     string `json:"name"`
			PIIType  string `json:"pii_type"`
			Regex    string `json:"regex"`
			Severity string `json:"severity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		tenantID, err := uuid.Parse(req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		rule := &storage.DataClassificationRule{
			TenantID: tenantID,
			Name:     req.Name,
			PIIType:  req.PIIType,
			Regex:    req.Regex,
			Severity: req.Severity,
			Enabled:  true,
		}
		created, err := s.store.CreateDataClassificationRule(r.Context(), rule)
		if err != nil {
			s.logger.Error("create dlp rule", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, tenantID, "dlp.rule.create", "dlp_rule", created.ID.String(), map[string]any{
			"name":     created.Name,
			"pii_type": created.PIIType,
		})
		writeJSON(w, http.StatusCreated, newDLPRuleResponse(*created))

	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ---- DLP rules resource (DELETE /api/v1/dlp/rules/:id) ---------------------

func (s *Server) handleDLPRulesResource(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/dlp/rules/")
	idStr = strings.TrimSuffix(idStr, "/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid rule id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		principal, ok := s.authorize(w, r, roleOperator)
		if !ok {
			return
		}
		if err := s.store.DeleteDataClassificationRule(r.Context(), id); err != nil {
			s.logger.Error("delete dlp rule", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, uuid.Nil, "dlp.rule.delete", "dlp_rule", id.String(), nil)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ---- DLP columns ------------------------------------------------------------

func (s *Server) handleDLPColumnsCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	tenantID, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cols, total, err := s.store.ListColumnClassifications(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Error("list column classifications", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]columnClassificationResponse, 0, len(cols))
	for _, cc := range cols {
		items = append(items, newColumnClassificationResponse(cc))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[columnClassificationResponse]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

// ---- DLP findings -----------------------------------------------------------

func (s *Server) handleDLPFindingsCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	tenantID, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	var resolved *bool
	if v := r.URL.Query().Get("resolved"); v != "" {
		b := v == "true"
		resolved = &b
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	findings, total, err := s.store.ListPIIFindings(r.Context(), tenantID, resolved, limit, offset)
	if err != nil {
		s.logger.Error("list pii findings", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]piiFindingResponse, 0, len(findings))
	for _, f := range findings {
		items = append(items, newPIIFindingResponse(f))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[piiFindingResponse]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

// ---- DLP findings resource (resolve) ----------------------------------------

func (s *Server) handleDLPFindingsResource(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/dlp/findings/")
	parts := strings.SplitN(rest, "/", 2)
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid finding id", http.StatusBadRequest)
		return
	}
	action := ""
	if len(parts) == 2 {
		action = strings.TrimSuffix(parts[1], "/")
	}
	switch action {
	case "resolve":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleOperator)
		if !ok {
			return
		}
		resolverID := s.userIDForPrincipalCtx(r.Context(), principal)
		if err := s.store.ResolvePIIFinding(r.Context(), id, resolverID); err != nil {
			s.logger.Error("resolve pii finding", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, uuid.Nil, "dlp.finding.resolve", "pii_finding", id.String(), nil)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// ---- DLP seed-rules ---------------------------------------------------------

func (s *Server) handleDLPSeedRules(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator)
	if !ok {
		return
	}

	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	// Fetch existing rules to avoid duplicates by name.
	existing, err := s.store.ListDataClassificationRules(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("list dlp rules for seed", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	existingNames := make(map[string]bool, len(existing))
	for _, e := range existing {
		existingNames[e.Name] = true
	}

	seeded := 0
	for _, tmpl := range DefaultDLPRules {
		if existingNames[tmpl.Name] {
			continue
		}
		rule := &storage.DataClassificationRule{
			TenantID: tenantID,
			Name:     tmpl.Name,
			PIIType:  tmpl.PIIType,
			Regex:    tmpl.Regex,
			Severity: tmpl.Severity,
			Enabled:  true,
		}
		if _, err := s.store.CreateDataClassificationRule(r.Context(), rule); err != nil {
			s.logger.Error("seed dlp rule", zap.String("name", tmpl.Name), zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		seeded++
	}

	s.recordAudit(r.Context(), principal, tenantID, "dlp.rules.seed", "dlp_rule", tenantID.String(), map[string]any{
		"seeded": seeded,
	})
	writeJSON(w, http.StatusOK, map[string]int{"seeded": seeded})
}
