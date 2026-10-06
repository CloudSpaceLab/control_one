package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

var networkTelemetryLimiter = newRateLimiterRegistry(rate.Limit(1000), 2000)

func allowNetworkTelemetry(w http.ResponseWriter, tenant uuid.UUID, collector string, count int) bool {
	if ok, delay := networkTelemetryLimiter.allow(tenant.String()+"/"+collector, count); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(delay.Seconds())+1))
		http.Error(w, "collector ingestion rate exceeded", 429)
		return false
	}
	return true
}

type networkTelemetryStore interface {
	ListNetworkSources(context.Context, uuid.UUID, storage.TargetAccess) ([]storage.NetworkSource, error)
	ConfigureNetworkSource(context.Context, uuid.UUID, storage.NetworkSourceConfig, storage.TargetAccess) error
	ReportNetworkSources(context.Context, uuid.UUID, string, []networkdevice.SourceReport) error
	ResolveNetworkSyslogSource(context.Context, uuid.UUID, string, string) (*storage.NetworkSource, error)
}

type networkConfigurationSnapshotStore interface {
	SaveNetworkConfigurationSnapshot(context.Context, uuid.UUID, string, uuid.UUID, string, time.Time, networkdevice.ConfigurationSnapshot) (*storage.NetworkConfigurationSnapshot, error)
}

func (s *Server) handleCollectorNetworkBindings(w http.ResponseWriter, r *http.Request, collector string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", 405)
		return
	}
	tenant, _, ok := s.authorizeContentPackEdgeCollectorCall(w, r, collector, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(interface {
		ListCollectorNetworkSources(context.Context, uuid.UUID, string, int, int) ([]storage.NetworkSource, error)
	})
	if !ok {
		http.Error(w, "source bindings unavailable", 503)
		return
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid offset", 400)
			return
		}
		offset = parsed
	}
	rows, err := store.ListCollectorNetworkSources(r.Context(), tenant, collector, 100, offset)
	if err != nil {
		http.Error(w, "unable to read bindings", 500)
		return
	}
	var next *int
	if len(rows) == 100 {
		value := offset + 100
		next = &value
	}
	writeJSON(w, 200, map[string]any{"data": rows, "next_offset": next})
}

func (s *Server) handleNetworkTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", 405)
		return
	}
	id, err := uuid.Parse(strings.TrimPrefix(r.URL.Path, "/api/v1/network-telemetry/"))
	if err != nil || id == uuid.Nil {
		http.NotFound(w, r)
		return
	}
	permission := "targets.read"
	if r.Method == http.MethodPut {
		permission = "targets.write"
	}
	access, ok := s.targetAccess(w, r, permission)
	if !ok {
		return
	}
	store, ok := s.store.(networkTelemetryStore)
	if !ok {
		http.Error(w, "network telemetry unavailable", 503)
		return
	}
	if r.Method == http.MethodPut {
		var p storage.NetworkSourceConfig
		if decodeStrictJSONDocument(r, &p, 4096) != nil || p.Validate() != nil {
			http.Error(w, "invalid source binding", 400)
			return
		}
		identity, ok := s.store.(targetIdentityStore)
		if !ok {
			http.Error(w, "target store unavailable", 503)
			return
		}
		readAccess := access
		readAccess.Permission = "targets.read"
		target, err := identity.GetTarget(r.Context(), id, readAccess)
		if err != nil || target == nil || target.Family != "network_security" {
			http.NotFound(w, r)
			return
		}
		if err = store.ConfigureNetworkSource(r.Context(), id, p, access); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "target or collector access denied", 403)
			} else {
				http.Error(w, "unable to bind source; verify collector and unique sender", 409)
			}
			return
		}
		principal, _ := auth.PrincipalFromContext(r.Context())
		s.recordAudit(r.Context(), principal, target.TenantID, "network_source.configured", "target", id.String(), map[string]any{"source_type": p.SourceType, "collector_id": p.CollectorID})
		writeJSON(w, 200, map[string]string{"status": "configured"})
		return
	}
	rows, err := store.ListNetworkSources(r.Context(), id, access)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "unable to read sources", 500)
		return
	}
	now := time.Now().UTC()
	for i := range rows {
		rows[i].State = networkdevice.EffectiveSourceState(rows[i].State, rows[i].LastContactAt, rows[i].StaleAfterSeconds, now)
		rows[i].CollectorStatus = networkdevice.CollectorFreshness(rows[i].CollectorStatus, rows[i].CollectorHeartbeatAt, now)
	}
	// Empty types stay explicit; a collector heartbeat never proves source contact.
	writeJSON(w, 200, map[string]any{"target_id": id, "sources": rows, "source_types": networkdevice.SourceTypes, "generated_at": now})
}

