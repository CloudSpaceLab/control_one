package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type webhookResponse struct {
	ID              string         `json:"id"`
	TenantID        *string        `json:"tenant_id,omitempty"`
	Name            string         `json:"name"`
	URL             string         `json:"url"`
	Events          []string       `json:"events"`
	Enabled         bool           `json:"enabled"`
	VerifySSL       bool           `json:"verify_ssl"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
	RetryCount      int            `json:"retry_count"`
	Headers         map[string]any `json:"headers,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	LastTriggeredAt *string        `json:"last_triggered_at,omitempty"`
	LastSuccessAt   *string        `json:"last_success_at,omitempty"`
	LastFailureAt   *string        `json:"last_failure_at,omitempty"`
	FailureCount    int            `json:"failure_count"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	CreatedBy       *string        `json:"created_by,omitempty"`
}

type createWebhookRequest struct {
	TenantID       *string        `json:"tenant_id,omitempty"`
	Name           string         `json:"name"`
	URL            string         `json:"url"`
	Events         []string       `json:"events"`
	Secret         *string        `json:"secret,omitempty"`
	Enabled        *bool          `json:"enabled,omitempty"`
	VerifySSL      *bool          `json:"verify_ssl,omitempty"`
	TimeoutSeconds *int           `json:"timeout_seconds,omitempty"`
	RetryCount     *int           `json:"retry_count,omitempty"`
	Headers        map[string]any `json:"headers,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type updateWebhookRequest struct {
	Name           *string        `json:"name,omitempty"`
	URL            *string        `json:"url,omitempty"`
	Events         []string       `json:"events,omitempty"`
	Secret         *string        `json:"secret,omitempty"`
	Enabled        *bool          `json:"enabled,omitempty"`
	VerifySSL      *bool          `json:"verify_ssl,omitempty"`
	TimeoutSeconds *int           `json:"timeout_seconds,omitempty"`
	RetryCount     *int           `json:"retry_count,omitempty"`
	Headers        map[string]any `json:"headers,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type webhookDeliveryResponse struct {
	ID             string         `json:"id"`
	WebhookID      string         `json:"webhook_id"`
	EventType      string         `json:"event_type"`
	EventID        *string        `json:"event_id,omitempty"`
	Status         string         `json:"status"`
	HTTPStatusCode *int           `json:"http_status_code,omitempty"`
	RequestBody    map[string]any `json:"request_body,omitempty"`
	ResponseBody   *string        `json:"response_body,omitempty"`
	ErrorMessage   *string        `json:"error_message,omitempty"`
	AttemptNumber  int            `json:"attempt_number"`
	DeliveredAt    *string        `json:"delivered_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
}

