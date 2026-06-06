package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

const JobTypePrivateAccessImport = "private_access.import"

type privateAccessProviderAccountStore interface {
	privateAccessStore
	UpsertPrivateAccessProviderAccount(context.Context, storage.UpsertPrivateAccessProviderAccountParams) (*storage.PrivateAccessProviderAccount, error)
	GetPrivateAccessProviderAccount(context.Context, uuid.UUID, uuid.UUID) (*storage.PrivateAccessProviderAccount, error)
	ListPrivateAccessProviderAccounts(context.Context, uuid.UUID, string, string, int, int) ([]storage.PrivateAccessProviderAccount, int, error)
	ListDuePrivateAccessProviderAccounts(context.Context, time.Time, int) ([]storage.PrivateAccessProviderAccount, error)
	RecordPrivateAccessProviderImportState(context.Context, uuid.UUID, uuid.UUID, string, string, time.Time, *time.Time) (*storage.PrivateAccessProviderAccount, error)
	CreatePrivateAccessImportRun(context.Context, storage.CreatePrivateAccessImportRunParams) (*storage.PrivateAccessImportRun, error)
	UpdatePrivateAccessImportRun(context.Context, uuid.UUID, storage.UpdatePrivateAccessImportRunParams) (*storage.PrivateAccessImportRun, error)
	ListPrivateAccessImportRuns(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.PrivateAccessImportRun, int, error)
}

type privateAccessProviderAccountRequest struct {
	Provider              string         `json:"provider"`
	AccountID             string         `json:"account_id"`
	DisplayName           string         `json:"display_name"`
	EndpointURL           string         `json:"endpoint_url"`
	CredentialID          *string        `json:"credential_id,omitempty"`
	Config                map[string]any `json:"config,omitempty"`
	ImportEnabled         bool           `json:"import_enabled"`
	ImportIntervalSeconds int            `json:"import_interval_seconds,omitempty"`
	NextImportAt          *string        `json:"next_import_at,omitempty"`
}

type privateAccessProviderAccountDTO struct {
	ID                    string         `json:"id"`
	TenantID              string         `json:"tenant_id"`
	Provider              string         `json:"provider"`
	AccountID             string         `json:"account_id"`
	DisplayName           string         `json:"display_name"`
	EndpointURL           string         `json:"endpoint_url,omitempty"`
	HasCredential         bool           `json:"has_credential"`
	Status                string         `json:"status"`
	Config                map[string]any `json:"config,omitempty"`
	ImportEnabled         bool           `json:"import_enabled"`
	ImportIntervalSeconds int            `json:"import_interval_seconds"`
	NextImportAt          *string        `json:"next_import_at,omitempty"`
	LastImportAt          *string        `json:"last_import_at,omitempty"`
	LastImportStatus      string         `json:"last_import_status,omitempty"`
	LastImportError       string         `json:"last_import_error,omitempty"`
	CreatedBySubject      string         `json:"created_by_subject,omitempty"`
	CreatedAt             string         `json:"created_at"`
	UpdatedAt             string         `json:"updated_at"`
}

