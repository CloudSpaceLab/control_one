package server

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type fakeLogDumpHTTPStore struct {
	expired []uuid.UUID
}

func (f *fakeLogDumpHTTPStore) CreateLogDump(context.Context, storage.LogDump) (*storage.LogDump, error) {
	return nil, nil
}
func (f *fakeLogDumpHTTPStore) CreateAgentLogDumpWithJob(context.Context, storage.LogDump, storage.Job) (*storage.LogDump, *storage.Job, error) {
	return nil, nil, nil
}
func (f *fakeLogDumpHTTPStore) GetLogDump(context.Context, uuid.UUID, uuid.UUID) (*storage.LogDump, error) {
	return nil, nil
}
func (f *fakeLogDumpHTTPStore) ListLogDumps(context.Context, storage.LogDumpFilter, int, int) ([]storage.LogDump, int, error) {
	return nil, 0, nil
}
func (f *fakeLogDumpHTTPStore) ExpireLogDump(_ context.Context, _, _ uuid.UUID, dumpID uuid.UUID, _ time.Time) (bool, error) {
	f.expired = append(f.expired, dumpID)
	return true, nil
}
func (f *fakeLogDumpHTTPStore) MarkControlPlaneLogDumpCaptured(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, int64, int64, bool, time.Time) error {
	return nil
}
func (f *fakeLogDumpHTTPStore) FailControlPlaneLogDump(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func TestApplyLogDumpReadExpiryUsesServerClock(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	server := &Server{clockOverride: func() time.Time { return now }}
	store := &fakeLogDumpHTTPStore{}
	dump := storage.LogDump{
		ID: uuid.New(), TenantID: uuid.New(), NodeID: uuid.New(),
		Status: storage.LogDumpStatusCaptured, ExpiresAt: now,
	}

	got := server.applyLogDumpReadExpiry(context.Background(), store, dump)

	require.Equal(t, storage.LogDumpStatusExpired, got.Status)
	require.Equal(t, []uuid.UUID{dump.ID}, store.expired)
	resp := server.logDumpResponse(got)
	require.True(t, resp.Expired)
	require.Empty(t, resp.DownloadURL)
	require.Empty(t, resp.PreviewURL)
}

func TestDecodeStrictLogDumpJSONRejectsUnknownAndTrailingDocuments(t *testing.T) {
	for _, body := range []string{
		`{"tenant_id":"x","unknown":true}`,
		`{"tenant_id":"x"} {"tenant_id":"y"}`,
	} {
		req := httptest.NewRequest("POST", "/api/v1/log-dumps", strings.NewReader(body))
		var payload logDumpRequest
		require.Error(t, decodeStrictJSONDocument(req, &payload, 8<<10))
	}
}

func TestLogDumpResponseHeadersDisableCaching(t *testing.T) {
	w := httptest.NewRecorder()
	setLogDumpResponseHeaders(w)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
}

func TestStreamLogDumpStopsWhenExpiryCrossesMidDownload(t *testing.T) {
	expiresAt := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	calls := 0
	now := func() time.Time {
		calls++
		if calls == 1 {
			return expiresAt.Add(-time.Second)
		}
		return expiresAt
	}
	payload := bytes.Repeat([]byte("x"), 128<<10)
	var dst bytes.Buffer

	err := streamLogDumpUntilExpiry(&dst, bytes.NewReader(payload), expiresAt, now)

	require.ErrorIs(t, err, errLogDumpDownloadExpired)
	require.Len(t, dst.Bytes(), 64<<10)
}
