package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Mode represents how a subscription should execute matching hooks.
type Mode string

const (
	// ModeAuto indicates matching runs should be scheduled immediately (within policy limits).
	ModeAuto Mode = "auto"
	// ModeManual indicates matching runs require an operator to approve/trigger execution.
	ModeManual Mode = "manual"
)

// HandlerType defines the supported handler execution environments.
type HandlerType string

const (
	HandlerTypeWASM   HandlerType = "wasm"
	HandlerTypeBash   HandlerType = "bash"
	HandlerTypeLua    HandlerType = "lua"
	HandlerTypeWebhook HandlerType = "webhook"
)

// Event encapsulates a typed emission from producers such as agents, compliance, mesh, or telemetry.
type Event struct {
	ID            string                 `json:"id"`
	EventID       string                 `json:"event_id"`
	Source        string                 `json:"source"`
	Subject       string                 `json:"subject"`
	Timestamp     time.Time              `json:"timestamp"`
	Payload       map[string]any         `json:"payload"`
	SchemaVersion string                 `json:"schema_version"`
	ReceivedAt    time.Time              `json:"received_at"`
	Metadata      map[string]string      `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the event to avoid mutation across subscribers.
func (e *Event) Clone() *Event {
	if e == nil {
		return nil
	}
	payloadCopy := make(map[string]any, len(e.Payload))
	for k, v := range e.Payload {
		payloadCopy[k] = v
	}
	metaCopy := make(map[string]string, len(e.Metadata))
	for k, v := range e.Metadata {
		metaCopy[k] = v
	}
	clone := *e
	clone.Payload = payloadCopy
	clone.Metadata = metaCopy
	return &clone
}

// RunPolicy captures execution limits for a subscription.
type RunPolicy struct {
	Timeout     time.Duration `json:"timeout"`
	MemoryMB    int           `json:"memory_mb"`
	Concurrency int           `json:"concurrency"`
	MaxRetries  int           `json:"max_retries"`
}

// Handler describes how a script/automation should execute.
type Handler struct {
	Type     HandlerType `json:"type"`
	Language string      `json:"language,omitempty"`
	Inline   string      `json:"inline,omitempty"`
	Source   string      `json:"source,omitempty"`
}

// Subscription binds events to handlers with optional filtering.
type Subscription struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenant_id"`
	EventID        string            `json:"event_id"`
	Filter         string            `json:"filter"`
	Handler        Handler           `json:"handler"`
	Mode           Mode              `json:"mode"`
	RunPolicy      RunPolicy         `json:"run_policy"`
	Remediate      bool              `json:"remediate_allowed"`
	RBACRoles      []string          `json:"rbac_roles"`
	CreatedBy      string            `json:"created_by"`
	CreatedAt      time.Time         `json:"created_at"`
	LastModifiedAt time.Time         `json:"last_modified_at"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// Matches performs a best-effort filter evaluation against the supplied event.
// The current implementation supports simple substring matching over the JSON payload
// and subject; more sophisticated filtering (CEL/expr) can be layered on later.
func (s *Subscription) Matches(evt *Event) bool {
	if s == nil || evt == nil {
		return false
	}
	if s.EventID != "" && s.EventID != evt.EventID {
		return false
	}
	if s.Filter == "" {
		return true
	}
	payloadBytes, err := json.Marshal(evt.Payload)
	if err != nil {
		return false
	}
	needle := strings.ToLower(s.Filter)
	payloadStr := strings.ToLower(string(payloadBytes))
	if strings.Contains(payloadStr, needle) {
		return true
	}
	subject := strings.ToLower(evt.Subject)
	return strings.Contains(subject, needle)
}

// ScriptRunStatus enumerates execution lifecycle states.
type ScriptRunStatus string

const (
	ScriptRunStatusQueued    ScriptRunStatus = "queued"
	ScriptRunStatusDelayed   ScriptRunStatus = "delayed"
	ScriptRunStatusRunning   ScriptRunStatus = "running"
	ScriptRunStatusSuccess   ScriptRunStatus = "success"
	ScriptRunStatusFailed    ScriptRunStatus = "failed"
	ScriptRunStatusTimedOut  ScriptRunStatus = "timed_out"
	ScriptRunStatusCancelled ScriptRunStatus = "cancelled"
)

// ScriptRun captures an execution attempt produced from an event/subscription match.
type ScriptRun struct {
	RunID          string           `json:"run_id"`
	SubscriptionID string           `json:"subscription_id"`
	EventID        string           `json:"event_id"`
	TenantID       string           `json:"tenant_id"`
	NodeID         string           `json:"node_id"`
	Status         ScriptRunStatus  `json:"status"`
	Mode           Mode             `json:"mode"`
	Priority       int              `json:"priority"`
	QueuedAt       time.Time        `json:"queued_at"`
	StartedAt      time.Time        `json:"started_at"`
	EndedAt        time.Time        `json:"ended_at"`
	ExitCode       int              `json:"exit_code"`
	Stdout         string           `json:"stdout"`
	Stderr         string           `json:"stderr"`
	Attempts       int              `json:"attempts"`
	RunPolicy      RunPolicy        `json:"run_policy"`
	Metadata       map[string]any   `json:"meta,omitempty"`
}

// OverrideRequest models operator approval workflows for elevated capabilities.
type OverrideRequest struct {
	ID             string    `json:"id"`
	SubscriptionID string    `json:"subscription_id"`
	RequestedBy    string    `json:"requested_by"`
	RequestedCaps  []string  `json:"requested_caps"`
	Status         string    `json:"status"`
	Approvers      []string  `json:"approvers"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// Publisher exposes the ability to publish events to the hook system.
type Publisher interface {
	PublishEvent(ctx context.Context, evt *Event) error
}
