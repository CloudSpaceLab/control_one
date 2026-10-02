package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// NodeService is one listening service the agent observed on a node. The
// agent computes service_kind locally via heuristics; probe fields are
// populated only when the optional localhost HTTP probe is enabled.
type NodeService struct {
	ID               uuid.UUID
	NodeID           uuid.UUID
	TenantID         uuid.UUID
	PID              int
	Process          string
	BinaryPath       string
	WorkingDir       string
	CommandLine      string
	ListenAddr       string
	Port             int
	ServiceKind      string
	ProbeStatus      *int
	ProbeServer      *string
	ProbeTitle       *string
	ProbeContentType *string
	AppRoot          string
	AppProfileID     string
	AppName          string
	AppConfidence    int
	AppEvidence      []string
	ObservedAt       time.Time
}

// NodeServiceInventoryRow adds the node context needed by tenant-wide
// observability views without forcing the UI to fan out one request per node.
type NodeServiceInventoryRow struct {
	NodeService
	NodeHostname   string
	NodeTargetType string
	NodeState      string
	NodeLastSeenAt *time.Time
}


// ReplaceNodeServices atomically swaps the listening-service set for a node.
// Called when an agent reports a fresh inventory cycle. Empty `services`
// means "no listening services discovered" — the table is cleared for that
// node.
func (s *Store) ReplaceNodeServices(ctx context.Context, nodeID, tenantID uuid.UUID, services []NodeService) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil || tenantID == uuid.Nil {
		return errors.New("node and tenant id required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_services WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("delete node services: %w", err)
	}

	if len(services) > 0 {
		stmt, perr := tx.PrepareContext(ctx, `
			INSERT INTO node_services
				(node_id, tenant_id, pid, process, binary_path, working_dir, command_line, listen_addr, port, service_kind,
				 probe_status, probe_server, probe_title, probe_content_type,
				 app_root, app_profile_id, app_name, app_confidence, app_evidence, observed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, NOW())
		`)
		if perr != nil {
			return fmt.Errorf("prepare insert: %w", perr)
		}
		defer func() { _ = stmt.Close() }()
		for _, svc := range services {
			appEvidence, err := json.Marshal(svc.AppEvidence)
			if err != nil {
				return fmt.Errorf("marshal service app evidence: %w", err)
			}
			if _, err := stmt.ExecContext(ctx,
				nodeID, tenantID, svc.PID, svc.Process, svc.BinaryPath, svc.WorkingDir, svc.CommandLine, svc.ListenAddr,
				svc.Port, kindOrUnknown(svc.ServiceKind),
				svc.ProbeStatus, svc.ProbeServer, svc.ProbeTitle, svc.ProbeContentType,
				svc.AppRoot, svc.AppProfileID, svc.AppName, svc.AppConfidence, appEvidence,
			); err != nil {
				return fmt.Errorf("insert service %s:%d: %w", svc.Process, svc.Port, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ListNodeServicesForNode returns every listening service for a node.
func (s *Store) ListNodeServicesForNode(ctx context.Context, nodeID uuid.UUID) ([]NodeService, error) {
	return s.queryServices(ctx,
		`WHERE node_id = $1 ORDER BY port`, nodeID,
	)
}

// ListNodeServicesForTenant returns every listening service for a tenant
// across all of its nodes — used by the knowledge-graph generator.
func (s *Store) ListNodeServicesForTenant(ctx context.Context, tenantID uuid.UUID) ([]NodeService, error) {
	return s.queryServices(ctx,
		`WHERE tenant_id = $1 ORDER BY node_id, port`, tenantID,
	)
}


// ListNodeServicesForTenantPage returns the current listening-service inventory
// across a tenant with node identity and target type attached. Search and device
// scope are applied before pagination so totals remain truthful.
func (s *Store) ListNodeServicesForTenantPage(
	ctx context.Context,
	tenantID uuid.UUID,
	search string,
	targetScope string,
	limit int,
	offset int,
) ([]NodeServiceInventoryRow, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id required")
	}
	if limit <= 0 || offset < 0 {
		return nil, 0, errors.New("limit must be positive and offset non-negative")
	}

	clauses := []string{"s.tenant_id = $1"}
	args := []any{tenantID}

	search = strings.TrimSpace(search)
	if search != "" {
		args = append(args, "%"+search+"%")
		arg := fmt.Sprintf("$%d", len(args))
		clauses = append(clauses, fmt.Sprintf(
			`(s.app_name ILIKE %[1]s OR s.process ILIKE %[1]s OR s.service_kind ILIKE %[1]s OR s.listen_addr ILIKE %[1]s OR CAST(s.port AS TEXT) ILIKE %[1]s OR n.hostname ILIKE %[1]s)`,
			arg,
		))
	}

	targetTypeExpr := `COALESCE(NULLIF(n.labels->>'target.type', ''), 'unknown')`
	switch strings.ToLower(strings.TrimSpace(targetScope)) {
	case "", "all":
	case "server":
		clauses = append(clauses, targetTypeExpr+` IN ('server','vm','cloud_instance','domain_controller')`)
	case "endpoint":
		clauses = append(clauses, targetTypeExpr+` IN ('personal_pc','workstation','laptop','kiosk')`)
	case "unknown":
		clauses = append(clauses, targetTypeExpr+` NOT IN ('server','vm','cloud_instance','domain_controller','personal_pc','workstation','laptop','kiosk')`)
	default:
		return nil, 0, fmt.Errorf("invalid target scope %q", targetScope)
	}

	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM node_services s JOIN nodes n ON n.id = s.node_id AND n.tenant_id = s.tenant_id WHERE `+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenant node services: %w", err)
	}

	queryArgs := append([]any(nil), args...)
	queryArgs = append(queryArgs, limit, offset)
	limitArg := fmt.Sprintf("$%d", len(queryArgs)-1)
	offsetArg := fmt.Sprintf("$%d", len(queryArgs))

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id, s.node_id, s.tenant_id, s.pid, s.process, s.binary_path, s.working_dir, s.command_line,
			s.listen_addr, s.port, s.service_kind, s.probe_status, s.probe_server, s.probe_title, s.probe_content_type,
			s.app_root, s.app_profile_id, s.app_name, s.app_confidence, s.app_evidence, s.observed_at,
			n.hostname, `+targetTypeExpr+`, n.state, n.last_seen_at
		FROM node_services s
		JOIN nodes n ON n.id = s.node_id AND n.tenant_id = s.tenant_id
		WHERE `+where+`
		ORDER BY
			COALESCE(NULLIF(s.app_name, ''), NULLIF(s.service_kind, ''), NULLIF(s.process, ''), 'unknown'),
			n.hostname,
			s.port,
			s.listen_addr
		LIMIT `+limitArg+` OFFSET `+offsetArg,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenant node services: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]NodeServiceInventoryRow, 0, min(limit, total))
	for rows.Next() {
		var row NodeServiceInventoryRow
		var status sql.NullInt64
		var server, title, ctype sql.NullString
		var appEvidenceRaw []byte
		var nodeLastSeen sql.NullTime
		if err := rows.Scan(
			&row.ID, &row.NodeID, &row.TenantID, &row.PID, &row.Process, &row.BinaryPath,
			&row.WorkingDir, &row.CommandLine, &row.ListenAddr, &row.Port, &row.ServiceKind,
			&status, &server, &title, &ctype,
			&row.AppRoot, &row.AppProfileID, &row.AppName, &row.AppConfidence, &appEvidenceRaw, &row.ObservedAt,
			&row.NodeHostname, &row.NodeTargetType, &row.NodeState, &nodeLastSeen,
		); err != nil {
			return nil, 0, fmt.Errorf("scan tenant node service: %w", err)
		}
		if status.Valid {
			v := int(status.Int64)
			row.ProbeStatus = &v
		}
		if server.Valid {
			v := server.String
			row.ProbeServer = &v
		}
		if title.Valid {
			v := title.String
			row.ProbeTitle = &v
		}
		if ctype.Valid {
			v := ctype.String
			row.ProbeContentType = &v
		}
		if len(appEvidenceRaw) > 0 {
			_ = json.Unmarshal(appEvidenceRaw, &row.AppEvidence)
		}
		if nodeLastSeen.Valid {
			t := nodeLastSeen.Time
			row.NodeLastSeenAt = &t
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate tenant node services: %w", err)
	}
	return out, total, nil
}

func (s *Store) queryServices(ctx context.Context, where string, args ...any) ([]NodeService, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	q := `SELECT id, node_id, tenant_id, pid, process, binary_path, working_dir, command_line, listen_addr, port, service_kind,
		probe_status, probe_server, probe_title, probe_content_type,
		app_root, app_profile_id, app_name, app_confidence, app_evidence, observed_at
		FROM node_services ` + where
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list node services: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NodeService
	for rows.Next() {
		var n NodeService
		var status sql.NullInt64
		var server, title, ctype sql.NullString
		var appEvidenceRaw []byte
		if err := rows.Scan(
			&n.ID, &n.NodeID, &n.TenantID, &n.PID, &n.Process, &n.BinaryPath,
			&n.WorkingDir, &n.CommandLine, &n.ListenAddr, &n.Port, &n.ServiceKind,
			&status, &server, &title, &ctype,
			&n.AppRoot, &n.AppProfileID, &n.AppName, &n.AppConfidence, &appEvidenceRaw, &n.ObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		if status.Valid {
			v := int(status.Int64)
			n.ProbeStatus = &v
		}
		if server.Valid {
			v := server.String
			n.ProbeServer = &v
		}
		if title.Valid {
			v := title.String
			n.ProbeTitle = &v
		}
		if ctype.Valid {
			v := ctype.String
			n.ProbeContentType = &v
		}
		if len(appEvidenceRaw) > 0 {
			_ = json.Unmarshal(appEvidenceRaw, &n.AppEvidence)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func kindOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
