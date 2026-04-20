package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ClusterLBRegistration tracks load-balancer membership for a node within a
// cluster. Rows are created when a new member joins (register) and are flipped
// to deregistered_at=NOW() during shrink/teardown — we keep the history row so
// audit reviewers can see exactly which LB endpoints the node hit.
type ClusterLBRegistration struct {
	ClusterID      uuid.UUID
	NodeID         uuid.UUID
	Provider       string
	LBIdentifier   string
	RegisteredAt   time.Time
	DeregisteredAt *time.Time
}

// CreateClusterLBRegistrationParams captures input for inserting a new LB
// registration row.
type CreateClusterLBRegistrationParams struct {
	ClusterID    uuid.UUID
	NodeID       uuid.UUID
	Provider     string
	LBIdentifier string
}

// CreateClusterLBRegistration inserts (or upserts on the composite key) a
// membership row and clears any prior deregistered_at so re-registrations
// restore the active state.
func (s *Store) CreateClusterLBRegistration(ctx context.Context, params CreateClusterLBRegistrationParams) (*ClusterLBRegistration, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.ClusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	if params.NodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	provider := strings.TrimSpace(params.Provider)
	if provider == "" {
		return nil, errors.New("provider is required")
	}
	lbID := strings.TrimSpace(params.LBIdentifier)
	if lbID == "" {
		return nil, errors.New("lb_identifier is required")
	}

	now := s.clock()
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO cluster_lb_registrations (cluster_id, node_id, provider, lb_identifier, registered_at, deregistered_at)
		VALUES ($1, $2, $3, $4, $5, NULL)
		ON CONFLICT (cluster_id, node_id, lb_identifier)
		DO UPDATE SET provider = EXCLUDED.provider,
		              registered_at = EXCLUDED.registered_at,
		              deregistered_at = NULL
		RETURNING cluster_id, node_id, provider, lb_identifier, registered_at, deregistered_at
	`, params.ClusterID, params.NodeID, provider, lbID, now)

	return scanLBRegistration(row)
}

// MarkClusterLBRegistrationDeregistered flips deregistered_at for the given
// row. Returns sql.ErrNoRows if no such (cluster_id, node_id, lb_identifier)
// exists.
func (s *Store) MarkClusterLBRegistrationDeregistered(ctx context.Context, clusterID, nodeID uuid.UUID, lbIdentifier string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil || nodeID == uuid.Nil {
		return errors.New("cluster id and node id are required")
	}
	lbID := strings.TrimSpace(lbIdentifier)
	if lbID == "" {
		return errors.New("lb_identifier is required")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE cluster_lb_registrations
		SET deregistered_at = $4
		WHERE cluster_id = $1 AND node_id = $2 AND lb_identifier = $3
	`, clusterID, nodeID, lbID, s.clock())
	if err != nil {
		return fmt.Errorf("update cluster lb registration: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListClusterLBRegistrationsForNode returns every LB row attached to a node
// (historical + live). Ordered by registered_at desc so the caller can cheaply
// walk the drain trail.
func (s *Store) ListClusterLBRegistrationsForNode(ctx context.Context, nodeID uuid.UUID) ([]ClusterLBRegistration, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cluster_id, node_id, provider, lb_identifier, registered_at, deregistered_at
		FROM cluster_lb_registrations
		WHERE node_id = $1
		ORDER BY registered_at DESC
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query lb registrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ClusterLBRegistration
	for rows.Next() {
		reg, scanErr := scanLBRegistration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *reg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lb registrations: %w", err)
	}
	return out, nil
}

// ListClusterLBRegistrationsForCluster returns every LB row attached to a
// cluster — used by teardown to walk every node's LB membership in one query.
func (s *Store) ListClusterLBRegistrationsForCluster(ctx context.Context, clusterID uuid.UUID) ([]ClusterLBRegistration, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cluster_id, node_id, provider, lb_identifier, registered_at, deregistered_at
		FROM cluster_lb_registrations
		WHERE cluster_id = $1
		ORDER BY registered_at DESC
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query lb registrations by cluster: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ClusterLBRegistration
	for rows.Next() {
		reg, scanErr := scanLBRegistration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *reg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lb registrations: %w", err)
	}
	return out, nil
}

func scanLBRegistration(scanner rowScanner) (*ClusterLBRegistration, error) {
	var reg ClusterLBRegistration
	var deregistered sql.NullTime
	if err := scanner.Scan(
		&reg.ClusterID,
		&reg.NodeID,
		&reg.Provider,
		&reg.LBIdentifier,
		&reg.RegisteredAt,
		&deregistered,
	); err != nil {
		return nil, err
	}
	if deregistered.Valid {
		t := deregistered.Time
		reg.DeregisteredAt = &t
	}
	return &reg, nil
}
