package server

import (
	"context"
	"database/sql"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type telemetryAPIStore struct {
	*fakeStore
	rows []storage.NetworkSource
	deny bool
}

func (f *telemetryAPIStore) ListNetworkSources(context.Context, uuid.UUID, storage.TargetAccess) ([]storage.NetworkSource, error) {
	if f.deny {
		return nil, sql.ErrNoRows
	}
	return f.rows, nil
}
func (f *telemetryAPIStore) ConfigureNetworkSource(context.Context, uuid.UUID, storage.NetworkSourceConfig, storage.TargetAccess) error {
	return nil
}
func (f *telemetryAPIStore) ReportNetworkSources(context.Context, uuid.UUID, string, []networkdevice.SourceReport) error {
	return nil
}
func (f *telemetryAPIStore) ResolveNetworkSyslogSource(context.Context, uuid.UUID, string, string) (*storage.NetworkSource, error) {
	return &f.rows[0], nil
}

func TestNetworkTelemetryReadTruthAndAuthorization(t *testing.T) {
	user := uuid.New()
	target := uuid.New()
	old := time.Now().UTC().Add(-time.Hour)
	recent := time.Now().UTC()
	store := &telemetryAPIStore{fakeStore: &fakeStore{usersByID: map[uuid.UUID]*storage.User{user: {ID: user}}}, rows: []storage.NetworkSource{{State: "ready", LastContactAt: &recent, StaleAfterSeconds: 300, CollectorStatus: "healthy", CollectorHeartbeatAt: &old}, {State: "ready", LastContactAt: &old, StaleAfterSeconds: 300, CollectorStatus: "healthy", CollectorHeartbeatAt: &recent}}}
	s := New(zap.NewNop(), &config.Config{}, store, nil)
	viewer := &auth.Principal{Type: "user", Subject: user.String(), Roles: []string{roleViewer}}
	req := func(method, body string, p *auth.Principal) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(w, withPrincipal(httptest.NewRequest(method, "/api/v1/network-telemetry/"+target.String(), strings.NewReader(body)), p))
		return w
	}
	w := req("GET", "", viewer)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"state":"ready"`)
	require.Contains(t, w.Body.String(), `"collector_status":"stale"`)
	require.Contains(t, w.Body.String(), `"state":"stale"`)
	require.Equal(t, 403, req("PUT", `{}`, viewer).Code)
	agent := &auth.Principal{Type: "agent", Subject: uuid.NewString()}
	require.Equal(t, 403, req("GET", "", agent).Code)
	store.deny = true
	require.Equal(t, 404, req("GET", "", viewer).Code)
}

func TestSyslogBindingProvidesIdentityWithoutNode(t *testing.T) {
	tenant, target, id := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	source := storage.NetworkSource{ID: id, TenantID: tenant, TargetID: target, CollectorID: "edge-lagos", SenderAddress: "192.0.2.10", SourceType: "syslog"}
	event := networkSyslogEnvelope(source, networkSyslogEvent{Message: "<134>spoofed-host message", ObservedAt: now}, now)
	require.Empty(t, event.NodeID)
	require.Equal(t, tenant.String(), event.TenantID)
	require.Equal(t, target.String(), event.Details["target_id"])
	require.Equal(t, id.String(), event.Details["source_instance_id"])
	require.Equal(t, "raw", event.ParserStatus)
	payload, err := encodeIngestedEventPayload([]IngestedEvent{event})
	require.NoError(t, err)
	restored, err := decodeIngestedEventPayload(payload, "", maxEventIngestDecompressed, maxEventIngestRows)
	require.NoError(t, err)
	require.Equal(t, target.String(), restored[0].Details["target_id"])
}
