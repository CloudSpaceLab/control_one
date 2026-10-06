package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspayload "go.opentelemetry.io/proto/otlp/logs/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

func (f *networkCollectorAPIStore) ResolveNetworkReceiverSource(_ context.Context, tenant uuid.UUID, collector, sourceType, sender string) (*storage.NetworkSource, error) {
	if tenant != f.source.TenantID || collector != f.source.CollectorID || sourceType != f.source.SourceType || sender != f.source.SenderAddress {
		return nil, sql.ErrNoRows
	}
	return &f.source, nil
}
func TestFlowOTLPProtocolOwnershipAndSourceReceipts(t *testing.T) {
	for _, tc := range []struct{ flow, source string }{{"netflow_v5", "netflow"}, {"netflow_v9", "netflow"}, {"ipfix", "ipfix"}, {"sflow_5", "sflow"}} {
		t.Run(tc.flow, func(t *testing.T) {
			tenant, target, id := uuid.New(), uuid.New(), uuid.New()
			collector, token := "flow-edge", "c1ec_fixture_only"
			f := &networkCollectorAPIStore{contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}, collectors: []storage.ContentPackEdgeCollector{{TenantID: tenant, CollectorID: collector, Status: "healthy"}}, collectorTokens: map[string]string{contentPackTestCollectorTokenKey(tenant, collector): token}}, source: storage.NetworkSource{ID: id, TenantID: tenant, TargetID: target, CollectorID: collector, SourceType: tc.source, SenderAddress: "192.0.2.10"}}
			s := New(zap.NewNop(), &config.Config{}, f, nil)
			attr := func(key, value string) *commonpb.KeyValue {
				return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
			}
			record := &logspayload.LogRecord{ObservedTimeUnixNano: uint64(time.Now().UnixNano()), Attributes: []*commonpb.KeyValue{attr("flow.type", tc.flow), attr("flow.sampler_address", "192.0.2.10"), attr("source.address", "203.0.113.44"), attr("destination.address", "203.0.113.99"), attr("target_id", "spoofed")}}
			p := &logspb.ExportLogsServiceRequest{ResourceLogs: []*logspayload.ResourceLogs{{ScopeLogs: []*logspayload.ScopeLogs{{LogRecords: []*logspayload.LogRecord{record}}}}}}
			request := func() *httptest.ResponseRecorder {
				body, err := protojson.Marshal(p)
				require.NoError(t, err)
				r := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/"+collector+"/network-otlp?tenant_id="+tenant.String(), strings.NewReader(string(body)))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			require.Equal(t, 200, request().Code)
			require.Len(t, f.eventIngestRecords, 1)
			payload := string(f.eventIngestRecords[0].Payload)
			require.Contains(t, payload, target.String())
			require.Contains(t, payload, "network."+tc.source)
			require.NotContains(t, payload, "spoofed")
			require.NotContains(t, payload, `"node_id"`)
			require.Equal(t, "ready", f.reports[0].State)
			require.Equal(t, 200, request().Code)
			require.Len(t, f.eventIngestRecords, 1)
			record.Attributes[1] = attr("flow.sampler_address", "192.0.2.99")
			require.Equal(t, 403, request().Code)
			require.Len(t, f.eventIngestRecords, 1)
		})
	}
}
func TestManagementRecordsUseAssignedSourceIdentity(t *testing.T) {
	for _, source := range []string{"ssh_config", "netconf", "restconf", "vendor_api", "snmp_trap"} {
		t.Run(source, func(t *testing.T) {
			tenant, target, id := uuid.New(), uuid.New(), uuid.New()
			collector, token := "management-edge", "c1ec_fixture_only"
			f := &networkCollectorAPIStore{contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}, collectors: []storage.ContentPackEdgeCollector{{TenantID: tenant, CollectorID: collector, Status: "healthy"}}, collectorTokens: map[string]string{contentPackTestCollectorTokenKey(tenant, collector): token}}, source: storage.NetworkSource{ID: id, TenantID: tenant, TargetID: target, CollectorID: collector, SourceType: source}}
			s := New(zap.NewNop(), &config.Config{}, f, nil)
			now := time.Now().UTC()
			record := map[string]any{"snapshot": "hostname fixture", "sanitized": true}
			if source != "snmp_trap" {
				adapter := source
				if source == "ssh_config" {
					adapter = "ssh_config/cisco"
				}
				record["adapter"] = adapter
				record["adapter_version"] = "management-read/v1"
				record["format"] = "text"
			}
			body, _ := json.Marshal(map[string]any{"reports": []any{map[string]any{"binding_id": id.String(), "state": "ready", "observed_at": now, "last_contact_at": now, "records": []any{record}}}})
			r := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/"+collector+"/network-reports?tenant_id="+tenant.String(), strings.NewReader(string(body)))
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			require.Equal(t, 200, w.Code)
			require.Len(t, f.eventIngestRecords, 1)
			payload := string(f.eventIngestRecords[0].Payload)
			require.Contains(t, payload, "network."+source)
			require.Contains(t, payload, target.String())
			require.NotContains(t, payload, `"node_id"`)
		})
	}
}
