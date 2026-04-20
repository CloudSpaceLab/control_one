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
	"github.com/lib/pq"
)

// ClusterRolloutWaveState enumerates the lifecycle states of a rollout wave.
const (
	ClusterRolloutWaveStateRunning   = "running"
	ClusterRolloutWaveStateHealthy   = "healthy"
	ClusterRolloutWaveStateUnhealthy = "unhealthy"
	ClusterRolloutWaveStateAborted   = "aborted"
)

// ClusterRolloutWave captures a single wave of a cluster rollout along with
// the nodes that received the new template version and the outcome of the
// post-wave health gate.
type ClusterRolloutWave struct {
	ID          uuid.UUID
	RolloutID   uuid.UUID
	WaveNumber  int
	MemberIDs   []uuid.UUID
	StartedAt   time.Time
	CompletedAt *time.Time
	State       string
	GateResult  map[string]any
}

// CreateClusterRolloutWaveParams defines input for recording a new wave.
type CreateClusterRolloutWaveParams struct {
	RolloutID  uuid.UUID
	WaveNumber int
	MemberIDs  []uuid.UUID
	State      string
	StartedAt  time.Time
	GateResult map[string]any
}

// UpdateClusterRolloutWaveParams captures patchable fields on a wave.
type UpdateClusterRolloutWaveParams struct {
	State       *string
	GateResult  *map[string]any
	CompletedAt *time.Time
}

// CreateClusterRolloutWave inserts a new rollout wave row.
func (s *Store) CreateClusterRolloutWave(ctx context.Context, params CreateClusterRolloutWaveParams) (*ClusterRolloutWave, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.RolloutID == uuid.Nil {
		return nil, errors.New("rollout id is required")
	}
	if params.WaveNumber < 0 {
		return nil, errors.New("wave_number must be non-negative")
	}

	state := strings.TrimSpace(params.State)
	if state == "" {
		state = ClusterRolloutWaveStateRunning
	}

	memberIDs := make([]string, 0, len(params.MemberIDs))
	for _, id := range params.MemberIDs {
		if id == uuid.Nil {
			continue
		}
		memberIDs = append(memberIDs, id.String())
	}

	startedAt := params.StartedAt
	if startedAt.IsZero() {
		startedAt = s.clock()
	}

	var gateResult any
	if params.GateResult != nil {
		encoded, err := json.Marshal(params.GateResult)
		if err != nil {
			return nil, fmt.Errorf("encode gate_result: %w", err)
		}
		gateResult = encoded
	}

	id := uuid.New()
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO cluster_rollout_waves (
			id, rollout_id, wave_number, member_ids, state, started_at, gate_result
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, rollout_id, wave_number, member_ids, started_at, completed_at, state, gate_result
	`, id, params.RolloutID, params.WaveNumber, pq.Array(memberIDs), state, startedAt, gateResult)

	return scanClusterRolloutWave(row)
}

// GetClusterRolloutWave returns a wave by ID.
func (s *Store) GetClusterRolloutWave(ctx context.Context, id uuid.UUID) (*ClusterRolloutWave, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("wave id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, rollout_id, wave_number, member_ids, started_at, completed_at, state, gate_result
		FROM cluster_rollout_waves
		WHERE id = $1
	`, id)
	wave, err := scanClusterRolloutWave(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return wave, nil
}

// GetClusterRolloutWaveByNumber returns a wave by (rollout_id, wave_number).
func (s *Store) GetClusterRolloutWaveByNumber(ctx context.Context, rolloutID uuid.UUID, waveNumber int) (*ClusterRolloutWave, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if rolloutID == uuid.Nil {
		return nil, errors.New("rollout id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, rollout_id, wave_number, member_ids, started_at, completed_at, state, gate_result
		FROM cluster_rollout_waves
		WHERE rollout_id = $1 AND wave_number = $2
	`, rolloutID, waveNumber)
	wave, err := scanClusterRolloutWave(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return wave, nil
}

// ListClusterRolloutWaves returns every wave associated with a rollout in
// ascending wave_number order.
func (s *Store) ListClusterRolloutWaves(ctx context.Context, rolloutID uuid.UUID) ([]ClusterRolloutWave, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if rolloutID == uuid.Nil {
		return nil, errors.New("rollout id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, rollout_id, wave_number, member_ids, started_at, completed_at, state, gate_result
		FROM cluster_rollout_waves
		WHERE rollout_id = $1
		ORDER BY wave_number ASC
	`, rolloutID)
	if err != nil {
		return nil, fmt.Errorf("query cluster rollout waves: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var waves []ClusterRolloutWave
	for rows.Next() {
		wave, err := scanClusterRolloutWave(rows)
		if err != nil {
			return nil, err
		}
		waves = append(waves, *wave)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster rollout waves: %w", err)
	}
	return waves, nil
}

// UpdateClusterRolloutWave applies partial updates to a rollout wave.
func (s *Store) UpdateClusterRolloutWave(ctx context.Context, id uuid.UUID, params UpdateClusterRolloutWaveParams) (*ClusterRolloutWave, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("wave id is required")
	}

	setFragments := []string{}
	args := []any{}
	idx := 1

	if params.State != nil {
		state := strings.TrimSpace(*params.State)
		if state == "" {
			return nil, errors.New("state cannot be empty")
		}
		setFragments = append(setFragments, fmt.Sprintf("state = $%d", idx))
		args = append(args, state)
		idx++
	}
	if params.GateResult != nil {
		encoded, err := json.Marshal(*params.GateResult)
		if err != nil {
			return nil, fmt.Errorf("encode gate_result: %w", err)
		}
		setFragments = append(setFragments, fmt.Sprintf("gate_result = $%d", idx))
		args = append(args, encoded)
		idx++
	}
	if params.CompletedAt != nil {
		setFragments = append(setFragments, fmt.Sprintf("completed_at = $%d", idx))
		args = append(args, *params.CompletedAt)
		idx++
	}

	if len(setFragments) == 0 {
		return s.GetClusterRolloutWave(ctx, id)
	}

	query := fmt.Sprintf(`
		UPDATE cluster_rollout_waves
		SET %s
		WHERE id = $%d
		RETURNING id, rollout_id, wave_number, member_ids, started_at, completed_at, state, gate_result
	`, strings.Join(setFragments, ", "), idx)
	args = append(args, id)

	row := s.db.QueryRowContext(ctx, query, args...)
	wave, err := scanClusterRolloutWave(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return wave, nil
}

func scanClusterRolloutWave(scanner rowScanner) (*ClusterRolloutWave, error) {
	var wave ClusterRolloutWave
	var rawMembers pq.StringArray
	var completedAt sql.NullTime
	var gateRaw []byte

	if err := scanner.Scan(
		&wave.ID,
		&wave.RolloutID,
		&wave.WaveNumber,
		&rawMembers,
		&wave.StartedAt,
		&completedAt,
		&wave.State,
		&gateRaw,
	); err != nil {
		return nil, err
	}

	wave.MemberIDs = make([]uuid.UUID, 0, len(rawMembers))
	for _, raw := range rawMembers {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("decode member id %q: %w", raw, err)
		}
		wave.MemberIDs = append(wave.MemberIDs, parsed)
	}

	if completedAt.Valid {
		completed := completedAt.Time
		wave.CompletedAt = &completed
	}

	if len(gateRaw) > 0 {
		gate, err := decodeJSONBMap(gateRaw)
		if err != nil {
			return nil, fmt.Errorf("decode gate_result: %w", err)
		}
		wave.GateResult = gate
	}

	return &wave, nil
}
