package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NodePackage is one OS package observed on a node.
type NodePackage struct {
	NodeID      uuid.UUID
	Name        string
	Version     string
	Source      string // apt | dpkg | rpm | winget | other
	Arch        *string
	InstalledAt *time.Time
}

// NodeInventorySync tracks the last-known-good package inventory state for a
// node. The agent only resends the full os_packages list when its locally
// computed hash differs from the last_full_sync hash, or when 24h have passed
// since the last full sync — whichever comes first.
type NodeInventorySync struct {
	NodeID        uuid.UUID
	PackageHash   string
	PackageCount  int
	KernelVersion *string
	OSVersion     *string
	LastFullSync  time.Time
	LastSeenAt    time.Time
}

// ReplaceNodePackages atomically replaces the package set for a node. Used
// when the agent sends a full os_packages list (typically every 24h or after
// a hash mismatch). Skipped at the SQL level when packages is empty so a
// hash-only heartbeat does not nuke the existing inventory.
func (s *Store) ReplaceNodePackages(ctx context.Context, nodeID uuid.UUID, packages []NodePackage) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return errors.New("node id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_packages WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("delete node packages: %w", err)
	}

	if len(packages) > 0 {
		stmt, perr := tx.PrepareContext(ctx, `
			INSERT INTO node_packages (node_id, name, version, source, arch, installed_at, observed_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (node_id, name, version, arch) DO NOTHING
		`)
		if perr != nil {
			return fmt.Errorf("prepare insert: %w", perr)
		}
		defer func() { _ = stmt.Close() }()
		for _, p := range packages {
			if _, err := stmt.ExecContext(ctx, nodeID, p.Name, p.Version, p.Source, p.Arch, p.InstalledAt); err != nil {
				return fmt.Errorf("insert package %s/%s: %w", p.Name, p.Version, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ListNodePackages returns the full package inventory for a node.
func (s *Store) ListNodePackages(ctx context.Context, nodeID uuid.UUID) ([]NodePackage, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, name, version, source, arch, installed_at
		FROM node_packages
		WHERE node_id = $1
		ORDER BY name, version
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node packages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NodePackage
	for rows.Next() {
		var p NodePackage
		if err := rows.Scan(&p.NodeID, &p.Name, &p.Version, &p.Source, &p.Arch, &p.InstalledAt); err != nil {
			return nil, fmt.Errorf("scan node package: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetNodeInventorySync returns the sync state for a node, or nil if the node
// has never reported package inventory.
func (s *Store) GetNodeInventorySync(ctx context.Context, nodeID uuid.UUID) (*NodeInventorySync, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var sync NodeInventorySync
	err := s.db.QueryRowContext(ctx, `
		SELECT node_id, package_hash, package_count, kernel_version, os_version, last_full_sync, last_seen_at
		FROM node_inventory_sync
		WHERE node_id = $1
	`, nodeID).Scan(
		&sync.NodeID, &sync.PackageHash, &sync.PackageCount,
		&sync.KernelVersion, &sync.OSVersion, &sync.LastFullSync, &sync.LastSeenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get node inventory sync: %w", err)
	}
	return &sync, nil
}

// UpsertNodeInventorySync records a full-sync event: the agent has sent a
// fresh package list and we replaced the table. Hash + count + kernel/os
// version are captured for future delta comparisons.
func (s *Store) UpsertNodeInventorySync(ctx context.Context, sync NodeInventorySync) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if sync.NodeID == uuid.Nil {
		return errors.New("node id is required")
	}
	now := time.Now().UTC()
	if sync.LastFullSync.IsZero() {
		sync.LastFullSync = now
	}
	if sync.LastSeenAt.IsZero() {
		sync.LastSeenAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO node_inventory_sync
			(node_id, package_hash, package_count, kernel_version, os_version, last_full_sync, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (node_id) DO UPDATE SET
			package_hash   = EXCLUDED.package_hash,
			package_count  = EXCLUDED.package_count,
			kernel_version = EXCLUDED.kernel_version,
			os_version     = EXCLUDED.os_version,
			last_full_sync = EXCLUDED.last_full_sync,
			last_seen_at   = EXCLUDED.last_seen_at
	`,
		sync.NodeID, sync.PackageHash, sync.PackageCount,
		sync.KernelVersion, sync.OSVersion, sync.LastFullSync, sync.LastSeenAt,
	)
	if err != nil {
		return fmt.Errorf("upsert node inventory sync: %w", err)
	}
	return nil
}

// TouchNodeInventorySync updates only last_seen_at — used when the agent
// reports a hash that matches the stored value (no full sync needed). Returns
// (rowsAffected, error). If 0 rows are affected, the caller should request a
// full inventory from the agent.
func (s *Store) TouchNodeInventorySync(ctx context.Context, nodeID uuid.UUID, hash string) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE node_inventory_sync
		SET last_seen_at = NOW()
		WHERE node_id = $1 AND package_hash = $2
	`, nodeID, hash)
	if err != nil {
		return 0, fmt.Errorf("touch node inventory sync: %w", err)
	}
	return res.RowsAffected()
}
