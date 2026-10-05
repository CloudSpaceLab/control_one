package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type networkCollectorAPIStore struct {
	*contentPackSnapshotFakeStore
	source  storage.NetworkSource
	reports []networkdevice.SourceReport
}

func (f *networkCollectorAPIStore) GetCollectorNetworkSource(_ context.Context, tenant uuid.UUID, collector string, id uuid.UUID) (*storage.NetworkSource, error) {
	if tenant != f.source.TenantID || collector != f.source.CollectorID || id != f.source.ID {
		return nil, sql.ErrNoRows
	}
	return &f.source, nil
}

func TestSNMPMetricReportCanonicalIdentityAndReplay(t *testing.T) {
	tenant, target, id := uuid.New(), uuid.New(), uuid.New()
	collector, token := "edge-snmp", "c1ec_fixture_only"
	f := &networkCollectorAPIStore{contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}, collectors: []storage.ContentPackEdgeCollector{{TenantID: tenant, CollectorID: collector, Status: "healthy"}}, collectorTokens: map[string]string{contentPackTestCollectorTokenKey(tenant, collector): token}}, source: storage.NetworkSource{ID: id, TenantID: tenant, TargetID: target, CollectorID: collector, SourceType: "snmp_poll"}}
	s := New(zap.NewNop(), &config.Config{}, f, nil)
	observed := time.Now().UTC()
	report := networkdevice.SourceReport{BindingID: id.String(), State: "ready", ObservedAt: observed, LastContactAt: &observed, Metrics: map[string]float64{"uptime_ticks": 123, "interface_count": 2}}
	request := func(p networkdevice.SourceReport) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"reports": []networkdevice.SourceReport{p}})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/"+collector+"/network-reports?tenant_id="+tenant.String(), strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	require.Equal(t, 200, request(report).Code)
	require.Equal(t, 200, request(report).Code)
	require.Len(t, f.eventIngestRecords, 1)
	payload := string(f.eventIngestRecords[0].Payload)
	require.Contains(t, payload, target.String())
	require.Contains(t, payload, "network.snmp")
	require.Contains(t, payload, "uptime_ticks")
	require.NotContains(t, payload, `"node_id"`)
	report.BindingID = uuid.NewString()
	require.Equal(t, 403, request(report).Code)
	require.Len(t, f.eventIngestRecords, 1)
	report.BindingID = id.String()
	report.Metrics = map[string]float64{"password": 1}
	require.Equal(t, 400, request(report).Code)
}

func (f *networkCollectorAPIStore) ListNetworkSources(context.Context, uuid.UUID, storage.TargetAccess) ([]storage.NetworkSource, error) {
	return []storage.NetworkSource{f.source}, nil
}
func (f *networkCollectorAPIStore) ConfigureNetworkSource(context.Context, uuid.UUID, storage.NetworkSourceConfig, storage.TargetAccess) error {
	return nil
}
func (f *networkCollectorAPIStore) ReportNetworkSources(_ context.Context, tenant uuid.UUID, collector string, r []networkdevice.SourceReport) error {
	if tenant != f.source.TenantID || collector != f.source.CollectorID {
		return sql.ErrNoRows
	}
	f.reports = append(f.reports, r...)
	return nil
}
func (f *networkCollectorAPIStore) ResolveNetworkSyslogSource(_ context.Context, tenant uuid.UUID, collector, sender string) (*storage.NetworkSource, error) {
	if tenant != f.source.TenantID || collector != f.source.CollectorID || sender != f.source.SenderAddress {
		return nil, sql.ErrNoRows
	}
	return &f.source, nil
}

func TestNetworkOTLPCollectorAuthenticationBindingAndJournal(t *testing.T) {
	tenant := uuid.New()
	target := uuid.New()
	id := uuid.New()
	collector := "edge-lagos"
	token := "c1ec_fixture_only"
	f := &networkCollectorAPIStore{contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}, collectors: []storage.ContentPackEdgeCollector{{TenantID: tenant, CollectorID: collector, Status: "healthy"}}, collectorTokens: map[string]string{contentPackTestCollectorTokenKey(tenant, collector): token}}, source: storage.NetworkSource{ID: id, TenantID: tenant, TargetID: target, CollectorID: collector, SenderAddress: "192.0.2.10"}}
	s := New(zap.NewNop(), &config.Config{}, f, nil)
	observed := time.Now().UTC().Add(-time.Second)
	raw, _ := json.Marshal(map[string]any{"resourceLogs": []any{map[string]any{"scopeLogs": []any{map[string]any{"logRecords": []any{map[string]any{"observedTimeUnixNano": uint64(observed.UnixNano()), "body": map[string]string{"stringValue": "<134>spoofed-host log"}, "attributes": []any{map[string]any{"key": "net.peer.ip", "value": map[string]string{"stringValue": "192.0.2.10"}}}}}}}}}})
	payload := string(raw)
	request := func(tenantID uuid.UUID, credential, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/"+collector+"/network-otlp?tenant_id="+tenantID.String(), strings.NewReader(body))
		r.Header.Set("X-ControlOne-Collector-Token", credential)
		r.Header.Set("Content-Type", "application/json")
		s.Handler().ServeHTTP(w, r)
		return w
	}
	require.Equal(t, 401, request(tenant, "wrong", payload).Code)
	require.Equal(t, 401, request(uuid.New(), token, payload).Code)
	w := request(tenant, token, payload)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, `{}`, w.Body.String())
	require.Len(t, f.eventIngestRecords, 1)
	record := f.eventIngestRecords[0]
	require.Equal(t, tenant, *record.TenantID)
	require.Equal(t, uuid.Nil, *record.NodeID)
	require.Contains(t, string(record.Payload), target.String())
	require.NotContains(t, string(record.Payload), `"node_id"`)
	require.Len(t, f.reports, 1)
	require.Equal(t, "partial", f.reports[0].State)
	require.WithinDuration(t, observed, f.reports[0].ObservedAt, time.Microsecond)
	require.Equal(t, 200, request(tenant, token, payload).Code)
	require.Len(t, f.eventIngestRecords, 1)
	require.Equal(t, 400, request(tenant, token, `{"resourceLogs":[]}`).Code)
}
