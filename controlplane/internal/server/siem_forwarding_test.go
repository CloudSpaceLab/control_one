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
)

type siemForwardingTestStore struct {
	*fakeStore
	upserts      []storage.UpsertSIEMForwardingDestinationParams
	destinations []storage.SIEMForwardingDestination
}

func (s *siemForwardingTestStore) UpsertSIEMForwardingDestination(_ context.Context, p storage.UpsertSIEMForwardingDestinationParams) (*storage.SIEMForwardingDestination, error) {
	s.upserts = append(s.upserts, p)
	now := time.Now().UTC()
	kind := strings.ToLower(strings.TrimSpace(p.Kind))
	if kind == "splunk" {
		kind = storage.SIEMForwardingKindSplunkHEC
	}
	if kind == "elastic" {
		kind = storage.SIEMForwardingKindElasticsearch
	}
	status := strings.ToLower(strings.TrimSpace(p.Status))
	if status == "" {
		status = storage.SIEMForwardingDestinationStatusEnabled
	}
	config := make(map[string]any, len(p.Config))
	for key, value := range p.Config {
		config[key] = value
	}
	for i := range s.destinations {
		if s.destinations[i].TenantID == p.TenantID && strings.EqualFold(s.destinations[i].Name, strings.TrimSpace(p.Name)) {
			s.destinations[i].Kind = kind
			s.destinations[i].Status = status
			s.destinations[i].URL = strings.TrimSpace(p.URL)
			s.destinations[i].Config = config
			s.destinations[i].UpdatedBySubject = strings.TrimSpace(p.UpdatedBySubject)
			s.destinations[i].UpdatedAt = now
			copy := s.destinations[i]
			return &copy, nil
		}
	}
	row := storage.SIEMForwardingDestination{
		ID:               uuid.New(),
		TenantID:         p.TenantID,
		Name:             strings.TrimSpace(p.Name),
		Kind:             kind,
		Status:           status,
		URL:              strings.TrimSpace(p.URL),
		Config:           config,
		CreatedBySubject: strings.TrimSpace(p.UpdatedBySubject),
		UpdatedBySubject: strings.TrimSpace(p.UpdatedBySubject),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	s.destinations = append(s.destinations, row)
	return &row, nil
}

func (s *siemForwardingTestStore) ListSIEMForwardingDestinations(_ context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]storage.SIEMForwardingDestination, int, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	filtered := make([]storage.SIEMForwardingDestination, 0, len(s.destinations))
	for _, destination := range s.destinations {
		if destination.TenantID != tenantID {
			continue
		}
		if status != "" && destination.Status != status {
			continue
		}
		filtered = append(filtered, destination)
	}
	total := len(filtered)
	if offset > total {
		return []storage.SIEMForwardingDestination{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func TestSIEMForwardingDestinationsAPIUpsertsWithRedactedCredentialRef(t *testing.T) {
	tenantID := uuid.New()
	store := &siemForwardingTestStore{fakeStore: &fakeStore{}}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("operator", "siem-operator"),
	}, store, &stubQueue{})

	body := bytes.NewBufferString(`{
		"name":"Existing Splunk",
		"kind":"splunk",
		"url":"https://splunk.example.test:8088/services/collector",
		"config":{"token_ref":"vault://tenant/siem/splunk","index":"bank_sec"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/siem/forwarding-destinations?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer siem-operator")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "vault://tenant/siem/splunk") {
		t.Fatalf("response leaked credential ref: %s", rec.Body.String())
	}
	var resp siemForwardingDestinationDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Kind != storage.SIEMForwardingKindSplunkHEC || resp.Status != storage.SIEMForwardingDestinationStatusEnabled {
		t.Fatalf("response kind/status = %s/%s", resp.Kind, resp.Status)
	}
	if !resp.HasCredentialRef || resp.Config["token_ref"] != "configured" || resp.Config["index"] != "bank_sec" {
		t.Fatalf("response config = %#v credential=%v", resp.Config, resp.HasCredentialRef)
	}
	if len(store.upserts) != 1 || store.upserts[0].UpdatedBySubject != "siem-operator" {
		t.Fatalf("upsert params = %#v", store.upserts)
	}
}

func TestSIEMForwardingDestinationsAPIListsTenantScopedDestinations(t *testing.T) {
	tenantID := uuid.New()
	otherTenantID := uuid.New()
	now := time.Now().UTC()
	store := &siemForwardingTestStore{
		fakeStore: &fakeStore{},
		destinations: []storage.SIEMForwardingDestination{
			{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Name:      "Loki",
				Kind:      storage.SIEMForwardingKindLoki,
				Status:    storage.SIEMForwardingDestinationStatusEnabled,
				URL:       "https://loki.example.test",
				Config:    map[string]any{"tenant_label": "bank-a"},
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Name:      "Disabled Splunk",
				Kind:      storage.SIEMForwardingKindSplunkHEC,
				Status:    storage.SIEMForwardingDestinationStatusDisabled,
				URL:       "https://splunk.example.test",
				Config:    map[string]any{"token_ref": "vault://tenant/disabled"},
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        uuid.New(),
				TenantID:  otherTenantID,
				Name:      "Other Tenant",
				Kind:      storage.SIEMForwardingKindLoki,
				Status:    storage.SIEMForwardingDestinationStatusEnabled,
				URL:       "https://other.example.test",
				Config:    map[string]any{},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "siem-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/siem/forwarding-destinations?tenant_id="+tenantID.String()+"&status=enabled&limit=10", nil)
	req.Header.Set("Authorization", "Bearer siem-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp paginatedResponse[siemForwardingDestinationDTO]
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Pagination.Total != 1 || len(resp.Data) != 1 {
		t.Fatalf("response = %#v", resp)
	}
	if resp.Data[0].TenantID != tenantID.String() || resp.Data[0].Name != "Loki" {
		t.Fatalf("data = %#v", resp.Data)
	}
}
