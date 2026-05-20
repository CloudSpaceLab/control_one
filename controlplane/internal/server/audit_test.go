package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ctxObservingStore wraps fakeStore but forwards CreateAuditLog while
// enforcing the parent context. The real *storage.Store also enforces ctx
// (the database driver returns context.Canceled). We mirror that here so the
// test catches regressions on the recordAudit goroutine path.
type ctxObservingStore struct {
	*fakeStore
	doneCh chan struct{}
}

func (c *ctxObservingStore) CreateAuditLog(ctx context.Context, entry *storage.AuditLog) (*storage.AuditLog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defer close(c.doneCh)
	return c.fakeStore.CreateAuditLog(ctx, entry)
}

// TestRecordAuditAsyncSurvivesRequestContextCancellation pins the fix for the
// silent audit-log dropout: recordAudit dispatched its DB write on a
// goroutine that captured r.Context(). When the HTTP handler returned the
// request context was cancelled, killing the insert with "context canceled"
// — which only surfaced as a Warn log line. The fix detaches the context.
func TestRecordAuditAsyncSurvivesRequestContextCancellation(t *testing.T) {
	t.Parallel()

	srv := &Server{
		logger:     zap.NewNop(),
		auditAsync: true,
	}
	store := &ctxObservingStore{
		fakeStore: &fakeStore{auditLogs: []storage.AuditLog{}},
		doneCh:    make(chan struct{}),
	}
	srv.store = store

	parent, cancel := context.WithCancel(context.Background())
	srv.recordAudit(parent, nil, uuid.New(), "test.action", "test", "rid-1", map[string]any{"k": "v"})
	// Simulate the HTTP handler returning: request ctx is cancelled before
	// the goroutine runs.
	cancel()

	select {
	case <-store.doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("audit write never completed — recordAudit goroutine likely died on cancelled context")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.auditLogs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(store.auditLogs))
	}
	if got := store.auditLogs[0].Action; got != "test.action" {
		t.Fatalf("unexpected action: %q", got)
	}
}

// TestRecordAuditAsyncRetainsValuesFromParent makes sure context.WithoutCancel
// (used by the fix) preserves values — auth principal lookup, request id,
// etc. — even though it strips the deadline.
func TestRecordAuditAsyncRetainsValuesFromParent(t *testing.T) {
	t.Parallel()

	srv := &Server{
		logger:     zap.NewNop(),
		auditAsync: true,
	}
	type ctxKey struct{}
	store := &ctxValueAssertingStore{
		fakeStore: &fakeStore{auditLogs: []storage.AuditLog{}},
		key:       ctxKey{},
		want:      "carry-me",
		got:       make(chan any, 1),
	}
	srv.store = store

	parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "carry-me"))
	srv.recordAudit(parent, &auth.Principal{Type: "service"}, uuid.New(), "test.action", "test", "", nil)
	cancel()

	select {
	case got := <-store.got:
		if got != "carry-me" {
			t.Fatalf("expected ctx value to be carried, got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("audit write never completed")
	}
}

func TestHandleAuditCollectionRequiresAndAppliesTenantFilter(t *testing.T) {
	t.Parallel()

	tenantA := uuid.New()
	tenantB := uuid.New()
	store := &fakeStore{
		auditLogs: []storage.AuditLog{
			{
				ID:           uuid.New(),
				TenantID:     tenantA,
				ActorType:    "user",
				Action:       "node.update",
				ResourceType: "node",
				CreatedAt:    time.Now().UTC(),
			},
			{
				ID:           uuid.New(),
				TenantID:     tenantB,
				ActorType:    "system",
				Action:       "job.succeeded",
				ResourceType: "job",
				CreatedAt:    time.Now().UTC().Add(-time.Minute),
			},
		},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}, store, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?limit=100&offset=0", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d got %d body=%s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?tenant_id="+tenantA.String()+"&limit=100&offset=0", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec = httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var got paginatedResponse[auditLogResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected logs for requested tenant, got %d: %#v", len(got.Data), got.Data)
	}
	if got.Pagination.Total != 1 {
		t.Fatalf("expected total 1, got %d", got.Pagination.Total)
	}
	if got.Data[0].TenantID == nil || *got.Data[0].TenantID != tenantA.String() {
		t.Fatalf("unexpected tenant in response: %#v", got.Data[0].TenantID)
	}
}

func TestHandleAuditCollectionRejectsInvalidTenantFilter(t *testing.T) {
	t.Parallel()

	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}, &fakeStore{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit?tenant_id=not-a-uuid", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d got %d body=%s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

type ctxValueAssertingStore struct {
	*fakeStore
	key  any
	want any
	got  chan any
}

func (c *ctxValueAssertingStore) CreateAuditLog(ctx context.Context, entry *storage.AuditLog) (*storage.AuditLog, error) {
	c.got <- ctx.Value(c.key)
	return c.fakeStore.CreateAuditLog(ctx, entry)
}
