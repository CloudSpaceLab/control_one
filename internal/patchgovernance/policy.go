// Package patchgovernance evaluates operator-owned review policies. It never
// dispatches installations or treats a recommendation as approval.
package patchgovernance

import (
	"fmt"
	"strings"
	"time"
)

type Rule struct {
	Name       string `json:"name"`
	Severity   string `json:"minimum_severity"`
	RequireKEV bool   `json:"require_kev"`
	Exposure   string `json:"exposure"`
	Importance string `json:"importance"`
	Priority   string `json:"priority"`
	DueHours   int    `json:"due_hours"`
}

type Policy struct {
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	Mode          string `json:"mode"`
	Rules         []Rule `json:"rules"`
	RetentionDays int    `json:"retention_days"`
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 || strings.TrimSpace(p.Owner) == "" || len(p.Owner) > 160 {
		return fmt.Errorf("policy name and accountable owner/team are required")
	}
	if p.Mode != "recommend_only" {
		return fmt.Errorf("only recommend_only is qualified; scheduled or automatic installation is unavailable")
	}
	if p.RetentionDays < 1 || p.RetentionDays > 3650 {
		return fmt.Errorf("choose evidence retention of 1–3650 days")
	}
	if len(p.Rules) < 1 || len(p.Rules) > 25 {
		return fmt.Errorf("choose 1–25 ordered priority rules")
	}
	seen := map[string]bool{}
	for _, r := range p.Rules {
		if strings.TrimSpace(r.Name) == "" || len(r.Name) > 100 || seen[r.Name] {
			return fmt.Errorf("rule names must be unique and nonempty")
		}
		seen[r.Name] = true
		if severity(r.Severity) < 0 || !oneOf(r.Exposure, "any", "exposed", "internal") || !oneOf(r.Importance, "any", "critical", "standard") || !oneOf(r.Priority, "urgent", "high", "normal", "low") || r.DueHours < 1 || r.DueHours > 8760 {
			return fmt.Errorf("invalid priority rule %q", r.Name)
		}
	}
	return nil
}

type Input struct {
	Severity        string    `json:"severity"`
	KEV             bool      `json:"kev"`
	Exposure        string    `json:"exposure"`
	Importance      string    `json:"importance"`
	Compatibility   string    `json:"compatibility"`
	EvidenceCurrent bool      `json:"evidence_current"`
	FirstSeen       time.Time `json:"first_seen_at"`
}
type Decision struct {
	Priority               string     `json:"priority"`
	Rule                   string     `json:"rule,omitempty"`
	Reasons                []string   `json:"reasons"`
	DueAt                  *time.Time `json:"due_at,omitempty"`
	Overdue                bool       `json:"overdue"`
	InstallationAuthorized bool       `json:"installation_authorized"`
}

func Evaluate(p Policy, in Input, now time.Time) Decision {
	d := Decision{Priority: "review_required", Reasons: []string{}, InstallationAuthorized: false}
	if p.Validate() != nil {
		d.Reasons = append(d.Reasons, "No valid tenant urgency policy is configured.")
		return d
	}
	if !in.EvidenceCurrent {
		d.Reasons = append(d.Reasons, "Current trusted finding evidence is required; do not infer priority from a newer package.")
	}
	if in.FirstSeen.IsZero() || in.FirstSeen.After(now) {
		d.Reasons = append(d.Reasons, "A valid first-observation time is required to calculate a deadline.")
	}
	if severity(in.Severity) < 0 {
		d.Reasons = append(d.Reasons, "Severity is unknown; no severity-based priority rule can be applied. Obtain trusted severity evidence before assigning urgency.")
	}
	if len(d.Reasons) > 0 {
		return d
	}
	for _, r := range p.Rules {
		if severity(in.Severity) < severity(r.Severity) || r.RequireKEV && !in.KEV {
			continue
		}
		if r.Exposure != "any" && r.Exposure != in.Exposure {
			continue
		}
		if r.Importance != "any" && r.Importance != in.Importance {
			continue
		}
		due := in.FirstSeen.Add(time.Duration(r.DueHours) * time.Hour)
		d.Priority = r.Priority
		d.Rule = r.Name
		d.DueAt = &due
		d.Overdue = !due.After(now)
		d.Reasons = append(d.Reasons, fmt.Sprintf("Tenant rule %q matched severity %s; known exploitation=%t; exposure=%s; importance=%s.", r.Name, in.Severity, in.KEV, in.Exposure, in.Importance), fmt.Sprintf("Deadline is first finding observation plus %d tenant-configured hours; it does not reset on refresh.", r.DueHours))
		break
	}
	if d.Rule == "" {
		d.Reasons = append(d.Reasons, "No tenant rule matches the available evidence; operator review is required.")
	}
	if !oneOf(in.Exposure, "exposed", "internal") {
		d.Reasons = append(d.Reasons, "Exposure is unknown; exposure-specific rules were not matched.")
	}
	if !oneOf(in.Importance, "critical", "standard") {
		d.Reasons = append(d.Reasons, "Asset/service importance is unknown; importance-specific rules were not matched.")
	}
	if in.Compatibility != "reviewed" {
		d.Reasons = append(d.Reasons, "Compatibility is unverified or blocked; resolve before any installation approval.")
	}
	d.Reasons = append(d.Reasons, "Recommendation only: no job, approval or automatic promotion is authorized.")
	return d
}
func severity(s string) int {
	switch s {
	case "info":
		return 0
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	}
	return -1
}
func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}

type Tracking struct {
	ContextExpiresAt *time.Time `json:"context_expires_at,omitempty"`
	Owner            string     `json:"owner"`
	EscalateTo       string     `json:"escalate_to"`
	Exposure         string     `json:"exposure"`
	Importance       string     `json:"importance"`
	Compatibility    string     `json:"compatibility"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	ExceptionUntil   *time.Time `json:"exception_until,omitempty"`
	Reason           string     `json:"reason"`
}

func (t Tracking) Validate(now time.Time) error {
	if t.ContextExpiresAt != nil && (!t.ContextExpiresAt.After(now) || t.ContextExpiresAt.After(now.Add(90*24*time.Hour))) {
		return fmt.Errorf("reviewed exposure, importance and compatibility must expire within 90 days")
	}
	if (t.Exposure != "unknown" || t.Importance != "unknown" || t.Compatibility != "unknown") && t.ContextExpiresAt == nil {
		return fmt.Errorf("set an expiry for reviewed context; otherwise leave it unknown")
	}
	if strings.TrimSpace(t.Owner) == "" || len(t.Owner) > 160 || strings.TrimSpace(t.EscalateTo) == "" || len(t.EscalateTo) > 160 || strings.TrimSpace(t.Reason) == "" || len(t.Reason) > 2000 {
		return fmt.Errorf("owner/team, escalation destination and review reason are required")
	}
	if !oneOf(t.Exposure, "unknown", "exposed", "internal") || !oneOf(t.Importance, "unknown", "critical", "standard") || !oneOf(t.Compatibility, "unknown", "reviewed", "blocked") {
		return fmt.Errorf("invalid review context")
	}
	if t.DueAt != nil && (t.DueAt.IsZero() || t.DueAt.After(now.Add(366*24*time.Hour))) {
		return fmt.Errorf("deadline must be within the next year or a recorded overdue date")
	}
	if t.ExceptionUntil != nil && (!t.ExceptionUntil.After(now) || t.ExceptionUntil.After(now.Add(90*24*time.Hour))) {
		return fmt.Errorf("exception must expire within 90 days")
	}
	return nil
}
