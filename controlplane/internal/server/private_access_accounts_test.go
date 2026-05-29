package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

type privateAccessAccountTestStore struct {
	*fakeStore
	accounts  map[uuid.UUID]storage.PrivateAccessProviderAccount
	snapshots []storage.PrivateAccessSnapshotRecord
	runs      map[uuid.UUID]storage.PrivateAccessImportRun
}

func newPrivateAccessAccountTestStore() *privateAccessAccountTestStore {
	return &privateAccessAccountTestStore{
		fakeStore: &fakeStore{},
		accounts:  map[uuid.UUID]storage.PrivateAccessProviderAccount{},
		runs:      map[uuid.UUID]storage.PrivateAccessImportRun{},
	}
}

func (s *privateAccessAccountTestStore) UpsertPrivateAccessProviderAccount(_ context.Context, p storage.UpsertPrivateAccessProviderAccountParams) (*storage.PrivateAccessProviderAccount, error) {
	now := time.Now().UTC()
	for id, account := range s.accounts {
		if account.TenantID == p.TenantID && account.Provider == p.Provider && account.AccountID == strings.TrimSpace(p.AccountID) {
			account.DisplayName = p.DisplayName
			account.EndpointURL = p.EndpointURL
			account.Config = p.Config
			account.ImportEnabled = p.ImportEnabled
			account.ImportIntervalSeconds = p.ImportIntervalSeconds
			account.UpdatedAt = now
			s.accounts[id] = account
			return &account, nil
		}
	}
	id := uuid.New()
	accountID := strings.TrimSpace(p.AccountID)
	if accountID == "" {
		accountID = "default"
	}
	display := strings.TrimSpace(p.DisplayName)
	if display == "" {
		display = string(p.Provider) + ":" + accountID
	}
	interval := p.ImportIntervalSeconds
	if interval <= 0 {
		interval = 3600
	}
	account := storage.PrivateAccessProviderAccount{
		ID:                    id,
		TenantID:              p.TenantID,
		Provider:              p.Provider,
		AccountID:             accountID,
		DisplayName:           display,
		EndpointURL:           p.EndpointURL,
		Status:                storage.PrivateAccessAccountStatusActive,
		Config:                p.Config,
		ImportEnabled:         p.ImportEnabled,
		ImportIntervalSeconds: interval,
		CreatedBySubject:      p.CreatedBySubject,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if p.CredentialID != nil {
		account.CredentialID.Valid = true
		account.CredentialID.UUID = *p.CredentialID
	}
	s.accounts[id] = account
	return &account, nil
}

func (s *privateAccessAccountTestStore) GetPrivateAccessProviderAccount(_ context.Context, tenantID, id uuid.UUID) (*storage.PrivateAccessProviderAccount, error) {
	account, ok := s.accounts[id]
	if !ok || account.TenantID != tenantID {
		return nil, nil
	}
	return &account, nil
}

func (s *privateAccessAccountTestStore) ListPrivateAccessProviderAccounts(_ context.Context, tenantID uuid.UUID, provider, status string, limit, offset int) ([]storage.PrivateAccessProviderAccount, int, error) {
	var out []storage.PrivateAccessProviderAccount
	for _, account := range s.accounts {
		if account.TenantID != tenantID {
			continue
		}
		if provider != "" && string(account.Provider) != provider {
			continue
		}
		if status != "" && account.Status != status {
			continue
		}
		out = append(out, account)
	}
	total := len(out)
	if offset > total {
		return []storage.PrivateAccessProviderAccount{}, total, nil
	}
	if offset > 0 {
		out = out[offset:]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (s *privateAccessAccountTestStore) ListDuePrivateAccessProviderAccounts(_ context.Context, _ time.Time, _ int) ([]storage.PrivateAccessProviderAccount, error) {
	return nil, nil
}

func (s *privateAccessAccountTestStore) RecordPrivateAccessProviderImportState(_ context.Context, tenantID, id uuid.UUID, status, message string, importedAt time.Time, nextImportAt *time.Time) (*storage.PrivateAccessProviderAccount, error) {
	account, ok := s.accounts[id]
	if !ok || account.TenantID != tenantID {
		return nil, nil
	}
	account.LastImportStatus = status
	account.LastImportError = message
	account.LastImportAt.Valid = true
	account.LastImportAt.Time = importedAt
	if nextImportAt != nil {
		account.NextImportAt.Valid = true
		account.NextImportAt.Time = *nextImportAt
	}
	s.accounts[id] = account
	return &account, nil
}

func (s *privateAccessAccountTestStore) CreatePrivateAccessImportRun(_ context.Context, p storage.CreatePrivateAccessImportRunParams) (*storage.PrivateAccessImportRun, error) {
	now := time.Now().UTC()
	run := storage.PrivateAccessImportRun{
		ID:                uuid.New(),
		TenantID:          p.TenantID,
		ProviderAccountID: p.ProviderAccountID,
		Provider:          p.Provider,
		AccountID:         p.AccountID,
		Status:            p.Status,
		Summary:           p.Summary,
		Error:             p.Error,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if p.JobID != nil {
		run.JobID.Valid = true
		run.JobID.UUID = *p.JobID
	}
	if p.StartedAt != nil {
		run.StartedAt.Valid = true
		run.StartedAt.Time = *p.StartedAt
	}
	if p.FinishedAt != nil {
		run.FinishedAt.Valid = true
		run.FinishedAt.Time = *p.FinishedAt
	}
	s.runs[run.ID] = run
	return &run, nil
}

func (s *privateAccessAccountTestStore) UpdatePrivateAccessImportRun(_ context.Context, id uuid.UUID, p storage.UpdatePrivateAccessImportRunParams) (*storage.PrivateAccessImportRun, error) {
	run, ok := s.runs[id]
	if !ok {
		return nil, nil
	}
	if p.JobID != nil {
		run.JobID.Valid = true
		run.JobID.UUID = *p.JobID
	}
	if strings.TrimSpace(p.Status) != "" {
		run.Status = p.Status
	}
	if p.Summary != nil {
		run.Summary = p.Summary
	}
	if strings.TrimSpace(p.Error) != "" {
		run.Error = p.Error
	}
	if p.StartedAt != nil {
		run.StartedAt.Valid = true
		run.StartedAt.Time = *p.StartedAt
	}
	if p.FinishedAt != nil {
		run.FinishedAt.Valid = true
		run.FinishedAt.Time = *p.FinishedAt
	}
	run.UpdatedAt = time.Now().UTC()
	s.runs[id] = run
	return &run, nil
}

func (s *privateAccessAccountTestStore) ListPrivateAccessImportRuns(_ context.Context, tenantID uuid.UUID, providerAccountID uuid.UUID, limit, offset int) ([]storage.PrivateAccessImportRun, int, error) {
	var out []storage.PrivateAccessImportRun
	for _, run := range s.runs {
		if run.TenantID != tenantID {
			continue
		}
		if providerAccountID != uuid.Nil && run.ProviderAccountID != providerAccountID {
			continue
		}
		out = append(out, run)
	}
	total := len(out)
	if offset > total {
		return []storage.PrivateAccessImportRun{}, total, nil
	}
	if offset > 0 {
		out = out[offset:]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (s *privateAccessAccountTestStore) UpsertPrivateAccessSnapshot(_ context.Context, tenantID uuid.UUID, snapshot privateaccess.Snapshot) (*storage.PrivateAccessSnapshotRecord, error) {
	now := time.Now().UTC()
	record := storage.PrivateAccessSnapshotRecord{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Provider:    snapshot.Provider,
		AccountID:   snapshot.AccountID,
		CollectedAt: snapshot.CollectedAt,
		Snapshot:    snapshot,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.snapshots = append(s.snapshots, record)
	return &record, nil
}

func (s *privateAccessAccountTestStore) ListPrivateAccessSnapshots(_ context.Context, tenantID uuid.UUID) ([]storage.PrivateAccessSnapshotRecord, error) {
	var out []storage.PrivateAccessSnapshotRecord
	for _, snapshot := range s.snapshots {
		if snapshot.TenantID == tenantID {
			out = append(out, snapshot)
		}
	}
	return out, nil
}

func (s *privateAccessAccountTestStore) ReplacePrivateAccessExposureFindings(_ context.Context, tenantID uuid.UUID, findings []privateaccess.ExposureFinding, observedAt time.Time) ([]storage.PrivateAccessExposureFindingRecord, error) {
	out := make([]storage.PrivateAccessExposureFindingRecord, 0, len(findings))
	for _, finding := range findings {
		out = append(out, storage.PrivateAccessExposureFindingRecord{
			ID:         uuid.New(),
			TenantID:   tenantID,
			Provider:   finding.Provider,
			Type:       finding.Type,
			Severity:   finding.Severity,
			Detail:     finding.Detail,
			Evidence:   finding.Evidence,
			ObservedAt: observedAt,
		})
	}
	return out, nil
}

func (s *privateAccessAccountTestStore) ListPrivateAccessExposureFindings(_ context.Context, _ uuid.UUID, _ bool, _, _ int) ([]storage.PrivateAccessExposureFindingRecord, error) {
	return nil, nil
}

func TestPrivateAccessProviderAccountsAPIManualImport(t *testing.T) {
	tenantID := uuid.New()
	store := newPrivateAccessAccountTestStore()
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "admin-token", "operator", "operator-token", "viewer", "viewer-token"),
	}, store, &stubQueue{})
	handler := srv.Handler()

	createBody := `{
		"provider":"netbird",
		"account_id":"prod",
		"display_name":"Bank NetBird",
		"config":{"token_ref":"vault://tenant/private-access/netbird"}
	}`
	rec := callPrivateAccessAPI(handler, http.MethodPost, "/api/v1/private-access/provider-accounts?tenant_id="+tenantID.String(), "admin-token", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "vault://tenant/private-access/netbird") {
		t.Fatalf("response leaked secret ref: %s", rec.Body.String())
	}
	var account privateAccessProviderAccountDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if account.Provider != "netbird" || account.Config["token_ref"] != "configured" {
		t.Fatalf("account response = %#v", account)
	}

	importBody := `{
		"provider_payload":{
			"peers":[{"id":"peer-1","name":"app-01","ip":"100.80.0.10","status":"connected"}],
			"routes":[{"id":"route-app","network":"10.40.1.0/24","peer":"peer-1","enabled":true,"access_control_groups":["grp-admins"]}]
		}
	}`
	rec = callPrivateAccessAPI(handler, http.MethodPost, "/api/v1/private-access/provider-accounts/"+account.ID+"/import?tenant_id="+tenantID.String(), "operator-token", importBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.snapshots) != 1 || store.snapshots[0].Provider != privateaccess.ProviderNetBird || len(store.snapshots[0].Snapshot.Routes) != 1 {
		t.Fatalf("snapshots = %#v", store.snapshots)
	}
	var importResp struct {
		Run privateAccessImportRunDTO `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("decode import: %v", err)
	}
	if importResp.Run.Status != storage.PrivateAccessImportStatusSucceeded || importResp.Run.Summary["routes"].(float64) != 1 {
		t.Fatalf("run = %#v", importResp.Run)
	}

	rec = callPrivateAccessAPI(handler, http.MethodGet, "/api/v1/private-access/import-runs?tenant_id="+tenantID.String(), "viewer-token", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listed paginatedResponse[privateAccessImportRunDTO]
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if listed.Pagination.Total != 1 || len(listed.Data) != 1 || listed.Data[0].Status != storage.PrivateAccessImportStatusSucceeded {
		t.Fatalf("listed = %#v", listed)
	}
}

func callPrivateAccessAPI(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