type testWebhookRequest struct {
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func (s *Server) handleWebhooksCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListWebhooks(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateWebhook(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebhookSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/webhooks/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	segments := strings.Split(trimmed, "/")
	webhookID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid webhook id", http.StatusBadRequest)
		return
	}

	if len(segments) == 1 {
		s.handleWebhookResource(w, r, webhookID)
		return
	}

	if len(segments) == 2 && segments[1] == "test" {
		s.handleTestWebhook(w, r, webhookID)
		return
	}

	if len(segments) == 2 && segments[1] == "deliveries" {
		s.handleListWebhookDeliveries(w, r, webhookID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	var enabled *bool
	if enabledParam := strings.TrimSpace(r.URL.Query().Get("enabled")); enabledParam != "" {
		enabledVal := enabledParam == "true"
		enabled = &enabledVal
	}

	webhooks, total, err := s.store.ListWebhooks(r.Context(), tenantID, enabled, limit, offset)
	if err != nil {
		s.logger.Error("list webhooks", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]webhookResponse, 0, len(webhooks))
	for _, w := range webhooks {
		resp := webhookToResponse(&w)
		respItems = append(respItems, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"items":  respItems,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if len(req.Events) == 0 {
		http.Error(w, "events array cannot be empty", http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if req.TenantID != nil {
		parsed, err := uuid.Parse(*req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	verifySSL := true
	if req.VerifySSL != nil {
		verifySSL = *req.VerifySSL
	}
	timeoutSeconds := 30
	if req.TimeoutSeconds != nil {
		timeoutSeconds = *req.TimeoutSeconds
	}
	retryCount := 3
	if req.RetryCount != nil {
		retryCount = *req.RetryCount
	}

	params := storage.CreateWebhookParams{
		TenantID:       tenantID,
		Name:           req.Name,
		URL:            req.URL,
		Events:         req.Events,
		Secret:         req.Secret,
		Enabled:        enabled,
		VerifySSL:      verifySSL,
		TimeoutSeconds: timeoutSeconds,
		RetryCount:     retryCount,
		Headers:        req.Headers,
		Metadata:       req.Metadata,
	}

	webhook, err := s.store.CreateWebhook(r.Context(), params)
	if err != nil {
		s.logger.Error("create webhook", zap.Error(err))
		if strings.Contains(err.Error(), "unique constraint") {
			http.Error(w, "webhook with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(webhookToResponse(webhook))
}

func (s *Server) handleWebhookResource(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetWebhook(w, r, id)
	case http.MethodPut:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleUpdateWebhook(w, r, id)
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleDeleteWebhook(w, r, id)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPut, http.MethodDelete}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	webhook, err := s.store.GetWebhook(r.Context(), id)
	if err != nil {
		s.logger.Error("get webhook", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if webhook == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(webhookToResponse(webhook))
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req updateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	params := storage.UpdateWebhookParams{
		Name:           req.Name,
		URL:            req.URL,
		Events:         req.Events,
		Secret:         req.Secret,
		Enabled:        req.Enabled,
		VerifySSL:      req.VerifySSL,
		TimeoutSeconds: req.TimeoutSeconds,
		RetryCount:     req.RetryCount,
		Headers:        req.Headers,
		Metadata:       req.Metadata,
	}

	webhook, err := s.store.UpdateWebhook(r.Context(), id, params)
	if err != nil {
		s.logger.Error("update webhook", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if webhook == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(webhookToResponse(webhook))
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	if err := s.store.DeleteWebhook(r.Context(), id); err != nil {
		s.logger.Error("delete webhook", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	webhook, err := s.store.GetWebhook(r.Context(), id)
	if err != nil {
		s.logger.Error("get webhook", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if webhook == nil {
		http.NotFound(w, r)
		return
	}

	var req testWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.EventType == "" {
		req.EventType = "test"
	}
	if req.Payload == nil {
		req.Payload = map[string]any{
			"message":   "Test webhook delivery",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
	}

	success, statusCode, responseBody, err := s.deliverWebhook(webhook, req.EventType, req.Payload)
	if err != nil {
		s.logger.Error("test webhook delivery", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":          success,
		"http_status_code": statusCode,
		"response_body":    responseBody,
		"error": func() *string {
			if err != nil {
				msg := err.Error()
				return &msg
			}
			return nil
		}(),
	})
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request, webhookID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var status *string
	if statusParam := strings.TrimSpace(r.URL.Query().Get("status")); statusParam != "" {
		status = &statusParam
	}

	deliveries, total, err := s.store.ListWebhookDeliveries(r.Context(), webhookID, status, limit, offset)
	if err != nil {
		s.logger.Error("list webhook deliveries", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]webhookDeliveryResponse, 0, len(deliveries))
	for _, d := range deliveries {
		resp := webhookDeliveryToResponse(&d)
		respItems = append(respItems, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"items":  respItems,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func webhookToResponse(w *storage.Webhook) webhookResponse {
	resp := webhookResponse{
		ID:             w.ID.String(),
		Name:           w.Name,
		URL:            w.URL,
		Events:         w.Events,
		Enabled:        w.Enabled,
		VerifySSL:      w.VerifySSL,
		TimeoutSeconds: w.TimeoutSeconds,
		RetryCount:     w.RetryCount,
		Headers:        w.Headers,
		Metadata:       w.Metadata,
		FailureCount:   w.FailureCount,
		CreatedAt:      w.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      w.UpdatedAt.Format(time.RFC3339),
	}

	if w.TenantID.Valid {
		tenantID := w.TenantID.UUID.String()
		resp.TenantID = &tenantID
	}
	if w.LastTriggeredAt.Valid {
		ts := w.LastTriggeredAt.Time.Format(time.RFC3339)
		resp.LastTriggeredAt = &ts
	}
	if w.LastSuccessAt.Valid {
		ts := w.LastSuccessAt.Time.Format(time.RFC3339)
		resp.LastSuccessAt = &ts
	}
	if w.LastFailureAt.Valid {
		ts := w.LastFailureAt.Time.Format(time.RFC3339)
		resp.LastFailureAt = &ts
	}
	if w.CreatedBy.Valid {
		createdBy := w.CreatedBy.UUID.String()
		resp.CreatedBy = &createdBy
	}

	return resp
}

func webhookDeliveryToResponse(d *storage.WebhookDelivery) webhookDeliveryResponse {
	resp := webhookDeliveryResponse{
		ID:            d.ID.String(),
		WebhookID:     d.WebhookID.String(),
		EventType:     d.EventType,
		Status:        d.Status,
		AttemptNumber: d.AttemptNumber,
		CreatedAt:     d.CreatedAt.Format(time.RFC3339),
		RequestBody:   d.RequestBody,
	}

	if d.EventID.Valid {
		resp.EventID = &d.EventID.String
	}
	if d.HTTPStatusCode.Valid {
		code := int(d.HTTPStatusCode.Int64)
		resp.HTTPStatusCode = &code
	}
	if d.ResponseBody.Valid {
		resp.ResponseBody = &d.ResponseBody.String
	}
	if d.ErrorMessage.Valid {
		resp.ErrorMessage = &d.ErrorMessage.String
	}
	if d.DeliveredAt.Valid {
		ts := d.DeliveredAt.Time.Format(time.RFC3339)
		resp.DeliveredAt = &ts
	}

	return resp
}

func (s *Server) deliverWebhook(webhook *storage.Webhook, eventType string, payload map[string]any) (bool, int, string, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return false, 0, "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhook.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		return false, 0, "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", eventType)
	req.Header.Set("X-Webhook-ID", webhook.ID.String())
	req.Header.Set("User-Agent", "Control-One-Webhook/1.0")

	if webhook.Secret.Valid && webhook.Secret.String != "" {
		signature := computeHMAC(webhook.Secret.String, payloadBytes)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	for k, v := range webhook.Headers {
		if str, ok := v.(string); ok {
			req.Header.Set(k, str)
		}
	}

	client := &http.Client{
		Timeout: time.Duration(webhook.TimeoutSeconds) * time.Second,
	}

	if !webhook.VerifySSL {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, 0, "", fmt.Errorf("deliver webhook: %w", err)
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)
	responseBodyStr := string(responseBody)

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	return success, resp.StatusCode, responseBodyStr, nil
}

func computeHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