type privateAccessImportRunDTO struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenant_id"`
	ProviderAccountID string         `json:"provider_account_id"`
	JobID             *string        `json:"job_id,omitempty"`
	Provider          string         `json:"provider"`
	AccountID         string         `json:"account_id"`
	Status            string         `json:"status"`
	Summary           map[string]any `json:"summary,omitempty"`
	Error             string         `json:"error,omitempty"`
	StartedAt         *string        `json:"started_at,omitempty"`
	FinishedAt        *string        `json:"finished_at,omitempty"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type privateAccessProviderImportRequest struct {
	Snapshot        *privateaccess.Snapshot    `json:"snapshot,omitempty"`
	ProviderPayload map[string]json.RawMessage `json:"provider_payload,omitempty"`
	Enqueue         *bool                      `json:"enqueue,omitempty"`
}

type privateAccessImportJobPayload struct {
	TenantID          string `json:"tenant_id"`
	ProviderAccountID string `json:"provider_account_id"`
	ImportRunID       string `json:"import_run_id,omitempty"`
}

func (s *Server) handlePrivateAccessProviderAccounts(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(privateAccessProviderAccountStore)
	if !ok {
		http.Error(w, "private access provider account store unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		limit, offset, err := parseLimitOffset(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		accounts, total, err := store.ListPrivateAccessProviderAccounts(
			r.Context(),
			tenantID,
			r.URL.Query().Get("provider"),
			r.URL.Query().Get("status"),
			limit,
			offset,
		)
		if err != nil {
			s.logger.Error("list private access provider accounts", zap.Error(err))
			http.Error(w, "list private access provider accounts", http.StatusInternalServerError)
			return
		}
		data := make([]privateAccessProviderAccountDTO, 0, len(accounts))
		for i := range accounts {
			data = append(data, privateAccessProviderAccountToDTO(&accounts[i]))
		}
		writeJSON(w, http.StatusOK, paginatedResponse[privateAccessProviderAccountDTO]{
			Data:       data,
			Pagination: paginationMeta{Total: total, Limit: limit, Offset: offset},
		})
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleAdmin)
		if !ok {
			return
		}
		var req privateAccessProviderAccountRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		params, ok := s.privateAccessProviderAccountParamsFromRequest(w, r, principal, tenantID, req)
		if !ok {
			return
		}
		account, err := store.UpsertPrivateAccessProviderAccount(r.Context(), params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.recordAudit(r.Context(), principal, tenantID, "private_access.provider_account.upserted", "private_access_provider_account", account.ID.String(), map[string]any{
			"provider":   account.Provider,
			"account_id": account.AccountID,
		})
		writeJSON(w, http.StatusCreated, privateAccessProviderAccountToDTO(account))
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePrivateAccessProviderAccountSubroutes(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(privateAccessProviderAccountStore)
	if !ok {
		http.Error(w, "private access provider account store unavailable", http.StatusServiceUnavailable)
		return
	}
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/private-access/provider-accounts/"), "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(trimmed, "/")
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid provider account id", http.StatusBadRequest)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		account, err := store.GetPrivateAccessProviderAccount(r.Context(), tenantID, id)
		if err != nil {
			http.Error(w, "get private access provider account", http.StatusInternalServerError)
			return
		}
		if account == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, privateAccessProviderAccountToDTO(account))
		return
	}
	if len(parts) == 2 && parts[1] == "import" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleOperator, roleAdmin)
		if !ok {
			return
		}
		s.handlePrivateAccessProviderAccountImport(w, r, store, principal, tenantID, id)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handlePrivateAccessImportRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	store, ok := s.store.(privateAccessProviderAccountStore)
	if !ok {
		http.Error(w, "private access provider account store unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var accountID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("provider_account_id")); raw != "" {
		accountID, err = uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid provider_account_id", http.StatusBadRequest)
			return
		}
	}
	runs, total, err := store.ListPrivateAccessImportRuns(r.Context(), tenantID, accountID, limit, offset)
	if err != nil {
		http.Error(w, "list private access import runs", http.StatusInternalServerError)
		return
	}
	data := make([]privateAccessImportRunDTO, 0, len(runs))
	for i := range runs {
		data = append(data, privateAccessImportRunToDTO(&runs[i]))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[privateAccessImportRunDTO]{
		Data:       data,
		Pagination: paginationMeta{Total: total, Limit: limit, Offset: offset},
	})
}

func (s *Server) handlePrivateAccessProviderAccountImport(w http.ResponseWriter, r *http.Request, store privateAccessProviderAccountStore, principal *auth.Principal, tenantID, accountID uuid.UUID) {
	account, err := store.GetPrivateAccessProviderAccount(r.Context(), tenantID, accountID)
	if err != nil {
		http.Error(w, "get private access provider account", http.StatusInternalServerError)
		return
	}
	if account == nil {
		http.NotFound(w, r)
		return
	}
	var req privateAccessProviderImportRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if r.Body != nil {
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}
	if req.Snapshot != nil || len(req.ProviderPayload) > 0 {
		run, snapshot, summary, err := s.storePrivateAccessManualImport(r.Context(), store, account, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.recordAudit(r.Context(), principal, tenantID, "private_access.import.completed", "private_access_provider_account", account.ID.String(), map[string]any{
			"provider":   account.Provider,
			"account_id": account.AccountID,
			"run_id":     run.ID.String(),
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"snapshot": snapshot,
			"summary":  summary,
			"run":      privateAccessImportRunToDTO(run),
		})
		return
	}
	enqueue := true
	if req.Enqueue != nil {
		enqueue = *req.Enqueue
	}
	if !enqueue {
		run, snapshot, summary, err := s.runPrivateAccessHTTPImport(r.Context(), store, account, uuid.Nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"snapshot": snapshot,
			"summary":  summary,
			"run":      privateAccessImportRunToDTO(run),
		})
		return
	}
	run, job, err := s.enqueuePrivateAccessImportJob(r.Context(), store, account)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "private_access.import.queued", "private_access_provider_account", account.ID.String(), map[string]any{
		"provider":   account.Provider,
		"account_id": account.AccountID,
		"job_id":     job.ID.String(),
		"run_id":     run.ID.String(),
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id": job.ID.String(),
		"run":    privateAccessImportRunToDTO(run),
	})
}

func (s *Server) privateAccessProviderAccountParamsFromRequest(w http.ResponseWriter, r *http.Request, principal *auth.Principal, tenantID uuid.UUID, req privateAccessProviderAccountRequest) (storage.UpsertPrivateAccessProviderAccountParams, bool) {
	provider := privateaccess.ProviderKind(strings.ToLower(strings.TrimSpace(req.Provider)))
	if !privateaccess.ValidProvider(provider) {
		http.Error(w, "provider must be one of netbird|headscale|openziti", http.StatusBadRequest)
		return storage.UpsertPrivateAccessProviderAccountParams{}, false
	}
	var credentialID *uuid.UUID
	if req.CredentialID != nil && strings.TrimSpace(*req.CredentialID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*req.CredentialID))
		if err != nil {
			http.Error(w, "invalid credential_id", http.StatusBadRequest)
			return storage.UpsertPrivateAccessProviderAccountParams{}, false
		}
		credentialID = &parsed
		if !s.validatePrivateAccessCredentialReference(w, r, tenantID, provider, *credentialID) {
			return storage.UpsertPrivateAccessProviderAccountParams{}, false
		}
	}
	if req.ImportEnabled {
		if strings.TrimSpace(req.EndpointURL) == "" {
			http.Error(w, "endpoint_url is required when import_enabled is true", http.StatusBadRequest)
			return storage.UpsertPrivateAccessProviderAccountParams{}, false
		}
		if credentialID == nil {
			http.Error(w, "credential_id is required when import_enabled is true", http.StatusBadRequest)
			return storage.UpsertPrivateAccessProviderAccountParams{}, false
		}
	}
	var nextImportAt *time.Time
	if req.NextImportAt != nil && strings.TrimSpace(*req.NextImportAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.NextImportAt))
		if err != nil {
			http.Error(w, "next_import_at must be RFC3339", http.StatusBadRequest)
			return storage.UpsertPrivateAccessProviderAccountParams{}, false
		}
		nextImportAt = &parsed
	}
	return storage.UpsertPrivateAccessProviderAccountParams{
		TenantID:              tenantID,
		Provider:              provider,
		AccountID:             req.AccountID,
		DisplayName:           req.DisplayName,
		EndpointURL:           req.EndpointURL,
		CredentialID:          credentialID,
		Config:                req.Config,
		ImportEnabled:         req.ImportEnabled,
		ImportIntervalSeconds: req.ImportIntervalSeconds,
		NextImportAt:          nextImportAt,
		CreatedBySubject:      principal.Subject,
	}, true
}

func (s *Server) validatePrivateAccessCredentialReference(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, provider privateaccess.ProviderKind, credentialID uuid.UUID) bool {
	cred, err := s.store.GetProviderCredential(r.Context(), credentialID)
	if err != nil {
		s.logger.Error("lookup provider credential for private access", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return false
	}
	if cred == nil || cred.TenantID != tenantID {
		http.Error(w, "credential_id not found for tenant", http.StatusBadRequest)
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(cred.Provider), string(provider)) {
		http.Error(w, "credential provider does not match private-access provider", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) storePrivateAccessManualImport(ctx context.Context, store privateAccessProviderAccountStore, account *storage.PrivateAccessProviderAccount, req privateAccessProviderImportRequest) (*storage.PrivateAccessImportRun, privateaccess.Snapshot, privateaccess.ImportSummary, error) {
	now := time.Now().UTC()
	run, err := store.CreatePrivateAccessImportRun(ctx, storage.CreatePrivateAccessImportRunParams{
		TenantID:          account.TenantID,
		ProviderAccountID: account.ID,
		Provider:          account.Provider,
		AccountID:         account.AccountID,
		Status:            storage.PrivateAccessImportStatusRunning,
		StartedAt:         &now,
	})
	if err != nil {
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
	}
	var snapshot privateaccess.Snapshot
	var summary privateaccess.ImportSummary
	if req.Snapshot != nil {
		snapshot = *req.Snapshot
		snapshot.Provider = account.Provider
		snapshot.AccountID = account.AccountID
		if snapshot.CollectedAt.IsZero() {
			snapshot.CollectedAt = now
		}
		summary = privateAccessSummaryForSnapshot(snapshot)
	} else {
		var err error
		snapshot, summary, err = privateaccess.SnapshotFromProviderPayload(account.Provider, account.AccountID, req.ProviderPayload, now)
		if err != nil {
			_, _ = store.UpdatePrivateAccessImportRun(ctx, run.ID, storage.UpdatePrivateAccessImportRunParams{
				Status:     storage.PrivateAccessImportStatusFailed,
				Error:      err.Error(),
				FinishedAt: &now,
			})
			return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
		}
	}
	return s.completePrivateAccessImport(ctx, store, account, run.ID, snapshot, summary)
}

func (s *Server) runPrivateAccessHTTPImport(ctx context.Context, store privateAccessProviderAccountStore, account *storage.PrivateAccessProviderAccount, importRunID uuid.UUID) (*storage.PrivateAccessImportRun, privateaccess.Snapshot, privateaccess.ImportSummary, error) {
	now := time.Now().UTC()
	var run *storage.PrivateAccessImportRun
	var err error
	if importRunID != uuid.Nil {
		run, err = store.UpdatePrivateAccessImportRun(ctx, importRunID, storage.UpdatePrivateAccessImportRunParams{
			Status:    storage.PrivateAccessImportStatusRunning,
			StartedAt: &now,
		})
	} else {
		run, err = store.CreatePrivateAccessImportRun(ctx, storage.CreatePrivateAccessImportRunParams{
			TenantID:          account.TenantID,
			ProviderAccountID: account.ID,
			Provider:          account.Provider,
			AccountID:         account.AccountID,
			Status:            storage.PrivateAccessImportStatusRunning,
			StartedAt:         &now,
		})
	}
	if err != nil {
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
	}
	if run == nil {
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, errors.New("private access import run not found")
	}
	cfg, err := s.privateAccessHTTPImportConfig(ctx, account)
	if err != nil {
		_, _ = store.UpdatePrivateAccessImportRun(ctx, run.ID, storage.UpdatePrivateAccessImportRunParams{Status: storage.PrivateAccessImportStatusFailed, Error: err.Error(), FinishedAt: &now})
		_, _ = store.RecordPrivateAccessProviderImportState(ctx, account.TenantID, account.ID, storage.PrivateAccessImportStatusFailed, err.Error(), now, nextPrivateAccessImportTime(account, now))
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
	}
	snapshot, summary, err := privateaccess.FetchSnapshot(ctx, cfg)
	if err != nil {
		_, _ = store.UpdatePrivateAccessImportRun(ctx, run.ID, storage.UpdatePrivateAccessImportRunParams{Status: storage.PrivateAccessImportStatusFailed, Error: err.Error(), FinishedAt: &now})
		_, _ = store.RecordPrivateAccessProviderImportState(ctx, account.TenantID, account.ID, storage.PrivateAccessImportStatusFailed, err.Error(), now, nextPrivateAccessImportTime(account, now))
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
	}
	return s.completePrivateAccessImport(ctx, store, account, run.ID, snapshot, summary)
}

func (s *Server) completePrivateAccessImport(ctx context.Context, store privateAccessProviderAccountStore, account *storage.PrivateAccessProviderAccount, runID uuid.UUID, snapshot privateaccess.Snapshot, summary privateaccess.ImportSummary) (*storage.PrivateAccessImportRun, privateaccess.Snapshot, privateaccess.ImportSummary, error) {
	now := time.Now().UTC()
	record, err := store.UpsertPrivateAccessSnapshot(ctx, account.TenantID, snapshot)
	if err != nil {
		_, _ = store.UpdatePrivateAccessImportRun(ctx, runID, storage.UpdatePrivateAccessImportRunParams{Status: storage.PrivateAccessImportStatusFailed, Error: err.Error(), FinishedAt: &now})
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
	}
	findings, reconcileErr := s.reconcilePrivateAccessExposure(ctx, store, account.TenantID)
	summaryMap := privateAccessImportSummaryMap(summary)
	summaryMap["snapshot_id"] = record.ID.String()
	if reconcileErr == nil {
		summaryMap["exposure_findings"] = findings
	}
	if reconcileErr != nil {
		summaryMap["exposure_reconcile_error"] = reconcileErr.Error()
	}
	run, err := store.UpdatePrivateAccessImportRun(ctx, runID, storage.UpdatePrivateAccessImportRunParams{
		Status:     storage.PrivateAccessImportStatusSucceeded,
		Summary:    summaryMap,
		FinishedAt: &now,
	})
	if err != nil {
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
	}
	if _, err := store.RecordPrivateAccessProviderImportState(ctx, account.TenantID, account.ID, storage.PrivateAccessImportStatusSucceeded, "", now, nextPrivateAccessImportTime(account, now)); err != nil {
		return nil, privateaccess.Snapshot{}, privateaccess.ImportSummary{}, err
	}
	return run, snapshot, summary, nil
}

func (s *Server) enqueuePrivateAccessImportJob(ctx context.Context, store privateAccessProviderAccountStore, account *storage.PrivateAccessProviderAccount) (*storage.PrivateAccessImportRun, *storage.Job, error) {
	now := time.Now().UTC()
	run, err := store.CreatePrivateAccessImportRun(ctx, storage.CreatePrivateAccessImportRunParams{
		TenantID:          account.TenantID,
		ProviderAccountID: account.ID,
		Provider:          account.Provider,
		AccountID:         account.AccountID,
		Status:            storage.PrivateAccessImportStatusQueued,
	})
	if err != nil {
		return nil, nil, err
	}
	payload, err := json.Marshal(privateAccessImportJobPayload{
		TenantID:          account.TenantID.String(),
		ProviderAccountID: account.ID.String(),
		ImportRunID:       run.ID.String(),
	})
	if err != nil {
		return nil, nil, err
	}
	job, err := s.store.CreateJob(ctx, &storage.Job{
		TenantID:   account.TenantID,
		Type:       JobTypePrivateAccessImport,
		Status:     storage.JobStatusQueued,
		Payload:    payload,
		MaxRetries: 3,
	}, &storage.JobEvent{Status: storage.JobStatusQueued, Message: "private access import queued"})
	if err != nil {
		return nil, nil, err
	}
	run, err = store.UpdatePrivateAccessImportRun(ctx, run.ID, storage.UpdatePrivateAccessImportRunParams{
		JobID:  &job.ID,
		Status: storage.PrivateAccessImportStatusQueued,
	})
	if err != nil {
		return nil, nil, err
	}
	if _, err := store.RecordPrivateAccessProviderImportState(ctx, account.TenantID, account.ID, storage.PrivateAccessImportStatusQueued, "", now, nextPrivateAccessImportTime(account, now)); err != nil {
		return nil, nil, err
	}
	if s.worker != nil {
		task := s.durableJobTask(job, fmt.Sprintf("private-access-import-%s", job.ID))
		if err := s.worker.Enqueue(task); err != nil {
			_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, fmt.Sprintf("enqueue failed: %v", err), map[string]any{"finished_at": time.Now()})
			_, _ = store.UpdatePrivateAccessImportRun(ctx, run.ID, storage.UpdatePrivateAccessImportRunParams{Status: storage.PrivateAccessImportStatusFailed, Error: err.Error(), FinishedAt: &now})
			return nil, nil, err
		}
	} else {
		go func(jobID uuid.UUID, jobType string) {
			_ = s.buildJobExecution(jobID, jobType, 3)(context.Background())
		}(job.ID, job.Type)
	}
	return run, job, nil
}

func (s *Server) handlePrivateAccessImportJob(ctx context.Context, job *storage.Job) error {
	if job == nil {
		return errors.New("job is required")
	}
	store, ok := s.store.(privateAccessProviderAccountStore)
	if !ok {
		return errors.New("private access provider account store unavailable")
	}
	payload, err := decodePrivateAccessImportPayload(job.Payload)
	if err != nil {
		return err
	}
	account, err := store.GetPrivateAccessProviderAccount(ctx, payload.tenantID, payload.providerAccountID)
	if err != nil {
		return err
	}
	if account == nil {
		return errors.New("private access provider account not found")
	}
	_, _, _, err = s.runPrivateAccessHTTPImport(ctx, store, account, payload.importRunID)
	return err
}

func decodePrivateAccessImportPayload(raw json.RawMessage) (struct {
	tenantID          uuid.UUID
	providerAccountID uuid.UUID
	importRunID       uuid.UUID
}, error) {
	var body privateAccessImportJobPayload
	if err := json.Unmarshal(raw, &body); err != nil {
		return struct {
			tenantID          uuid.UUID
			providerAccountID uuid.UUID
			importRunID       uuid.UUID
		}{}, fmt.Errorf("decode private access import payload: %w", err)
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(body.TenantID))
	if err != nil {
		return struct {
			tenantID          uuid.UUID
			providerAccountID uuid.UUID
			importRunID       uuid.UUID
		}{}, errors.New("tenant_id is required")
	}
	accountID, err := uuid.Parse(strings.TrimSpace(body.ProviderAccountID))
	if err != nil {
		return struct {
			tenantID          uuid.UUID
			providerAccountID uuid.UUID
			importRunID       uuid.UUID
		}{}, errors.New("provider_account_id is required")
	}
	var importRunID uuid.UUID
	if strings.TrimSpace(body.ImportRunID) != "" {
		importRunID, err = uuid.Parse(strings.TrimSpace(body.ImportRunID))
		if err != nil {
			return struct {
				tenantID          uuid.UUID
				providerAccountID uuid.UUID
				importRunID       uuid.UUID
			}{}, errors.New("import_run_id must be a valid UUID")
		}
	}
	return struct {
		tenantID          uuid.UUID
		providerAccountID uuid.UUID
		importRunID       uuid.UUID
	}{tenantID: tenantID, providerAccountID: accountID, importRunID: importRunID}, nil
}

func (s *Server) privateAccessHTTPImportConfig(ctx context.Context, account *storage.PrivateAccessProviderAccount) (privateaccess.HTTPImportConfig, error) {
	config := cloneMapStringAny(account.Config)
	var credentialConfig map[string]any
	if account.CredentialID.Valid {
		cred, err := s.store.GetProviderCredential(ctx, account.CredentialID.UUID)
		if err != nil {
			return privateaccess.HTTPImportConfig{}, err
		}
		if cred == nil || cred.TenantID != account.TenantID {
			return privateaccess.HTTPImportConfig{}, errors.New("credential_id references missing credential")
		}
		if !strings.EqualFold(strings.TrimSpace(cred.Provider), string(account.Provider)) {
			return privateaccess.HTTPImportConfig{}, errors.New("credential provider does not match private-access provider")
		}
		credentialConfig, err = s.openProviderCredential(cred)
		if err != nil {
			return privateaccess.HTTPImportConfig{}, err
		}
	}
	baseURL := firstMapString(account.EndpointURL, config, credentialConfig, "base_url", "endpoint_url", "url")
	if strings.TrimSpace(baseURL) == "" {
		return privateaccess.HTTPImportConfig{}, errors.New("endpoint_url or credential base_url is required")
	}
	return privateaccess.HTTPImportConfig{
		Provider:      account.Provider,
		AccountID:     account.AccountID,
		BaseURL:       baseURL,
		Token:         firstMapString("", credentialConfig, config, "token", "api_token", "api_key", "bearer_token"),
		Authorization: firstMapString("", credentialConfig, config, "authorization", "authorization_header"),
		Endpoints:     privateAccessEndpointMap(config, credentialConfig),
		Timeout:       privateAccessTimeout(config, credentialConfig),
		SkipTLSVerify: firstMapBool(false, config, credentialConfig, "skip_tls_verify", "tls_skip_verify"),
	}, nil
}

func (s *Server) reconcilePrivateAccessExposure(ctx context.Context, store privateAccessProviderAccountStore, tenantID uuid.UUID) (int, error) {
	services, err := store.ListNodeServicesForTenant(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	snapshots, err := store.ListPrivateAccessSnapshots(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	exposureContext, err := loadPrivateAccessExposureContext(ctx, store, tenantID)
	if err != nil {
		return 0, err
	}
	models := make([]privateaccess.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		models = append(models, snapshot.Snapshot)
	}
	now := time.Now().UTC()
	exposureContext.Now = now
	observations := privateAccessObservationsFromNodeServicesWithContext(services, exposureContext)
	findings := privateaccess.ReconcileExposure(observations, models, privateaccess.ReconcileOptions{Now: now})
	records, err := store.ReplacePrivateAccessExposureFindings(ctx, tenantID, findings, now)
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

func privateAccessProviderAccountToDTO(account *storage.PrivateAccessProviderAccount) privateAccessProviderAccountDTO {
	out := privateAccessProviderAccountDTO{
		ID:                    account.ID.String(),
		TenantID:              account.TenantID.String(),
		Provider:              string(account.Provider),
		AccountID:             account.AccountID,
		DisplayName:           account.DisplayName,
		EndpointURL:           account.EndpointURL,
		HasCredential:         account.CredentialID.Valid,
		Status:                account.Status,
		Config:                redactPrivateAccessConfig(account.Config),
		ImportEnabled:         account.ImportEnabled,
		ImportIntervalSeconds: account.ImportIntervalSeconds,
		LastImportStatus:      account.LastImportStatus,
		LastImportError:       account.LastImportError,
		CreatedBySubject:      account.CreatedBySubject,
		CreatedAt:             account.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:             account.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if account.NextImportAt.Valid {
		value := account.NextImportAt.Time.UTC().Format(time.RFC3339)
		out.NextImportAt = &value
	}
	if account.LastImportAt.Valid {
		value := account.LastImportAt.Time.UTC().Format(time.RFC3339)
		out.LastImportAt = &value
	}
	return out
}

func privateAccessImportRunToDTO(run *storage.PrivateAccessImportRun) privateAccessImportRunDTO {
	out := privateAccessImportRunDTO{
		ID:                run.ID.String(),
		TenantID:          run.TenantID.String(),
		ProviderAccountID: run.ProviderAccountID.String(),
		Provider:          string(run.Provider),
		AccountID:         run.AccountID,
		Status:            run.Status,
		Summary:           cloneMapStringAny(run.Summary),
		Error:             run.Error,
		CreatedAt:         run.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         run.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if run.JobID.Valid {
		value := run.JobID.UUID.String()
		out.JobID = &value
	}
	if run.StartedAt.Valid {
		value := run.StartedAt.Time.UTC().Format(time.RFC3339)
		out.StartedAt = &value
	}
	if run.FinishedAt.Valid {
		value := run.FinishedAt.Time.UTC().Format(time.RFC3339)
		out.FinishedAt = &value
	}
	return out
}

func privateAccessImportSummaryMap(summary privateaccess.ImportSummary) map[string]any {
	return map[string]any{
		"provider":         string(summary.Provider),
		"account_id":       summary.AccountID,
		"collected_at":     summary.CollectedAt.UTC().Format(time.RFC3339),
		"peers":            summary.Peers,
		"groups":           summary.Groups,
		"policies":         summary.Policies,
		"routes":           summary.Routes,
		"services":         summary.Services,
		"connector_health": summary.ConnectorHealth,
		"audit_events":     summary.AuditEvents,
	}
}

func privateAccessSummaryForSnapshot(snapshot privateaccess.Snapshot) privateaccess.ImportSummary {
	return privateaccess.ImportSummary{
		Provider:        snapshot.Provider,
		AccountID:       snapshot.AccountID,
		CollectedAt:     snapshot.CollectedAt,
		Peers:           len(snapshot.Peers),
		Groups:          len(snapshot.Groups),
		Policies:        len(snapshot.Policies),
		Routes:          len(snapshot.Routes),
		Services:        len(snapshot.Services),
		ConnectorHealth: len(snapshot.ConnectorHealth),
		AuditEvents:     len(snapshot.AuditEvents),
	}
}

func nextPrivateAccessImportTime(account *storage.PrivateAccessProviderAccount, now time.Time) *time.Time {
	if account == nil || !account.ImportEnabled {
		return nil
	}
	seconds := account.ImportIntervalSeconds
	if seconds <= 0 {
		seconds = 3600
	}
	next := now.UTC().Add(time.Duration(seconds) * time.Second)
	return &next
}

func redactPrivateAccessConfig(in map[string]any) map[string]any {
	out := cloneMapStringAny(in)
	for key, value := range out {
		lower := strings.ToLower(strings.TrimSpace(key))
		if strings.HasSuffix(lower, "_ref") {
			out[key] = "configured"
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			out[key] = redactPrivateAccessConfig(nested)
		}
	}
	return out
}

func cloneMapStringAny(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneMapStringAny(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func firstMapString(fallback string, primary, secondary map[string]any, keys ...string) string {
	for _, source := range []map[string]any{primary, secondary} {
		for _, key := range keys {
			if value, ok := source[key]; ok {
				text := strings.TrimSpace(fmt.Sprint(value))
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
	}
	return fallback
}

func firstMapBool(fallback bool, primary, secondary map[string]any, keys ...string) bool {
	for _, source := range []map[string]any{primary, secondary} {
		for _, key := range keys {
			value, ok := source[key]
			if !ok {
				continue
			}
			switch typed := value.(type) {
			case bool:
				return typed
			case string:
				switch strings.ToLower(strings.TrimSpace(typed)) {
				case "true", "1", "yes":
					return true
				case "false", "0", "no":
					return false
				}
			case float64:
				return typed != 0
			case int:
				return typed != 0
			}
		}
	}
	return fallback
}

func privateAccessEndpointMap(maps ...map[string]any) map[string]string {
	out := map[string]string{}
	for _, source := range maps {
		if source == nil {
			continue
		}
		raw, ok := source["endpoints"]
		if !ok {
			continue
		}
		switch typed := raw.(type) {
		case map[string]any:
			for key, value := range typed {
				if text := strings.TrimSpace(fmt.Sprint(value)); key != "" && text != "" {
					out[key] = text
				}
			}
		case map[string]string:
			for key, value := range typed {
				if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
					out[key] = strings.TrimSpace(value)
				}
			}
		}
	}
	return out
}

func privateAccessTimeout(maps ...map[string]any) time.Duration {
	for _, source := range maps {
		for _, key := range []string{"timeout_seconds", "http_timeout_seconds"} {
			if value, ok := source[key]; ok {
				switch typed := value.(type) {
				case float64:
					if typed > 0 {
						return time.Duration(typed) * time.Second
					}
				case int:
					if typed > 0 {
						return time.Duration(typed) * time.Second
					}
				case string:
					if seconds, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil && seconds > 0 {
						return time.Duration(seconds) * time.Second
					}
					if duration, err := time.ParseDuration(strings.TrimSpace(typed)); err == nil && duration > 0 {
						return duration
					}
				}
			}
		}
	}
	return 30 * time.Second
}
