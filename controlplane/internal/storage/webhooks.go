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

// Webhook represents a webhook configuration.
type Webhook struct {
	ID              uuid.UUID
	TenantID        uuid.NullUUID
	Name            string
	URL             string
	Events          []string
	Secret          sql.NullString
	Enabled         bool
	VerifySSL       bool
	TimeoutSeconds  int
	RetryCount      int
	Headers         map[string]any
	Metadata        map[string]any
	LastTriggeredAt sql.NullTime
	LastSuccessAt   sql.NullTime
	LastFailureAt   sql.NullTime
	FailureCount    int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CreatedBy       uuid.NullUUID
}

// WebhookDelivery represents a webhook delivery attempt.
type WebhookDelivery struct {
	ID            uuid.UUID
	WebhookID     uuid.UUID
	EventType     string
	EventID       sql.NullString
	Status        string
	HTTPStatusCode sql.NullInt64
	RequestBody   map[string]any
	ResponseBody  sql.NullString
	ErrorMessage  sql.NullString
	AttemptNumber int
	DeliveredAt   sql.NullTime
	CreatedAt     time.Time
}

// CreateWebhookParams defines input for creating a webhook.
type CreateWebhookParams struct {
	TenantID       uuid.UUID
	Name           string
	URL            string
	Events         []string
	Secret         *string
	Enabled        bool
	VerifySSL      bool
	TimeoutSeconds int
	RetryCount     int
	Headers        map[string]any
	Metadata       map[string]any
	CreatedBy      uuid.UUID
}

// UpdateWebhookParams captures patchable fields on a webhook.
type UpdateWebhookParams struct {
	Name           *string
	URL            *string
	Events         []string
	Secret         *string
	Enabled        *bool
	VerifySSL      *bool
	TimeoutSeconds *int
	RetryCount     *int
	Headers        map[string]any
	Metadata       map[string]any
}

// ListWebhooks returns webhooks with filtering.
func (s *Store) ListWebhooks(ctx context.Context, tenantID uuid.UUID, enabled *bool, limit, offset int) ([]Webhook, int, error) {
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

	if enabled != nil {
		args = append(args, *enabled)
		clauses = append(clauses, fmt.Sprintf("enabled = $%d", len(args)))
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM webhooks WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count webhooks: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, url, events, secret, enabled, verify_ssl, timeout_seconds, retry_count,
		       headers, metadata, last_triggered_at, last_success_at, last_failure_at, failure_count,
		       created_at, updated_at, created_by
		FROM webhooks
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
		return nil, 0, fmt.Errorf("query webhooks: %w", err)
	}
	defer rows.Close()

	var webhooks []Webhook
	for rows.Next() {
		var w Webhook
		var events pq.StringArray
		var headersJSON, metadataJSON []byte

		err := rows.Scan(
			&w.ID, &w.TenantID, &w.Name, &w.URL, &events, &w.Secret, &w.Enabled, &w.VerifySSL,
			&w.TimeoutSeconds, &w.RetryCount, &headersJSON, &metadataJSON,
			&w.LastTriggeredAt, &w.LastSuccessAt, &w.LastFailureAt, &w.FailureCount,
			&w.CreatedAt, &w.UpdatedAt, &w.CreatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan webhook: %w", err)
		}

		w.Events = []string(events)
		if len(headersJSON) > 0 {
			if err := json.Unmarshal(headersJSON, &w.Headers); err != nil {
				return nil, 0, fmt.Errorf("unmarshal headers: %w", err)
			}
		} else {
			w.Headers = make(map[string]any)
		}
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &w.Metadata); err != nil {
				return nil, 0, fmt.Errorf("unmarshal metadata: %w", err)
			}
		} else {
			w.Metadata = make(map[string]any)
		}

		webhooks = append(webhooks, w)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate webhooks: %w", err)
	}

	return webhooks, total, nil
}

