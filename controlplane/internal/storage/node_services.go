package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	ListenAddr       string
	Port             int
	ServiceKind      string
	ProbeStatus      *int
	ProbeServer      *string
	ProbeTitle       *string
	ProbeContentType *string
	ObservedAt       time.Time
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
				(node_id, tenant_id, pid, process, binary_path, listen_addr, port, service_kind,
				 probe_status, probe_server, probe_title, probe_content_type, observed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		`)
		if perr != nil {
			return fmt.Errorf("prepare insert: %w", perr)
		}
		defer func() { _ = stmt.Close() }()
		for _, svc := range services {
			if _, err := stmt.ExecContext(ctx,
				nodeID, tenantID, svc.PID, svc.Process, svc.BinaryPath, svc.ListenAddr,
				svc.Port, kindOrUnknown(svc.ServiceKind),
				svc.ProbeStatus, svc.ProbeServer, svc.ProbeTitle, svc.ProbeContentType,
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

func (s *Store) queryServices(ctx context.Context, where string, args ...any) ([]NodeService, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	q := `SELECT id, node_id, tenant_id, pid, process, binary_path, listen_addr, port, service_kind,
		probe_status, probe_server, probe_title, probe_content_type, observed_at
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
		if err := rows.Scan(
			&n.ID, &n.NodeID, &n.TenantID, &n.PID, &n.Process, &n.BinaryPath,
			&n.ListenAddr, &n.Port, &n.ServiceKind,
			&status, &server, &title, &ctype, &n.ObservedAt,
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
