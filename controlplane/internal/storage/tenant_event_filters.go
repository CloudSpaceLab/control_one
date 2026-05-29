package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// TenantEventFilters describes a tenant's event-capture policy. Pushed to
// agents via the heartbeat response; agents reconfigure collectors on the
// next tick.
type TenantEventFilters struct {
	TenantID                          uuid.UUID `json:"tenant_id"`
	CaptureExternal                   bool      `json:"capture_external"`
	CaptureInternalSummary            bool      `json:"capture_internal_summary"`
	CaptureListeningChanges           bool      `json:"capture_listening_changes"`
	CaptureFiles                      bool      `json:"capture_files"`
	CaptureDBQueries                  bool      `json:"capture_db_queries"`
	ThreatMatchFull                   bool      `json:"threat_match_full"`
	FilePathsWatch                    []string  `json:"file_paths_watch"`
	FileSizeMinBytes                  int64     `json:"file_size_min_bytes"`
	AllowlistCIDRs                    []string  `json:"allowlist_cidrs"`
	DenylistCIDRs                     []string  `json:"denylist_cidrs"`
	TrustedProxyCIDRs                 []string  `json:"trusted_proxy_cidrs"`
	DBQueryTextCapture                bool      `json:"db_query_text_capture"`
	ForensicMode                      bool      `json:"forensic_mode"`
	ConnectorAutoConnectMediumRisk    bool      `json:"connector_auto_connect_medium_risk"`
	ConnectorAutoConnectHighRisk      bool      `json:"connector_auto_connect_high_risk"`
	ConnectorAutoConnectPrograms      []string  `json:"connector_auto_connect_programs"`
	ConnectorApprovalRequiredPrograms []string  `json:"connector_approval_required_programs"`
	ConnectorBlockedPrograms          []string  `json:"connector_blocked_programs"`
	UpdatedAt                         time.Time `json:"updated_at"`
}

// DefaultTenantEventFilters returns the safe defaults.
func DefaultTenantEventFilters(tenantID uuid.UUID) TenantEventFilters {
	return TenantEventFilters{
		TenantID:                          tenantID,
		CaptureExternal:                   true,
		CaptureInternalSummary:            true,
		CaptureListeningChanges:           true,
		CaptureFiles:                      true,
		CaptureDBQueries:                  true,
		ThreatMatchFull:                   true,
		FilePathsWatch:                    []string{"/etc/", "/var/lib/", "/var/log/", "/opt/", "/home/", "/root/"},
		AllowlistCIDRs:                    []string{},
		DenylistCIDRs:                     []string{},
		TrustedProxyCIDRs:                 []string{},
		DBQueryTextCapture:                true,
		ConnectorAutoConnectPrograms:      []string{},
		ConnectorApprovalRequiredPrograms: []string{},
		ConnectorBlockedPrograms:          []string{},
	}
}

// GetTenantEventFilters returns the policy for a tenant. When no row exists
// the default is returned (the agent applies it without bouncing the
// collectors).
func (s *Store) GetTenantEventFilters(ctx context.Context, tenantID uuid.UUID) (*TenantEventFilters, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, capture_external, capture_internal_summary, capture_listening_changes,
		       capture_files, capture_db_queries, threat_match_full,
		       file_paths_watch, file_size_min_bytes,
		       allowlist_cidrs, denylist_cidrs, trusted_proxy_cidrs,
		       db_query_text_capture, forensic_mode,
		       connector_auto_connect_medium_risk, connector_auto_connect_high_risk,
		       connector_auto_connect_programs, connector_approval_required_programs,
		       connector_blocked_programs, updated_at
		FROM tenant_event_filters WHERE tenant_id = $1
	`, tenantID)
	var f TenantEventFilters
	err := row.Scan(&f.TenantID, &f.CaptureExternal, &f.CaptureInternalSummary, &f.CaptureListeningChanges,
		&f.CaptureFiles, &f.CaptureDBQueries, &f.ThreatMatchFull,
		pq.Array(&f.FilePathsWatch), &f.FileSizeMinBytes,
		pq.Array(&f.AllowlistCIDRs), pq.Array(&f.DenylistCIDRs), pq.Array(&f.TrustedProxyCIDRs),
		&f.DBQueryTextCapture, &f.ForensicMode,
		&f.ConnectorAutoConnectMediumRisk, &f.ConnectorAutoConnectHighRisk,
		pq.Array(&f.ConnectorAutoConnectPrograms), pq.Array(&f.ConnectorApprovalRequiredPrograms),
		pq.Array(&f.ConnectorBlockedPrograms), &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			d := DefaultTenantEventFilters(tenantID)
			return &d, nil
		}
		return nil, err
	}
	return &f, nil
}

// UpsertTenantEventFilters writes the policy. Idempotent.
func (s *Store) UpsertTenantEventFilters(ctx context.Context, f TenantEventFilters) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if f.TenantID == uuid.Nil {
		return errors.New("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_event_filters (
			tenant_id, capture_external, capture_internal_summary, capture_listening_changes,
			capture_files, capture_db_queries, threat_match_full,
			file_paths_watch, file_size_min_bytes,
			allowlist_cidrs, denylist_cidrs, trusted_proxy_cidrs,
			db_query_text_capture, forensic_mode,
			connector_auto_connect_medium_risk, connector_auto_connect_high_risk,
			connector_auto_connect_programs, connector_approval_required_programs,
			connector_blocked_programs, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET
			capture_external = EXCLUDED.capture_external,
			capture_internal_summary = EXCLUDED.capture_internal_summary,
			capture_listening_changes = EXCLUDED.capture_listening_changes,
			capture_files = EXCLUDED.capture_files,
			capture_db_queries = EXCLUDED.capture_db_queries,
			threat_match_full = EXCLUDED.threat_match_full,
			file_paths_watch = EXCLUDED.file_paths_watch,
			file_size_min_bytes = EXCLUDED.file_size_min_bytes,
			allowlist_cidrs = EXCLUDED.allowlist_cidrs,
			denylist_cidrs = EXCLUDED.denylist_cidrs,
			trusted_proxy_cidrs = EXCLUDED.trusted_proxy_cidrs,
			db_query_text_capture = EXCLUDED.db_query_text_capture,
			forensic_mode = EXCLUDED.forensic_mode,
			connector_auto_connect_medium_risk = EXCLUDED.connector_auto_connect_medium_risk,
			connector_auto_connect_high_risk = EXCLUDED.connector_auto_connect_high_risk,
			connector_auto_connect_programs = EXCLUDED.connector_auto_connect_programs,
			connector_approval_required_programs = EXCLUDED.connector_approval_required_programs,
			connector_blocked_programs = EXCLUDED.connector_blocked_programs,
			updated_at = NOW()
	`, f.TenantID, f.CaptureExternal, f.CaptureInternalSummary, f.CaptureListeningChanges,
		f.CaptureFiles, f.CaptureDBQueries, f.ThreatMatchFull,
		pq.Array(f.FilePathsWatch), f.FileSizeMinBytes,
		pq.Array(f.AllowlistCIDRs), pq.Array(f.DenylistCIDRs), pq.Array(f.TrustedProxyCIDRs),
		f.DBQueryTextCapture, f.ForensicMode,
		f.ConnectorAutoConnectMediumRisk, f.ConnectorAutoConnectHighRisk,
		pq.Array(f.ConnectorAutoConnectPrograms), pq.Array(f.ConnectorApprovalRequiredPrograms),
		pq.Array(f.ConnectorBlockedPrograms))
	return err
}