// GetWebhook returns a webhook by ID.
func (s *Store) GetWebhook(ctx context.Context, id uuid.UUID) (*Webhook, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("webhook id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, url, events, secret, enabled, verify_ssl, timeout_seconds, retry_count,
		       headers, metadata, last_triggered_at, last_success_at, last_failure_at, failure_count,
		       created_at, updated_at, created_by
		FROM webhooks
		WHERE id = $1
	`, id)

	var w Webhook
	var events pq.StringArray
	var headersJSON, metadataJSON []byte

	err := row.Scan(
		&w.ID, &w.TenantID, &w.Name, &w.URL, &events, &w.Secret, &w.Enabled, &w.VerifySSL,
		&w.TimeoutSeconds, &w.RetryCount, &headersJSON, &metadataJSON,
		&w.LastTriggeredAt, &w.LastSuccessAt, &w.LastFailureAt, &w.FailureCount,
		&w.CreatedAt, &w.UpdatedAt, &w.CreatedBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get webhook: %w", err)
	}

	w.Events = []string(events)
	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &w.Headers); err != nil {
			return nil, fmt.Errorf("unmarshal headers: %w", err)
		}
	} else {
		w.Headers = make(map[string]any)
	}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &w.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	} else {
		w.Metadata = make(map[string]any)
	}

	return &w, nil
}

// CreateWebhook creates a new webhook.
func (s *Store) CreateWebhook(ctx context.Context, params CreateWebhookParams) (*Webhook, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.Name == "" {
		return nil, errors.New("webhook name is required")
	}
	if params.URL == "" {
		return nil, errors.New("webhook URL is required")
	}

	now := s.clock()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var tenantID uuid.NullUUID
	if params.TenantID != uuid.Nil {
		tenantID = uuid.NullUUID{UUID: params.TenantID, Valid: true}
	}

	var createdBy uuid.NullUUID
	if params.CreatedBy != uuid.Nil {
		createdBy = uuid.NullUUID{UUID: params.CreatedBy, Valid: true}
	}

	headersJSON, _ := json.Marshal(params.Headers)
	metadataJSON, _ := json.Marshal(params.Metadata)

	timeoutSeconds := params.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	retryCount := params.RetryCount
	if retryCount < 0 {
		retryCount = 3
	}

	var secret sql.NullString
	if params.Secret != nil && *params.Secret != "" {
		secret = sql.NullString{String: *params.Secret, Valid: true}
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO webhooks (tenant_id, name, url, events, secret, enabled, verify_ssl, timeout_seconds, retry_count, headers, metadata, created_at, updated_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id
	`,
		tenantID, params.Name, params.URL, pq.Array(params.Events), secret, params.Enabled, params.VerifySSL,
		timeoutSeconds, retryCount, headersJSON, metadataJSON, now, now, createdBy,
	)

	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		return nil, fmt.Errorf("create webhook: %w", err)
	}

	return s.GetWebhook(ctx, id)
}

// UpdateWebhook updates an existing webhook.
func (s *Store) UpdateWebhook(ctx context.Context, id uuid.UUID, params UpdateWebhookParams) (*Webhook, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("webhook id is required")
	}

	now := s.clock()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	updates := []string{"updated_at = $1"}
	args := []any{now}
	argIdx := 2

	if params.Name != nil {
		updates = append(updates, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *params.Name)
		argIdx++
	}
	if params.URL != nil {
		updates = append(updates, fmt.Sprintf("url = $%d", argIdx))
		args = append(args, *params.URL)
		argIdx++
	}
	if params.Events != nil {
		updates = append(updates, fmt.Sprintf("events = $%d", argIdx))
		args = append(args, pq.Array(params.Events))
		argIdx++
	}
	if params.Secret != nil {
		if *params.Secret == "" {
			updates = append(updates, fmt.Sprintf("secret = NULL"))
		} else {
			updates = append(updates, fmt.Sprintf("secret = $%d", argIdx))
			args = append(args, *params.Secret)
			argIdx++
		}
	}
	if params.Enabled != nil {
		updates = append(updates, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *params.Enabled)
		argIdx++
	}
	if params.VerifySSL != nil {
		updates = append(updates, fmt.Sprintf("verify_ssl = $%d", argIdx))
		args = append(args, *params.VerifySSL)
		argIdx++
	}
	if params.TimeoutSeconds != nil {
		updates = append(updates, fmt.Sprintf("timeout_seconds = $%d", argIdx))
		args = append(args, *params.TimeoutSeconds)
		argIdx++
	}
	if params.RetryCount != nil {
		updates = append(updates, fmt.Sprintf("retry_count = $%d", argIdx))
		args = append(args, *params.RetryCount)
		argIdx++
	}
	if params.Headers != nil {
		headersJSON, _ := json.Marshal(params.Headers)
		updates = append(updates, fmt.Sprintf("headers = $%d", argIdx))
		args = append(args, headersJSON)
		argIdx++
	}
	if params.Metadata != nil {
		metadataJSON, _ := json.Marshal(params.Metadata)
		updates = append(updates, fmt.Sprintf("metadata = $%d", argIdx))
		args = append(args, metadataJSON)
		argIdx++
	}

	if len(updates) == 1 {
		return s.GetWebhook(ctx, id)
	}

	args = append(args, id)
	query := fmt.Sprintf(`
		UPDATE webhooks
		SET %s
		WHERE id = $%d
	`, strings.Join(updates, ", "), len(args))

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update webhook: %w", err)
	}

	return s.GetWebhook(ctx, id)
}

// DeleteWebhook deletes a webhook.
func (s *Store) DeleteWebhook(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("webhook id is required")
	}

	_, err := s.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}

	return nil
}

