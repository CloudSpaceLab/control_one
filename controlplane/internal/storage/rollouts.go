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

// TemplateRollout represents a rollout of a template version.
type TemplateRollout struct {
	ID                uuid.UUID
	TemplateVersionID uuid.UUID
	TargetPercent     int
	State             string
	Metadata          map[string]any
	ScheduledFor      sql.NullTime
	CompletedAt       sql.NullTime
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateRolloutParams defines input for creating a rollout.
type CreateRolloutParams struct {
	TemplateVersionID uuid.UUID
	TargetPercent     int
	State             string
	Metadata          map[string]any
	ScheduledFor      *time.Time
}

// UpdateRolloutParams captures patchable fields on a rollout.
type UpdateRolloutParams struct {
	State         *string
	TargetPercent *int
	Metadata      *map[string]any
	CompletedAt   *time.Time
}

// ListRollouts returns rollouts for a template.
func (s *Store) ListRollouts(ctx context.Context, templateID uuid.UUID, limit, offset int) ([]TemplateRollout, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if templateID == uuid.Nil {
		return nil, 0, errors.New("template id is required")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	countQuery := `
		SELECT COUNT(*)
		FROM provisioning_template_rollouts r
		JOIN provisioning_template_versions v ON r.template_version_id = v.id
		WHERE v.template_id = $1
	`
	countRow := s.db.QueryRowContext(ctx, countQuery, templateID)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count rollouts: %w", err)
	}

	query := `
		SELECT r.id, r.template_version_id, r.target_percent, r.state, r.metadata,
		       r.scheduled_for, r.completed_at, r.created_at, r.updated_at
		FROM provisioning_template_rollouts r
		JOIN provisioning_template_versions v ON r.template_version_id = v.id
		WHERE v.template_id = $1
		ORDER BY r.created_at DESC
	`
	args := []any{templateID}
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
		return nil, 0, fmt.Errorf("query rollouts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rollouts []TemplateRollout
	for rows.Next() {
		var r TemplateRollout
		var metadataRaw []byte
		if err := rows.Scan(
			&r.ID,
			&r.TemplateVersionID,
			&r.TargetPercent,
			&r.State,
			&metadataRaw,
			&r.ScheduledFor,
			&r.CompletedAt,
			&r.CreatedAt,
			&r.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan rollout: %w", err)
		}
		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &r.Metadata); err != nil {
				return nil, 0, fmt.Errorf("decode metadata: %w", err)
			}
		}
		if r.Metadata == nil {
			r.Metadata = make(map[string]any)
		}
		rollouts = append(rollouts, r)
	}

	return rollouts, total, nil
}

// GetRollout returns a rollout by ID.
func (s *Store) GetRollout(ctx context.Context, rolloutID uuid.UUID) (*TemplateRollout, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if rolloutID == uuid.Nil {
		return nil, errors.New("rollout id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, template_version_id, target_percent, state, metadata,
		       scheduled_for, completed_at, created_at, updated_at
		FROM provisioning_template_rollouts
		WHERE id = $1
	`, rolloutID)

	var r TemplateRollout
	var metadataRaw []byte
	if err := row.Scan(
		&r.ID,
		&r.TemplateVersionID,
		&r.TargetPercent,
		&r.State,
		&metadataRaw,
		&r.ScheduledFor,
		&r.CompletedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get rollout: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &r.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}

	return &r, nil
}

// CreateRollout creates a new rollout.
func (s *Store) CreateRollout(ctx context.Context, params CreateRolloutParams) (*TemplateRollout, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.TemplateVersionID == uuid.Nil {
		return nil, errors.New("template version id is required")
	}
	if params.TargetPercent < 0 || params.TargetPercent > 100 {
		return nil, errors.New("target_percent must be between 0 and 100")
	}
	params.State = strings.TrimSpace(params.State)
	if params.State == "" {
		params.State = "scheduled"
	}

	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	id := uuid.New()
	now := s.clock()
	scheduledFor := sql.NullTime{}
	if params.ScheduledFor != nil {
		scheduledFor = sql.NullTime{Time: *params.ScheduledFor, Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO provisioning_template_rollouts (
			id, template_version_id, target_percent, state, metadata,
			scheduled_for, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, template_version_id, target_percent, state, metadata,
		          scheduled_for, completed_at, created_at, updated_at
	`, id, params.TemplateVersionID, params.TargetPercent, params.State, metadataJSON, scheduledFor, now, now)

	var r TemplateRollout
	var metadataRaw []byte
	if err := row.Scan(
		&r.ID,
		&r.TemplateVersionID,
		&r.TargetPercent,
		&r.State,
		&metadataRaw,
		&r.ScheduledFor,
		&r.CompletedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create rollout: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &r.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}

	return &r, nil
}

// UpdateRollout updates a rollout.
func (s *Store) UpdateRollout(ctx context.Context, rolloutID uuid.UUID, params UpdateRolloutParams) (*TemplateRollout, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if rolloutID == uuid.Nil {
		return nil, errors.New("rollout id is required")
	}

	updates := []string{}
	args := []any{rolloutID}
	argPos := 2

	if params.State != nil {
		state := strings.TrimSpace(*params.State)
		if state == "" {
			return nil, errors.New("state cannot be empty")
		}
		args = append(args, state)
		updates = append(updates, fmt.Sprintf("state = $%d", argPos))
		argPos++
	}
	if params.TargetPercent != nil {
		if *params.TargetPercent < 0 || *params.TargetPercent > 100 {
			return nil, errors.New("target_percent must be between 0 and 100")
		}
		args = append(args, *params.TargetPercent)
		updates = append(updates, fmt.Sprintf("target_percent = $%d", argPos))
		argPos++
	}
	if params.Metadata != nil {
		metadataJSON, err := json.Marshal(*params.Metadata)
		if err != nil {
			return nil, fmt.Errorf("encode metadata: %w", err)
		}
		args = append(args, metadataJSON)
		updates = append(updates, fmt.Sprintf("metadata = $%d", argPos))
		argPos++
	}
	if params.CompletedAt != nil {
		completedAt := sql.NullTime{Time: *params.CompletedAt, Valid: true}
		args = append(args, completedAt)
		updates = append(updates, fmt.Sprintf("completed_at = $%d", argPos))
		argPos++
	}

	if len(updates) == 0 {
		return s.GetRollout(ctx, rolloutID)
	}

	updates = append(updates, fmt.Sprintf("updated_at = $%d", argPos))
	args = append(args, s.clock())

	query := fmt.Sprintf(`
		UPDATE provisioning_template_rollouts
		SET %s
		WHERE id = $1
		RETURNING id, template_version_id, target_percent, state, metadata,
		          scheduled_for, completed_at, created_at, updated_at
	`, strings.Join(updates, ", "))

	row := s.db.QueryRowContext(ctx, query, args...)

	var r TemplateRollout
	var metadataRaw []byte
	if err := row.Scan(
		&r.ID,
		&r.TemplateVersionID,
		&r.TargetPercent,
		&r.State,
		&metadataRaw,
		&r.ScheduledFor,
		&r.CompletedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update rollout: %w", err)
	}

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &r.Metadata); err != nil {
			return nil, fmt.Errorf("decode metadata: %w", err)
		}
	}
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}

	return &r, nil
}
