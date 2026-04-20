package server

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// handleRetireNode marks a node retired in response to a successful agent-side
// uninstall. The endpoint is mTLS-authenticated (agent identity) or admin/operator
// (operator-initiated retirement). We accept both because uninstall can be driven
// from either side: an operator running the uninstall one-liner, or the agent itself
// calling back with its own cert before shutting down.
func (s *Server) handleRetireNode(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for retire", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	if err := s.store.RetireNode(r.Context(), nodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("retire node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"node_id": nodeID.String(),
		"state":   "retired",
	})

	s.recordAudit(r.Context(), principal, node.TenantID, "node.retired", "node", nodeID.String(), map[string]any{
		"hostname": node.Hostname,
	})
}

// rotateCertResponse is returned to the agent after a successful rotation. It
// carries the newly-signed client certificate + key so the agent can swap
// them in-place without re-enrolling.
type rotateCertResponse struct {
	NodeID     string `json:"node_id"`
	Serial     string `json:"serial"`
	HistoryID  string `json:"history_id"`
	IssuedAt   string `json:"issued_at"`
	CertPEM    string `json:"cert_pem"`
	KeyPEM     string `json:"key_pem"`
	CACertPEM  string `json:"ca_cert_pem,omitempty"`
	ReplacedBy string `json:"replaced_history_id,omitempty"`
}

// handleRotateCert issues a fresh ECDSA client certificate for an already-enrolled
// node without requiring re-enrollment. The call is mTLS-only: the agent must
// present its existing cert and its Subject CommonName must equal the node_id
// in the path, preventing a leaked cert from one node from rotating a sibling.
func (s *Server) handleRotateCert(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	// Rotation is a mutual-auth-only operation. Operator principals (token/OIDC)
	// cannot rotate on behalf of a node — only the node itself, carrying its
	// current cert as proof-of-identity, may rotate.
	if principal.Type != "agent" {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	if principal.Name == "" || principal.Name != nodeID.String() {
		s.logger.Warn("rotate cert CN mismatch",
			zap.String("cn", principal.Name),
			zap.String("node_id", nodeID.String()),
		)
		http.Error(w, "certificate subject does not match node id", http.StatusForbidden)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for rotate cert", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	// Reuse the enrollment cert issuance path so we get CA-signed ECDSA P-256
	// certs identical to the ones minted at join time.
	certPEM, keyPEM, caCertPEM, err := s.generateClientCertificate(node.Hostname, nodeID.String())
	if err != nil {
		s.logger.Error("generate rotated client certificate", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	serial, err := extractCertSerial(certPEM)
	if err != nil {
		s.logger.Error("extract rotated cert serial", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Capture the superseded history row id so the response can surface the
	// chain to the agent for its own audit trail.
	var replacedHistoryID string
	if prev, latestErr := s.store.LatestNodeCertHistory(r.Context(), nodeID); latestErr == nil && prev != nil {
		replacedHistoryID = prev.ID.String()
	}

	history, err := s.store.RotateNodeCertificate(r.Context(), nodeID, serial)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("rotate node certificate", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := rotateCertResponse{
		NodeID:     nodeID.String(),
		Serial:     history.Serial,
		HistoryID:  history.ID.String(),
		IssuedAt:   history.IssuedAt.UTC().Format(timeFormatRFC3339),
		CertPEM:    string(certPEM),
		KeyPEM:     string(keyPEM),
		CACertPEM:  string(caCertPEM),
		ReplacedBy: replacedHistoryID,
	}

	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, node.TenantID, "node.cert_rotated", "node", nodeID.String(), map[string]any{
		"hostname":   node.Hostname,
		"serial":     history.Serial,
		"history_id": history.ID.String(),
	})

	s.emitNodeCertRotatedEvent(r.Context(), node.TenantID, nodeID, history)
}

// extractCertSerial reads the serial number from a PEM-encoded certificate and
// returns it as a hex string. This matches the format used by OpenSSL-style
// tooling so ops can cross-reference with the `nodes.cert_serial` column.
func extractCertSerial(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", errors.New("decode certificate PEM: no block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse certificate: %w", err)
	}
	if cert.SerialNumber == nil {
		return "", errors.New("certificate has no serial number")
	}
	// Pad to even-length hex for readability.
	serial := new(big.Int).Set(cert.SerialNumber).Text(16)
	if len(serial)%2 != 0 {
		serial = "0" + serial
	}
	return serial, nil
}

// timeFormatRFC3339 is the canonical timestamp format used across node-scoped
// responses.
const timeFormatRFC3339 = "2006-01-02T15:04:05Z07:00"

// EventNodeCertRotated is emitted when a node's client cert is rotated via the
// rotate endpoint. Operators hook this into their SIEM to confirm rotations
// align with their 90-day policy.
const EventNodeCertRotated = "node.cert_rotated"

// emitNodeCertRotatedEvent fires a webhook payload to subscribers of the
// `node.cert_rotated` event. Deliveries are best-effort and never fail the
// rotation itself.
func (s *Server) emitNodeCertRotatedEvent(ctx context.Context, tenantID, nodeID uuid.UUID, history *storage.NodeCertHistory) {
	if s.store == nil || history == nil {
		return
	}

	webhooks, err := s.store.ListWebhooksByEvent(ctx, tenantID, EventNodeCertRotated)
	if err != nil {
		s.logger.Warn("list webhooks for cert rotation event", zap.Error(err))
		return
	}
	if len(webhooks) == 0 {
		return
	}

	payload := map[string]any{
		"node_id":    nodeID.String(),
		"tenant_id":  tenantID.String(),
		"serial":     history.Serial,
		"history_id": history.ID.String(),
		"issued_at":  history.IssuedAt.UTC().Format(timeFormatRFC3339),
	}

	for _, wh := range webhooks {
		wh := wh
		go s.deliverAndRecordCertRotation(context.Background(), &wh, EventNodeCertRotated, payload)
	}
}

// deliverAndRecordCertRotation mirrors the compliance webhook delivery helper
// but for the node.cert_rotated event. Kept local to nodes.go so the emission
// path is easy to follow.
func (s *Server) deliverAndRecordCertRotation(ctx context.Context, webhook *storage.Webhook, eventType string, payload map[string]any) {
	success, statusCode, responseBody, err := s.deliverWebhook(webhook, eventType, payload)

	deliveryStatus := "success"
	if !success {
		deliveryStatus = "failed"
	}

	delivery := storage.WebhookDelivery{
		ID:            uuid.New(),
		WebhookID:     webhook.ID,
		EventType:     eventType,
		Status:        deliveryStatus,
		AttemptNumber: 1,
		RequestBody:   payload,
		CreatedAt:     time.Now().UTC(),
	}
	if statusCode > 0 {
		delivery.HTTPStatusCode = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}
	if responseBody != "" {
		delivery.ResponseBody = sql.NullString{String: responseBody, Valid: true}
	}
	if err != nil {
		delivery.ErrorMessage = sql.NullString{String: err.Error(), Valid: true}
	}
	delivery.DeliveredAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}

	if recordErr := s.store.RecordWebhookDelivery(ctx, delivery); recordErr != nil {
		s.logger.Warn("record cert rotation webhook delivery",
			zap.String("webhook_id", webhook.ID.String()),
			zap.Error(recordErr),
		)
	}
}