// RecordWebhookDelivery records a webhook delivery attempt.
func (s *Store) RecordWebhookDelivery(ctx context.Context, delivery WebhookDelivery) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}

	requestBodyJSON, _ := json.Marshal(delivery.RequestBody)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_deliveries (id, webhook_id, event_type, event_id, status, http_status_code, request_body, response_body, error_message, attempt_number, delivered_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		delivery.ID, delivery.WebhookID, delivery.EventType, delivery.EventID, delivery.Status,
		delivery.HTTPStatusCode, requestBodyJSON, delivery.ResponseBody, delivery.ErrorMessage,
		delivery.AttemptNumber, delivery.DeliveredAt, delivery.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record webhook delivery: %w", err)
	}

	now := s.clock()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	updateFields := []string{"last_triggered_at = $1"}
	args := []any{now}
	argIdx := 2

	if delivery.Status == "success" {
		updateFields = append(updateFields, fmt.Sprintf("last_success_at = $%d", argIdx))
		args = append(args, now)
		argIdx++
		updateFields = append(updateFields, fmt.Sprintf("failure_count = 0"))
	} else if delivery.Status == "failed" {
		updateFields = append(updateFields, fmt.Sprintf("last_failure_at = $%d", argIdx))
		args = append(args, now)
		argIdx++
		updateFields = append(updateFields, fmt.Sprintf("failure_count = failure_count + 1"))
	}

	args = append(args, delivery.WebhookID)
	query := fmt.Sprintf(`
		UPDATE webhooks
		SET %s
		WHERE id = $%d
	`, strings.Join(updateFields, ", "), len(args))

	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update webhook stats: %w", err)
	}

	return nil
}

// ListWebhookDeliveries returns delivery history for a webhook.
func (s *Store) ListWebhookDeliveries(ctx context.Context, webhookID uuid.UUID, status *string, limit, offset int) ([]WebhookDelivery, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit < 0 || offset < 0 {
		return nil, 0, errors.New("limit and offset must be non-negative")
	}

	clauses := []string{"webhook_id = $1"}
	args := []any{webhookID}
	argIdx := 2

	if status != nil {
		clauses = append(clauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *status)
		argIdx++
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM webhook_deliveries WHERE %s`, strings.Join(clauses, " AND "))
	countRow := s.db.QueryRowContext(ctx, countQuery, args...)
	var total int
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count webhook deliveries: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, webhook_id, event_type, event_id, status, http_status_code, request_body, response_body, error_message, attempt_number, delivered_at, created_at
		FROM webhook_deliveries
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
		return nil, 0, fmt.Errorf("query webhook deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		var requestBodyJSON []byte

		err := rows.Scan(
			&d.ID, &d.WebhookID, &d.EventType, &d.EventID, &d.Status, &d.HTTPStatusCode,
			&requestBodyJSON, &d.ResponseBody, &d.ErrorMessage, &d.AttemptNumber,
			&d.DeliveredAt, &d.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan webhook delivery: %w", err)
		}

		if len(requestBodyJSON) > 0 {
			if err := json.Unmarshal(requestBodyJSON, &d.RequestBody); err != nil {
				return nil, 0, fmt.Errorf("unmarshal request body: %w", err)
			}
		} else {
			d.RequestBody = make(map[string]any)
		}

		deliveries = append(deliveries, d)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate webhook deliveries: %w", err)
	}

	return deliveries, total, nil
}

// GetEnabledWebhooksForEvent returns all enabled webhooks that subscribe to a specific event type.
func (s *Store) GetEnabledWebhooksForEvent(ctx context.Context, eventType string) ([]Webhook, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, url, events, secret, enabled, verify_ssl, timeout_seconds, retry_count,
		       headers, metadata, last_triggered_at, last_success_at, last_failure_at, failure_count,
		       created_at, updated_at, created_by
		FROM webhooks
		WHERE enabled = true AND $1 = ANY(events)
		ORDER BY created_at
	`, eventType)
	if err != nil {
		return nil, fmt.Errorf("query webhooks for event: %w", err)
	}
	defer rows.Close()

	var webhooks []Webhook
	for rows.Next() {
		var w Webhook
		var events pq.StringArray
		var headersJSON, metadataJSON []byte

		err := rows.Scan(
			&w.ID, &w.TenantID, &w.Name, &w.URL, &events, &w.Secret, &w.Enabled, &w.VerifySSL,
			&w.TimeoutSeconds, &w.RetryCount, &headersJSON, &metadataJSON,
			&w.LastTriggeredAt, &w.LastSuccessAt, &w.LastFailureAt, &w.FailureCount,
			&w.CreatedAt, &w.UpdatedAt, &w.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}

		w.Events = []string(events)
		if len(headersJSON) > 0 {
			if err := json.Unmarshal(headersJSON, &w.Headers); err != nil {
				return nil, fmt.Errorf("unmarshal headers: %w", err)
			}
		} else {
			w.Headers = make(map[string]any)
		}
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &w.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		} else {
			w.Metadata = make(map[string]any)
		}

		webhooks = append(webhooks, w)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhooks: %w", err)
	}

	return webhooks, nil
}

