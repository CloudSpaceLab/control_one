package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// NodeAppDependency is one application/library dependency observed under an
// app root or imported from an SBOM. It complements OS package inventory so CVE
// matching can cover npm/PyPI/Go/Maven/NuGet-style software.
type NodeAppDependency struct {
	ID             uuid.UUID      `json:"id"`
	NodeID         uuid.UUID      `json:"node_id"`
	TenantID       uuid.UUID      `json:"tenant_id"`
	AppRoot        string         `json:"app_root"`
	Ecosystem      string         `json:"ecosystem"`
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	PackageManager string         `json:"package_manager"`
	ManifestPath   string         `json:"manifest_path"`
	Scope          string         `json:"scope"`
	License        string         `json:"license"`
	PURL           string         `json:"purl"`
	CPE            string         `json:"cpe"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ObservedAt     time.Time      `json:"observed_at"`
}

func (s *Store) ReplaceNodeAppDependencies(ctx context.Context, nodeID, tenantID uuid.UUID, deps []NodeAppDependency) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil || tenantID == uuid.Nil {
		return errors.New("node and tenant id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin app dependencies tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_app_dependencies WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("delete node app dependencies: %w", err)
	}
	if len(deps) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO node_app_dependencies (
				node_id, tenant_id, app_root, ecosystem, name, version, package_manager,
				manifest_path, scope, license, purl, cpe, metadata, observed_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
			ON CONFLICT (node_id, app_root, ecosystem, name, version, manifest_path) DO NOTHING
		`)
		if err != nil {
			return fmt.Errorf("prepare app dependency insert: %w", err)
		}
		defer func() { _ = stmt.Close() }()
		for _, dep := range deps {
			dep = normalizeNodeAppDependency(nodeID, tenantID, dep)
			if dep.Name == "" || dep.Ecosystem == "" {
				continue
			}
			metadata, err := json.Marshal(dep.Metadata)
			if err != nil {
				return fmt.Errorf("marshal app dependency metadata for %s/%s: %w", dep.Ecosystem, dep.Name, err)
			}
			if _, err := stmt.ExecContext(ctx,
				nodeID, tenantID, dep.AppRoot, dep.Ecosystem, dep.Name, dep.Version,
				dep.PackageManager, dep.ManifestPath, dep.Scope, dep.License, dep.PURL,
				dep.CPE, metadata,
			); err != nil {
				return fmt.Errorf("insert app dependency %s/%s: %w", dep.Ecosystem, dep.Name, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit app dependencies tx: %w", err)
	}
	return nil
}

func (s *Store) ListNodeAppDependencies(ctx context.Context, nodeID uuid.UUID) ([]NodeAppDependency, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_id, tenant_id, app_root, ecosystem, name, version, package_manager,
		       manifest_path, scope, license, purl, cpe, metadata, observed_at
		FROM node_app_dependencies
		WHERE node_id = $1
		ORDER BY app_root, ecosystem, name, version
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node app dependencies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []NodeAppDependency
	for rows.Next() {
		dep, err := scanNodeAppDependency(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *dep)
	}
	return out, rows.Err()
}

func normalizeNodeAppDependency(nodeID, tenantID uuid.UUID, in NodeAppDependency) NodeAppDependency {
	in.NodeID = nodeID
	in.TenantID = tenantID
	in.AppRoot = strings.TrimSpace(in.AppRoot)
	in.Ecosystem = normalizeDependencyEcosystem(in.Ecosystem)
	in.Name = strings.TrimSpace(in.Name)
	in.Version = strings.TrimSpace(in.Version)
	in.PackageManager = strings.TrimSpace(in.PackageManager)
	in.ManifestPath = strings.TrimSpace(in.ManifestPath)
	in.Scope = strings.TrimSpace(in.Scope)
	in.License = strings.TrimSpace(in.License)
	in.PURL = strings.TrimSpace(in.PURL)
	in.CPE = strings.TrimSpace(in.CPE)
	if in.Metadata == nil {
		in.Metadata = map[string]any{}
	}
	return in
}

func normalizeDependencyEcosystem(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "javascript", "node", "nodejs", "npmjs":
		return "npm"
	case "python", "pip":
		return "pypi"
	case "golang", "gomod":
		return "go"
	case "dotnet":
		return "nuget"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func scanNodeAppDependency(row interface {
	Scan(dest ...any) error
}) (*NodeAppDependency, error) {
	var out NodeAppDependency
	var metadataRaw []byte
	if err := row.Scan(
		&out.ID,
		&out.NodeID,
		&out.TenantID,
		&out.AppRoot,
		&out.Ecosystem,
		&out.Name,
		&out.Version,
		&out.PackageManager,
		&out.ManifestPath,
		&out.Scope,
		&out.License,
		&out.PURL,
		&out.CPE,
		&metadataRaw,
		&out.ObservedAt,
	); err != nil {
		return nil, fmt.Errorf("scan node app dependency: %w", err)
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &out.Metadata); err != nil {
			return nil, fmt.Errorf("decode app dependency metadata: %w", err)
		}
	}
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	return &out, nil
}
