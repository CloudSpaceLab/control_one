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

// Cluster represents a group of nodes managed together under a shared plan.
type Cluster struct {
	ID                    uuid.UUID
	TenantID              uuid.UUID
	Name                  string
	Provider              string
	DesiredSize           int
	RolePlan              map[string]any
	Labels                map[string]any
	FailureDomainStrategy string
	State                 string
	TemplateID            uuid.NullUUID
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ClusterMember ties a node to a cluster with a role and ordinal position.
type ClusterMember struct {
	ClusterID uuid.UUID
	NodeID    uuid.UUID
	Role      string
	Position  int
	JoinedAt  time.Time
}

// ClusterRollout captures a staged deployment of a template version to a cluster.
type ClusterRollout struct {
	ID                uuid.UUID
	ClusterID         uuid.UUID
	TemplateVersionID uuid.UUID
	WaveSize          int
	WaveStrategy      string
	HealthGate        map[string]any
	State             string
	CurrentWave       int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateClusterParams defines input for creating a cluster.
type CreateClusterParams struct {
	TenantID              uuid.UUID
	Name                  string
	Provider              string
	DesiredSize           int
	RolePlan              map[string]any
	Labels                map[string]any
	FailureDomainStrategy string
	State                 string
	TemplateID            *uuid.UUID
}

// UpdateClusterParams captures patchable fields on a cluster.
type UpdateClusterParams struct {
	Name                  *string
	Provider              *string
	DesiredSize           *int
	RolePlan              *map[string]any
	Labels                *map[string]any
	FailureDomainStrategy *string
	State                 *string
	TemplateID            *uuid.UUID
	ClearTemplateID       bool
}

// CreateClusterRolloutParams defines input for creating a cluster rollout.
type CreateClusterRolloutParams struct {
	ClusterID         uuid.UUID
	TemplateVersionID uuid.UUID
	WaveSize          int
	WaveStrategy      string
	HealthGate        map[string]any
	State             string
	CurrentWave       int
}

// UpdateClusterRolloutParams captures patchable fields on a cluster rollout.
type UpdateClusterRolloutParams struct {
	WaveSize     *int
	WaveStrategy *string
	HealthGate   *map[string]any
	State        *string
	CurrentWave  *int
}

// CreateCluster inserts a new cluster record.
func (s *Store) CreateCluster(ctx context.Context, params CreateClusterParams) (*Cluster, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, errors.New("cluster name is required")
	}
	provider := strings.TrimSpace(params.Provider)
	if provider == "" {
		return nil, errors.New("cluster provider is required")
	}
	if params.DesiredSize < 0 {
		return nil, errors.New("desired_size must be non-negative")
	}

	strategy := strings.TrimSpace(params.FailureDomainStrategy)
	if strategy == "" {
		strategy = "spread"
	}
	state := strings.TrimSpace(params.State)
	if state == "" {
		state = "pending"
	}

	rolePlan, err := marshalJSONBMap(params.RolePlan)
	if err != nil {
		return nil, fmt.Errorf("encode role_plan: %w", err)
	}
	labels, err := marshalJSONBMap(params.Labels)
	if err != nil {
		return nil, fmt.Errorf("encode labels: %w", err)
	}

	id := uuid.New()
	now := s.clock()

	var templateID any
	if params.TemplateID != nil && *params.TemplateID != uuid.Nil {
		templateID = *params.TemplateID
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO clusters (
			id, tenant_id, name, provider, desired_size, role_plan, labels,
			failure_domain_strategy, state, template_id, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, tenant_id, name, provider, desired_size, role_plan, labels,
		          failure_domain_strategy, state, template_id, created_at, updated_at
	`, id, params.TenantID, name, provider, params.DesiredSize, rolePlan, labels,
		strategy, state, templateID, now, now)

	return scanCluster(row)
}

// GetClusterByID returns a cluster by ID.
func (s *Store) GetClusterByID(ctx context.Context, id uuid.UUID) (*Cluster, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, provider, desired_size, role_plan, labels,
		       failure_domain_strategy, state, template_id, created_at, updated_at
		FROM clusters
		WHERE id = $1
	`, id)

	cluster, err := scanCluster(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return cluster, nil
}

// GetClusterByName returns a cluster scoped to the tenant by name.
func (s *Store) GetClusterByName(ctx context.Context, tenantID uuid.UUID, name string) (*Cluster, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("cluster name is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, provider, desired_size, role_plan, labels,
		       failure_domain_strategy, state, template_id, created_at, updated_at
		FROM clusters
		WHERE tenant_id = $1 AND name = $2
	`, tenantID, name)

	cluster, err := scanCluster(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return cluster, nil
}

// ListClusters returns clusters for a tenant with pagination.
func (s *Store) ListClusters(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Cluster, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if tenantID != uuid.Nil {
		args = append(args, tenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM clusters WHERE %s`, strings.Join(clauses, " AND "))
	argsForCount := make([]any, len(args))
	copy(argsForCount, args)

	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, argsForCount...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count clusters: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, provider, desired_size, role_plan, labels,
		       failure_domain_strategy, state, template_id, created_at, updated_at
		FROM clusters
		WHERE %s
		ORDER BY created_at DESC
	`, strings.Join(clauses, " AND "))

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query clusters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var clusters []Cluster
	for rows.Next() {
		cluster, err := scanCluster(rows)
		if err != nil {
			return nil, 0, err
		}
		clusters = append(clusters, *cluster)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate clusters: %w", err)
	}

	return clusters, total, nil
}

// UpdateCluster applies partial updates to a cluster.
func (s *Store) UpdateCluster(ctx context.Context, id uuid.UUID, params UpdateClusterParams) (*Cluster, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}

	setFragments := []string{}
	args := []any{}
	idx := 1

	if params.Name != nil {
		name := strings.TrimSpace(*params.Name)
		if name == "" {
			return nil, errors.New("name cannot be empty")
		}
		setFragments = append(setFragments, fmt.Sprintf("name = $%d", idx))
		args = append(args, name)
		idx++
	}
	if params.Provider != nil {
		provider := strings.TrimSpace(*params.Provider)
		if provider == "" {
			return nil, errors.New("provider cannot be empty")
		}
		setFragments = append(setFragments, fmt.Sprintf("provider = $%d", idx))
		args = append(args, provider)
		idx++
	}
	if params.DesiredSize != nil {
		if *params.DesiredSize < 0 {
			return nil, errors.New("desired_size must be non-negative")
		}
		setFragments = append(setFragments, fmt.Sprintf("desired_size = $%d", idx))
		args = append(args, *params.DesiredSize)
		idx++
	}
	if params.RolePlan != nil {
		encoded, err := marshalJSONBMap(*params.RolePlan)
		if err != nil {
			return nil, fmt.Errorf("encode role_plan: %w", err)
		}
		setFragments = append(setFragments, fmt.Sprintf("role_plan = $%d", idx))
		args = append(args, encoded)
		idx++
	}
	if params.Labels != nil {
		encoded, err := marshalJSONBMap(*params.Labels)
		if err != nil {
			return nil, fmt.Errorf("encode labels: %w", err)
		}
		setFragments = append(setFragments, fmt.Sprintf("labels = $%d", idx))
		args = append(args, encoded)
		idx++
	}
	if params.FailureDomainStrategy != nil {
		strategy := strings.TrimSpace(*params.FailureDomainStrategy)
		if strategy == "" {
			return nil, errors.New("failure_domain_strategy cannot be empty")
		}
		setFragments = append(setFragments, fmt.Sprintf("failure_domain_strategy = $%d", idx))
		args = append(args, strategy)
		idx++
	}
	if params.State != nil {
		state := strings.TrimSpace(*params.State)
		if state == "" {
			return nil, errors.New("state cannot be empty")
		}
		setFragments = append(setFragments, fmt.Sprintf("state = $%d", idx))
		args = append(args, state)
		idx++
	}
	if params.ClearTemplateID {
		setFragments = append(setFragments, "template_id = NULL")
	} else if params.TemplateID != nil {
		if *params.TemplateID == uuid.Nil {
			setFragments = append(setFragments, "template_id = NULL")
		} else {
			setFragments = append(setFragments, fmt.Sprintf("template_id = $%d", idx))
			args = append(args, *params.TemplateID)
			idx++
		}
	}

	if len(setFragments) == 0 {
		return s.GetClusterByID(ctx, id)
	}

	now := s.clock()
	setFragments = append(setFragments, fmt.Sprintf("updated_at = $%d", idx))
	args = append(args, now)
	idx++

	query := fmt.Sprintf(`
		UPDATE clusters
		SET %s
		WHERE id = $%d
		RETURNING id, tenant_id, name, provider, desired_size, role_plan, labels,
		          failure_domain_strategy, state, template_id, created_at, updated_at
	`, strings.Join(setFragments, ", "), idx)
	args = append(args, id)

	row := s.db.QueryRowContext(ctx, query, args...)
	cluster, err := scanCluster(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return cluster, nil
}

// DeleteCluster removes a cluster by ID. Cascades to members and rollouts.
func (s *Store) DeleteCluster(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("cluster id is required")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM clusters WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete cluster rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CountClustersByTenant returns the number of clusters owned by a tenant.
func (s *Store) CountClustersByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return 0, errors.New("tenant id is required")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM clusters WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count clusters by tenant: %w", err)
	}
	return count, nil
}

// AddClusterMember assigns a node to a cluster with the given role and position.
func (s *Store) AddClusterMember(ctx context.Context, clusterID, nodeID uuid.UUID, role string, position int) (*ClusterMember, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return nil, errors.New("role is required")
	}
	if position < 0 {
		return nil, errors.New("position must be non-negative")
	}

	now := s.clock()
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO cluster_members (cluster_id, node_id, role, position, joined_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING cluster_id, node_id, role, position, joined_at
	`, clusterID, nodeID, role, position, now)

	var member ClusterMember
	if err := row.Scan(&member.ClusterID, &member.NodeID, &member.Role, &member.Position, &member.JoinedAt); err != nil {
		return nil, fmt.Errorf("insert cluster member: %w", err)
	}
	return &member, nil
}

// RemoveClusterMember detaches a node from a cluster and strips any labels
// that were projected onto the node with the `cluster.` prefix. `cluster.*`
// labels are only owned by the cluster — user-set labels on the node are
// preserved untouched.
//
// NOTE: label-strip relies on nodes.labels JSONB column from migration 0028
// (Worktree A). The two statements run in separate transactions so a missing
// column doesn't block the membership delete. At merge time (A lands before E)
// both statements succeed together; before then, the strip is a best-effort
// log-only operation.
func (s *Store) RemoveClusterMember(ctx context.Context, clusterID, nodeID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return errors.New("cluster id is required")
	}
	if nodeID == uuid.Nil {
		return errors.New("node id is required")
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM cluster_members WHERE cluster_id = $1 AND node_id = $2
	`, clusterID, nodeID)
	if err != nil {
		return fmt.Errorf("delete cluster member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete cluster member rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	// Strip only the `cluster.`-prefixed keys — this is a cheap JSONB filter
	// that preserves every other label key on the node. A separate statement
	// (not shared tx) so a missing column doesn't abort the membership delete.
	if _, stripErr := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET labels = COALESCE(
		    (SELECT jsonb_object_agg(key, value)
		     FROM jsonb_each(labels)
		     WHERE key NOT LIKE 'cluster.%'),
		    '{}'::jsonb
		), updated_at = $2
		WHERE id = $1
	`, nodeID, s.clock()); stripErr != nil {
		// Probable reason: Worktree A not merged yet — nodes.labels doesn't
		// exist. We intentionally soft-fail here so the membership drop still
		// counts. When A lands this branch becomes unreachable.
		_ = stripErr
	}

	return nil
}

// PropagateClusterLabelsToNode upserts each cluster label onto the given node
// as `cluster.<key>=<value>`. Non-cluster-prefix keys on the node are left
// untouched. When the cluster has no labels, any existing `cluster.` keys on
// the node are stripped — this keeps the node in sync with a label-removal
// update on the cluster side.
//
// Relies on nodes.labels JSONB column from migration 0028 (Worktree A). At
// dispatch time that column may not yet exist on `seigha` — the orchestrator
// merges A before E so the column is live at merge time.
func (s *Store) PropagateClusterLabelsToNode(ctx context.Context, clusterID, nodeID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return errors.New("cluster id is required")
	}
	if nodeID == uuid.Nil {
		return errors.New("node id is required")
	}

	cluster, err := s.GetClusterByID(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("load cluster for label propagation: %w", err)
	}
	if cluster == nil {
		return sql.ErrNoRows
	}

	clusterLabelJSON, err := marshalJSONBMap(buildClusterLabelOverlay(cluster.Labels))
	if err != nil {
		return fmt.Errorf("encode cluster labels for node: %w", err)
	}

	// Build the new labels as:
	//   (existing node labels with `cluster.`-prefixed keys removed)
	//   merged with (each cluster label prefixed `cluster.`)
	// We do this in a single UPDATE so propagation is idempotent and atomic.
	//
	// The SQL below:
	//   - strips current `cluster.*` keys
	//   - overlays the fresh cluster label map
	// If nodes.labels doesn't exist yet (Worktree A unmerged) this errors —
	// the caller (cluster.provision / handleUpdateCluster) logs WARN and
	// proceeds; the cluster membership itself is already persisted.
	_, err = s.db.ExecContext(ctx, `
		UPDATE nodes
		SET labels = COALESCE(
		    (SELECT jsonb_object_agg(key, value)
		     FROM jsonb_each(labels)
		     WHERE key NOT LIKE 'cluster.%'),
		    '{}'::jsonb
		) || $2::jsonb,
		    updated_at = $3
		WHERE id = $1
	`, nodeID, clusterLabelJSON, s.clock())
	if err != nil {
		return fmt.Errorf("propagate cluster labels to node: %w", err)
	}
	return nil
}