func (s *Server) handleNetworkSourceReports(w http.ResponseWriter, r *http.Request, collector string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	tenant, _, ok := s.authorizeContentPackEdgeCollectorCall(w, r, collector, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(networkTelemetryStore)
	if !ok {
		http.Error(w, "network telemetry unavailable", 503)
		return
	}
	var p struct {
		Reports []networkdevice.SourceReport `json:"reports"`
	}
	if decodeStrictJSONDocument(r, &p, 1<<20) != nil || len(p.Reports) == 0 || len(p.Reports) > 100 {
		http.Error(w, "invalid source reports", 400)
		return
	}
	if !allowNetworkTelemetry(w, tenant, collector, len(p.Reports)) {
		return
	}
	for _, report := range p.Reports {
		if report.Validate(time.Now().UTC()) != nil {
			http.Error(w, "invalid source evidence", 400)
			return
		}
	}
	events := []IngestedEvent{}
	for _, report := range p.Reports {
		if len(report.Metrics) == 0 && len(report.Records) == 0 {
			continue
		}
		resolver, ok := s.store.(interface {
			GetCollectorNetworkSource(context.Context, uuid.UUID, string, uuid.UUID) (*storage.NetworkSource, error)
		})
		id, err := uuid.Parse(report.BindingID)
		if !ok || err != nil {
			http.Error(w, "SNMP metric binding unavailable", 403)
			return
		}
		source, err := resolver.GetCollectorNetworkSource(r.Context(), tenant, collector, id)
		if err != nil || source == nil || (len(report.Metrics) > 0 && source.SourceType != "snmp_poll") {
			http.Error(w, "SNMP metric binding denied", 403)
			return
		}
		if len(report.Metrics) > 0 {
			events = append(events, IngestedEvent{Type: "network.snmp", TS: report.ObservedAt, TenantID: tenant.String(), Collector: "snmp", ParserStatus: "parsed", Details: map[string]any{"target_id": source.TargetID.String(), "source_instance_id": source.ID.String(), "source_type": source.SourceType, "collector_id": collector, "metrics": report.Metrics}})
		}
		for _, record := range report.Records {
			if isConfigurationSource(source.SourceType) {
				configuration, ok := configurationSnapshotFromRecord(source.SourceType, record)
				if !ok {
					http.Error(w, "invalid sanitized configuration snapshot", 400)
					return
				}
				snapshotStore, ok := s.store.(networkConfigurationSnapshotStore)
				if !ok {
					http.Error(w, "configuration snapshot storage unavailable", 503)
					return
				}
				snapshot, err := snapshotStore.SaveNetworkConfigurationSnapshot(r.Context(), tenant, collector, source.ID, source.SourceType, report.ObservedAt, configuration)
				if err != nil || snapshot == nil {
					http.Error(w, "configuration snapshot could not be safely persisted", 422)
					return
				}
				record = map[string]any{"snapshot_id": snapshot.ID.String(), "revision": snapshot.Revision, "format": snapshot.Format, "adapter": snapshot.Adapter, "adapter_version": snapshot.AdapterVersion, "sha256": snapshot.ContentHash, "snapshot": snapshot.Content, "sanitized": true}
			}
			events = append(events, IngestedEvent{Type: "network." + source.SourceType, TS: report.ObservedAt, TenantID: tenant.String(), Collector: source.SourceType, ParserStatus: "parsed", Details: map[string]any{"target_id": source.TargetID.String(), "source_instance_id": source.ID.String(), "source_type": source.SourceType, "collector_id": collector, "record": record}})
		}
	}
	if err := store.ReportNetworkSources(r.Context(), tenant, collector, p.Reports); err != nil {
		http.Error(w, "source binding denied or invalid", 403)
		return
	}
	if len(events) > 0 {
		canonical, _ := json.Marshal(events)
		sum := sha256.Sum256(append([]byte(tenant.String()+"/"+collector+"/"), canonical...))
		svc := s.eventIngestService()
		batch, err := svc.recordLogDerivedBatch(r.Context(), tenant, uuid.Nil, events, "network-snmp/"+hex.EncodeToString(sum[:]))
		if err != nil {
			http.Error(w, "SNMP observations could not be journaled", 503)
			return
		}
		if !batch.Duplicate {
			if _, _, err = svc.complete(r.Context(), batch.ID, tenant, uuid.Nil, events); err != nil {
				writeJSON(w, 202, map[string]any{"status": "pending_doris"})
				return
			}
		}
	}
	writeJSON(w, 200, map[string]string{"status": "recorded"})
}

func isConfigurationSource(source string) bool {
	switch source {
	case "ssh_config", "netconf", "restconf", "vendor_api":
		return true
	default:
		return false
	}
}

func configurationSnapshotFromRecord(source string, record map[string]any) (networkdevice.ConfigurationSnapshot, bool) {
	format, formatOK := record["format"].(string)
	content, contentOK := record["snapshot"].(string)
	adapter, adapterOK := record["adapter"].(string)
	version, versionOK := record["adapter_version"].(string)
	sanitized, sanitizedOK := record["sanitized"].(bool)
	if !formatOK || !contentOK || !adapterOK || !versionOK || !sanitizedOK || !sanitized {
		return networkdevice.ConfigurationSnapshot{}, false
	}
	validAdapter := adapter == source
	if source == "ssh_config" {
		validAdapter = adapter == "ssh_config/cisco" || adapter == "ssh_config/juniper" || adapter == "ssh_config/fortinet"
	}
	if !validAdapter {
		return networkdevice.ConfigurationSnapshot{}, false
	}
	snapshot, err := networkdevice.NormalizeConfigurationSnapshot(adapter, version, format, content)
	if err != nil {
		return networkdevice.ConfigurationSnapshot{}, false
	}
	return snapshot, true
}

type networkSyslogEvent struct {
	SourceType    string         `json:"source_type,omitempty"`
	Fields        map[string]any `json:"fields,omitempty"`
	SenderAddress string         `json:"sender_address"`
	ObservedAt    time.Time      `json:"observed_at"`
	Message       string         `json:"message"`
}

func networkSyslogEnvelope(source storage.NetworkSource, e networkSyslogEvent, received time.Time) IngestedEvent {
	// Target identity comes from the tenant/collector/transport-sender binding,
	// never from the message's hostname or caller-supplied target ID.
	kind, parser := "log.line", "raw"
	if source.SourceType != "syslog" {
		kind, parser = "network."+source.SourceType, "parsed"
	}
	return IngestedEvent{Type: kind, TS: e.ObservedAt, TenantID: source.TenantID.String(), Collector: source.SourceType, ParserStatus: parser, Message: e.Message, Details: map[string]any{
		"target_id": source.TargetID.String(), "source_instance_id": source.ID.String(), "source_type": source.SourceType, "collector_id": source.CollectorID, "sender_address": source.SenderAddress, "received_at": received.Format(time.RFC3339Nano), "record": e.Fields,
	}}
}

func (s *Server) handleNetworkSyslogEvents(w http.ResponseWriter, r *http.Request, collector string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	tenant, _, ok := s.authorizeContentPackEdgeCollectorCall(w, r, collector, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(networkTelemetryStore)
	if !ok {
		http.Error(w, "network telemetry unavailable", 503)
		return
	}
	var p struct {
		Events  []networkSyslogEvent `json:"events"`
		BatchID string               `json:"batch_id"`
	}
	if decodeStrictJSONDocument(r, &p, 1<<20) != nil || len(p.Events) == 0 || len(p.Events) > 100 || len(p.BatchID) > 128 || strings.TrimSpace(p.BatchID) == "" {
		http.Error(w, "provide batch_id and 1 to 100 syslog events", 400)
		return
	}
	if !allowNetworkTelemetry(w, tenant, collector, len(p.Events)) {
		return
	}
	now := time.Now().UTC()
	events := make([]IngestedEvent, 0, len(p.Events))
	reports := map[string]networkdevice.SourceReport{}
	for _, e := range p.Events {
		if e.SourceType == "" {
			e.SourceType = "syslog"
		}
		if !networkdevice.IsReceiverSource(e.SourceType) {
			http.Error(w, "invalid receiver source", 400)
			return
		}
		fields, fieldErr := json.Marshal(e.Fields)
		if fieldErr != nil || len(fields) > 32768 {
			http.Error(w, "receiver record exceeds limit", 400)
			return
		}
		if e.ObservedAt.IsZero() || e.ObservedAt.After(now.Add(time.Minute)) || e.ObservedAt.Before(now.Add(-24*time.Hour)) || len(e.Message) == 0 || len(e.Message) > 8192 {
			http.Error(w, "invalid syslog observation", 400)
			return
		}
		var source *storage.NetworkSource
		var err error
		if e.SourceType == "syslog" {
			source, err = store.ResolveNetworkSyslogSource(r.Context(), tenant, collector, e.SenderAddress)
		} else if resolver, ok := s.store.(interface {
			ResolveNetworkReceiverSource(context.Context, uuid.UUID, string, string, string) (*storage.NetworkSource, error)
		}); ok {
			source, err = resolver.ResolveNetworkReceiverSource(r.Context(), tenant, collector, e.SourceType, e.SenderAddress)
		} else {
			err = sql.ErrNoRows
		}
		if err != nil || source == nil {
			http.Error(w, "unbound syslog transport sender", 403)
			return
		}
		ev := networkSyslogEnvelope(*source, e, now)
		normalizeIngestedEventMetadata(&ev, tenant, uuid.Nil)
		events = append(events, ev)
		// Raw forwarding is partial until parser evidence is separately reported.
		contact := e.ObservedAt
		if old, ok := reports[source.ID.String()]; !ok || contact.After(*old.LastContactAt) {
			state := "ready"
			if e.SourceType == "syslog" {
				state = "partial"
			}
			reports[source.ID.String()] = networkdevice.SourceReport{BindingID: source.ID.String(), State: state, ObservedAt: contact, LastContactAt: &contact}
		}
	}
	canonical, _ := json.Marshal(p)
	sum := sha256.Sum256(append([]byte(tenant.String()+"/"+collector+"/"), canonical...))
	svc := s.eventIngestService()
	batch, err := svc.recordLogDerivedBatch(r.Context(), tenant, uuid.Nil, events, "network-syslog/"+hex.EncodeToString(sum[:]))
	if err != nil {
		http.Error(w, "unable to journal syslog events", 503)
		return
	}
	reportList := make([]networkdevice.SourceReport, 0, len(reports))
	for _, v := range reports {
		reportList = append(reportList, v)
	}
	// Receipt retries reuse event observation times and cannot freshen old events.
	if err = store.ReportNetworkSources(r.Context(), tenant, collector, reportList); err != nil {
		http.Error(w, "events journaled; source receipt failed", 503)
		return
	}
	if !batch.Duplicate {
		_, _, err = svc.complete(r.Context(), batch.ID, tenant, uuid.Nil, events)
		if err != nil {
			writeJSON(w, 202, map[string]any{"batch_id": batch.ID, "status": "pending_doris"})
			return
		}
	}
	writeJSON(w, 202, map[string]any{"batch_id": batch.ID, "status": batch.Status, "duplicate": batch.Duplicate})
}
