package networkdevice

import (
	"encoding/json"
	"errors"
	"math"
	"time"
)

// Source readiness is evidence about collection, never device operational health.
var SourceTypes = []string{"snmp_poll", "snmp_trap", "syslog", "netflow", "ipfix", "sflow", "ssh_config", "netconf", "restconf", "vendor_api"}

func IsSourceType(value string) bool {
	for _, source := range SourceTypes {
		if source == value {
			return true
		}
	}
	return false
}
func IsReceiverSource(value string) bool {
	switch value {
	case "snmp_trap", "syslog", "netflow", "ipfix", "sflow":
		return true
	}
	return false
}

type SourceReport struct {
	BindingID     string             `json:"binding_id"`
	State         string             `json:"state"`
	ObservedAt    time.Time          `json:"observed_at"`
	LastContactAt *time.Time         `json:"last_contact_at,omitempty"`
	QueueDepth    int64              `json:"queue_depth"`
	LagMillis     int64              `json:"lag_millis"`
	Metrics       map[string]float64 `json:"metrics,omitempty"`
	Records       []map[string]any   `json:"records,omitempty"`
}

func (r SourceReport) Validate(now time.Time) error {
	if len(r.Records) > 10 {
		return errors.New("too many source records")
	}
	for _, record := range r.Records {
		raw, err := json.Marshal(record)
		if err != nil || len(record) == 0 || len(raw) > 32768 || (r.State != "ready" && r.State != "partial") {
			return errors.New("invalid source record")
		}
	}
	for name, value := range r.Metrics {
		if (name != "uptime_ticks" && name != "interface_count") || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxUint32 || math.Trunc(value) != value || (r.State != "ready" && r.State != "partial") {
			return errors.New("invalid SNMP metrics")
		}
	}
	switch r.State {
	case "ready", "partial", "auth_failed", "unreachable", "unsupported", "policy_blocked", "stale":
	default:
		return errors.New("invalid source state")
	}
	if r.ObservedAt.IsZero() || r.ObservedAt.After(now.Add(time.Minute)) || r.ObservedAt.Before(now.Add(-24*time.Hour)) {
		return errors.New("report observation must be within the last day")
	}
	if r.LastContactAt != nil && (r.LastContactAt.IsZero() || r.LastContactAt.After(r.ObservedAt)) {
		return errors.New("invalid source contact time")
	}
	if (r.State == "ready" || r.State == "partial") && r.LastContactAt == nil {
		return errors.New("ready or partial requires source contact evidence")
	}
	if r.QueueDepth < 0 || r.LagMillis < 0 {
		return errors.New("negative queue or lag")
	}
	return nil
}

func EffectiveSourceState(state string, contact *time.Time, staleAfter int, now time.Time) string {
	if (state == "ready" || state == "partial") && (contact == nil || now.Sub(*contact) > time.Duration(staleAfter)*time.Second) {
		return "stale"
	}
	return state
}

func CollectorFreshness(status string, heartbeat *time.Time, now time.Time) string {
	if status == "disabled" {
		return "disabled"
	}
	if heartbeat == nil {
		return "not_reporting"
	}
	if now.Sub(*heartbeat) > 5*time.Minute {
		return "stale"
	}
	return status
}