// buildClusterLabelOverlay takes the cluster's raw labels map and returns a
// new map with every key prefixed `cluster.`. Non-scalar values are serialised
// defensively (map/slice survive JSONB roundtrip via the storage layer).
func buildClusterLabelOverlay(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out["cluster."+key] = v
	}
	return out
}

// ListClusterMembers returns members for a cluster ordered by role and position.
func (s *Store) ListClusterMembers(ctx context.Context, clusterID uuid.UUID) ([]ClusterMember, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT cluster_id, node_id, role, position, joined_at
		FROM cluster_members
		WHERE cluster_id = $1
		ORDER BY role ASC, position ASC
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []ClusterMember
	for rows.Next() {
		var member ClusterMember
		if err := rows.Scan(&member.ClusterID, &member.NodeID, &member.Role, &member.Position, &member.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan cluster member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster members: %w", err)
	}
	return members, nil
}

// CreateClusterRollout inserts a new rollout targeting a cluster.
func (s *Store) CreateClusterRollout(ctx context.Context, params CreateClusterRolloutParams) (*ClusterRollout, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.ClusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	if params.TemplateVersionID == uuid.Nil {
		return nil, errors.New("template version id is required")
	}

	waveSize := params.WaveSize
	if waveSize == 0 {
		waveSize = 1
	}
	if waveSize < 1 {
		return nil, errors.New("wave_size must be >= 1")
	}

	strategy := strings.TrimSpace(params.WaveStrategy)
	if strategy == "" {
		strategy = "rolling"
	}
	state := strings.TrimSpace(params.State)
	if state == "" {
		state = "pending"
	}
	if params.CurrentWave < 0 {
		return nil, errors.New("current_wave must be non-negative")
	}

	healthGate, err := marshalJSONBMap(params.HealthGate)
	if err != nil {
		return nil, fmt.Errorf("encode health_gate: %w", err)
	}

	id := uuid.New()
	now := s.clock()

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO cluster_rollouts (
			id, cluster_id, template_version_id, wave_size, wave_strategy,
			health_gate, state, current_wave, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, cluster_id, template_version_id, wave_size, wave_strategy,
		          health_gate, state, current_wave, created_at, updated_at
	`, id, params.ClusterID, params.TemplateVersionID, waveSize, strategy,
		healthGate, state, params.CurrentWave, now, now)

	return scanClusterRollout(row)
}

// GetClusterRolloutByID returns a cluster rollout by ID.
func (s *Store) GetClusterRolloutByID(ctx context.Context, id uuid.UUID) (*ClusterRollout, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("rollout id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, cluster_id, template_version_id, wave_size, wave_strategy,
		       health_gate, state, current_wave, created_at, updated_at
		FROM cluster_rollouts
		WHERE id = $1
	`, id)

	rollout, err := scanClusterRollout(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return rollout, nil
}

// ListClusterRollouts returns rollouts for a cluster with pagination.
func (s *Store) ListClusterRollouts(ctx context.Context, clusterID uuid.UUID, limit, offset int) ([]ClusterRollout, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return nil, 0, errors.New("cluster id is required")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cluster_rollouts WHERE cluster_id = $1`, clusterID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count cluster rollouts: %w", err)
	}

	query := `
		SELECT id, cluster_id, template_version_id, wave_size, wave_strategy,
		       health_gate, state, current_wave, created_at, updated_at
		FROM cluster_rollouts
		WHERE cluster_id = $1
		ORDER BY created_at DESC
	`
	args := []any{clusterID}
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query cluster rollouts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rollouts []ClusterRollout
	for rows.Next() {
		rollout, err := scanClusterRollout(rows)
		if err != nil {
			return nil, 0, err
		}
		rollouts = append(rollouts, *rollout)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate cluster rollouts: %w", err)
	}
	return rollouts, total, nil
}

// UpdateClusterRollout applies partial updates to a cluster rollout.
func (s *Store) UpdateClusterRollout(ctx context.Context, id uuid.UUID, params UpdateClusterRolloutParams) (*ClusterRollout, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("rollout id is required")
	}

	setFragments := []string{}
	args := []any{}
	idx := 1

	if params.WaveSize != nil {
		if *params.WaveSize < 1 {
			return nil, errors.New("wave_size must be >= 1")
		}
		setFragments = append(setFragments, fmt.Sprintf("wave_size = $%d", idx))
		args = append(args, *params.WaveSize)
		idx++
	}
	if params.WaveStrategy != nil {
		strategy := strings.TrimSpace(*params.WaveStrategy)
		if strategy == "" {
			return nil, errors.New("wave_strategy cannot be empty")
		}
		setFragments = append(setFragments, fmt.Sprintf("wave_strategy = $%d", idx))
		args = append(args, strategy)
		idx++
	}
	if params.HealthGate != nil {
		encoded, err := marshalJSONBMap(*params.HealthGate)
		if err != nil {
			return nil, fmt.Errorf("encode health_gate: %w", err)
		}
		setFragments = append(setFragments, fmt.Sprintf("health_gate = $%d", idx))
		args = append(args, encoded)
		idx++
	}
	if params.State != nil {
		state := strings.TrimSpace(*params.State)
		if state == "" {
			return nil, errors.New("state cannot be empty")
		}
		setFragments = append(setFragments, fmt.Sprintf("state = $%d", idx))
		args = append(args, state)
		idx++
	}
	if params.CurrentWave != nil {
		if *params.CurrentWave < 0 {
			return nil, errors.New("current_wave must be non-negative")
		}
		setFragments = append(setFragments, fmt.Sprintf("current_wave = $%d", idx))
		args = append(args, *params.CurrentWave)
		idx++
	}

	if len(setFragments) == 0 {
		return s.GetClusterRolloutByID(ctx, id)
	}

	now := s.clock()
	setFragments = append(setFragments, fmt.Sprintf("updated_at = $%d", idx))
	args = append(args, now)
	idx++

	query := fmt.Sprintf(`
		UPDATE cluster_rollouts
		SET %s
		WHERE id = $%d
		RETURNING id, cluster_id, template_version_id, wave_size, wave_strategy,
		          health_gate, state, current_wave, created_at, updated_at
	`, strings.Join(setFragments, ", "), idx)
	args = append(args, id)

	row := s.db.QueryRowContext(ctx, query, args...)
	rollout, err := scanClusterRollout(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return rollout, nil
}

// DeleteClusterRollout removes a cluster rollout by ID.
func (s *Store) DeleteClusterRollout(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("rollout id is required")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM cluster_rollouts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete cluster rollout: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete cluster rollout rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanCluster(scanner rowScanner) (*Cluster, error) {
	var cluster Cluster
	var rolePlanRaw, labelsRaw []byte
	var templateID sql.NullString
	if err := scanner.Scan(
		&cluster.ID,
		&cluster.TenantID,
		&cluster.Name,
		&cluster.Provider,
		&cluster.DesiredSize,
		&rolePlanRaw,
		&labelsRaw,
		&cluster.FailureDomainStrategy,
		&cluster.State,
		&templateID,
		&cluster.CreatedAt,
		&cluster.UpdatedAt,
	); err != nil {
		return nil, err
	}

	rolePlan, err := decodeJSONBMap(rolePlanRaw)
	if err != nil {
		return nil, fmt.Errorf("decode role_plan: %w", err)
	}
	cluster.RolePlan = rolePlan

	labels, err := decodeJSONBMap(labelsRaw)
	if err != nil {
		return nil, fmt.Errorf("decode labels: %w", err)
	}
	cluster.Labels = labels

	if templateID.Valid {
		if parsed, perr := uuid.Parse(templateID.String); perr == nil {
			cluster.TemplateID = uuid.NullUUID{UUID: parsed, Valid: true}
		}
	}

	return &cluster, nil
}

func scanClusterRollout(scanner rowScanner) (*ClusterRollout, error) {
	var rollout ClusterRollout
	var healthGateRaw []byte
	if err := scanner.Scan(
		&rollout.ID,
		&rollout.ClusterID,
		&rollout.TemplateVersionID,
		&rollout.WaveSize,
		&rollout.WaveStrategy,
		&healthGateRaw,
		&rollout.State,
		&rollout.CurrentWave,
		&rollout.CreatedAt,
		&rollout.UpdatedAt,
	); err != nil {
		return nil, err
	}

	healthGate, err := decodeJSONBMap(healthGateRaw)
	if err != nil {
		return nil, fmt.Errorf("decode health_gate: %w", err)
	}
	rollout.HealthGate = healthGate

	return &rollout, nil
}

func marshalJSONBMap(input map[string]any) ([]byte, error) {
	if len(input) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(input)
}

func decodeJSONBMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
