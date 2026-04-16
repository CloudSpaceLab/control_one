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

// SessionRecording represents a recorded session
type SessionRecording struct {
	ID                uuid.UUID
	NodeID            uuid.UUID
	UserID            sql.NullString
	SessionType       string
	StartedAt         time.Time
	EndedAt           sql.NullTime
	DurationSeconds   sql.NullInt64
	Status            string
	Metadata          map[string]any
	ArtifactPath      sql.NullString
	ArtifactSizeBytes sql.NullInt64
	Checksum          sql.NullString
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// SessionEvent represents an event within a session
type SessionEvent struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	EventType string
	Timestamp time.Time
	Data      map[string]any
	CreatedAt time.Time
}

// CreateSessionRecordingParams defines input for creating a session recording
type CreateSessionRecordingParams struct {
	NodeID      uuid.UUID
	UserID      *string
	SessionType string
	StartedAt   time.Time
	Status      string
	Metadata    map[string]any
}

// UpdateSessionRecordingParams captures updatable fields
type UpdateSessionRecordingParams struct {
	EndedAt           *time.Time
	DurationSeconds   *int64
	Status            *string
	ArtifactPath      *string
	ArtifactSizeBytes *int64
	Checksum          *string
	Metadata          map[string]any
}

// ListSessionRecordingsParams defines filters for listing sessions
type ListSessionRecordingsParams struct {
	NodeID      *uuid.UUID
	UserID      *string
	SessionType *string
	Status      *string
	StartTime   *time.Time
	EndTime     *time.Time
}

// CreateSessionRecording inserts a new session recording
func (s *Store) CreateSessionRecording(ctx context.Context, params CreateSessionRecordingParams) (*SessionRecording, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	recording := &SessionRecording{
		ID:          uuid.New(),
		NodeID:      params.NodeID,
		SessionType: params.SessionType,
		StartedAt:   params.StartedAt,
		Status:      params.Status,
		CreatedAt:   s.clock(),
		UpdatedAt:   s.clock(),
	}

	if params.UserID != nil {
		recording.UserID = sql.NullString{String: *params.UserID, Valid: true}
	}
	if params.Metadata != nil {
		recording.Metadata = params.Metadata
	} else {
		recording.Metadata = make(map[string]any)
	}

	if recording.SessionType == "" {
		recording.SessionType = "terminal"
	}
	if recording.Status == "" {
		recording.Status = "active"
	}

	metadataJSON, err := json.Marshal(recording.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO session_recordings (
			id, node_id, user_id, session_type, started_at, status, metadata, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, recording.ID, recording.NodeID, recording.UserID, recording.SessionType,
		recording.StartedAt, recording.Status, metadataJSON, recording.CreatedAt, recording.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert session recording: %w", err)
	}

	return recording, nil
}

// GetSessionRecording retrieves a session recording by ID
func (s *Store) GetSessionRecording(ctx context.Context, id uuid.UUID) (*SessionRecording, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	var recording SessionRecording
	var metadataRaw []byte
	var userID sql.NullString
	var endedAt sql.NullTime
	var durationSeconds sql.NullInt64
	var artifactPath sql.NullString
	var artifactSizeBytes sql.NullInt64
	var checksum sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, node_id, user_id, session_type, started_at, ended_at, duration_seconds,
			status, metadata, artifact_path, artifact_size_bytes, checksum, created_at, updated_at
		FROM session_recordings
		WHERE id = $1
	`, id).Scan(
		&recording.ID, &recording.NodeID, &userID, &recording.SessionType,
		&recording.StartedAt, &endedAt, &durationSeconds, &recording.Status,
		&metadataRaw, &artifactPath, &artifactSizeBytes, &checksum,
		&recording.CreatedAt, &recording.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get session recording: %w", err)
	}

	recording.UserID = userID
	recording.EndedAt = endedAt
	recording.DurationSeconds = durationSeconds
	recording.ArtifactPath = artifactPath
	recording.ArtifactSizeBytes = artifactSizeBytes
	recording.Checksum = checksum

	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &recording.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	} else {
		recording.Metadata = make(map[string]any)
	}

	return &recording, nil
}

// ListSessionRecordings returns session recordings matching filters
func (s *Store) ListSessionRecordings(ctx context.Context, params ListSessionRecordingsParams, limit, offset int) ([]SessionRecording, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"TRUE"}
	args := []any{}

	if params.NodeID != nil {
		args = append(args, *params.NodeID)
		clauses = append(clauses, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if params.UserID != nil {
		args = append(args, *params.UserID)
		clauses = append(clauses, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if params.SessionType != nil {
		args = append(args, *params.SessionType)
		clauses = append(clauses, fmt.Sprintf("session_type = $%d", len(args)))
	}
	if params.Status != nil {
		args = append(args, *params.Status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if params.StartTime != nil {
		args = append(args, *params.StartTime)
		clauses = append(clauses, fmt.Sprintf("started_at >= $%d", len(args)))
	}
	if params.EndTime != nil {
		args = append(args, *params.EndTime)
		clauses = append(clauses, fmt.Sprintf("started_at <= $%d", len(args)))
	}

	query := fmt.Sprintf(`
		SELECT id, node_id, user_id, session_type, started_at, ended_at, duration_seconds,
			status, metadata, artifact_path, artifact_size_bytes, checksum, created_at, updated_at
		FROM session_recordings
		WHERE %s
		ORDER BY started_at DESC
	`, strings.Join(clauses, " AND "))

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM session_recordings WHERE %s`, strings.Join(clauses, " AND "))

	argsForCount := make([]any, len(args))
	copy(argsForCount, args)

	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	countRow := s.db.QueryRowContext(ctx, countQuery, argsForCount...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count session recordings: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query session recordings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var recordings []SessionRecording
	for rows.Next() {
		var recording SessionRecording
		var metadataRaw []byte
		var userID sql.NullString
		var endedAt sql.NullTime
		var durationSeconds sql.NullInt64
		var artifactPath sql.NullString
		var artifactSizeBytes sql.NullInt64
		var checksum sql.NullString

		if err := rows.Scan(
			&recording.ID, &recording.NodeID, &userID, &recording.SessionType,
			&recording.StartedAt, &endedAt, &durationSeconds, &recording.Status,
			&metadataRaw, &artifactPath, &artifactSizeBytes, &checksum,
			&recording.CreatedAt, &recording.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan session recording: %w", err)
		}

		recording.UserID = userID
		recording.EndedAt = endedAt
		recording.DurationSeconds = durationSeconds
		recording.ArtifactPath = artifactPath
		recording.ArtifactSizeBytes = artifactSizeBytes
		recording.Checksum = checksum

		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &recording.Metadata); err != nil {
				return nil, 0, fmt.Errorf("unmarshal metadata: %w", err)
			}
		} else {
			recording.Metadata = make(map[string]any)
		}

		recordings = append(recordings, recording)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate session recordings: %w", err)
	}

	return recordings, total, nil
}

// UpdateSessionRecording updates a session recording
func (s *Store) UpdateSessionRecording(ctx context.Context, id uuid.UUID, params UpdateSessionRecordingParams) (*SessionRecording, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	updates := []string{}
	args := []any{id}

	if params.EndedAt != nil {
		args = append(args, *params.EndedAt)
		updates = append(updates, fmt.Sprintf("ended_at = $%d", len(args)))
	}
	if params.DurationSeconds != nil {
		args = append(args, *params.DurationSeconds)
		updates = append(updates, fmt.Sprintf("duration_seconds = $%d", len(args)))
	}
	if params.Status != nil {
		args = append(args, *params.Status)
		updates = append(updates, fmt.Sprintf("status = $%d", len(args)))
	}
	if params.ArtifactPath != nil {
		args = append(args, *params.ArtifactPath)
		updates = append(updates, fmt.Sprintf("artifact_path = $%d", len(args)))
	}
	if params.ArtifactSizeBytes != nil {
		args = append(args, *params.ArtifactSizeBytes)
		updates = append(updates, fmt.Sprintf("artifact_size_bytes = $%d", len(args)))
	}
	if params.Checksum != nil {
		args = append(args, *params.Checksum)
		updates = append(updates, fmt.Sprintf("checksum = $%d", len(args)))
	}
	if params.Metadata != nil {
		metadataJSON, err := json.Marshal(params.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		args = append(args, metadataJSON)
		updates = append(updates, fmt.Sprintf("metadata = $%d", len(args)))
	}

	if len(updates) == 0 {
		return s.GetSessionRecording(ctx, id)
	}

	args = append(args, s.clock())
	updates = append(updates, fmt.Sprintf("updated_at = $%d", len(args)))

	query := fmt.Sprintf(`
		UPDATE session_recordings
		SET %s
		WHERE id = $1
	`, strings.Join(updates, ", "))

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update session recording: %w", err)
	}

	return s.GetSessionRecording(ctx, id)
}

// CreateSessionEvent inserts a new session event
func (s *Store) CreateSessionEvent(ctx context.Context, sessionID uuid.UUID, eventType string, timestamp time.Time, data map[string]any) (*SessionEvent, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	event := &SessionEvent{
		ID:        uuid.New(),
		SessionID: sessionID,
		EventType: eventType,
		Timestamp: timestamp,
		CreatedAt: s.clock(),
	}

	if data != nil {
		event.Data = data
	} else {
		event.Data = make(map[string]any)
	}

	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return nil, fmt.Errorf("marshal event data: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO session_events (id, session_id, event_type, timestamp, data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.ID, event.SessionID, event.EventType, event.Timestamp, dataJSON, event.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert session event: %w", err)
	}

	return event, nil
}

// ListSessionEvents returns events for a session
func (s *Store) ListSessionEvents(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]SessionEvent, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	countRow := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM session_events WHERE session_id = $1
	`, sessionID)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count session events: %w", err)
	}

	query := `
		SELECT id, session_id, event_type, timestamp, data, created_at
		FROM session_events
		WHERE session_id = $1
		ORDER BY timestamp ASC
	`
	args := []any{sessionID}

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
		return nil, 0, fmt.Errorf("query session events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []SessionEvent
	for rows.Next() {
		var event SessionEvent
		var dataRaw []byte

		if err := rows.Scan(
			&event.ID, &event.SessionID, &event.EventType,
			&event.Timestamp, &dataRaw, &event.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan session event: %w", err)
		}

		if len(dataRaw) > 0 {
			if err := json.Unmarshal(dataRaw, &event.Data); err != nil {
				return nil, 0, fmt.Errorf("unmarshal event data: %w", err)
			}
		} else {
			event.Data = make(map[string]any)
		}

		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate session events: %w", err)
	}

	return events, total, nil
}
