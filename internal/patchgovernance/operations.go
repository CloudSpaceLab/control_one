package patchgovernance

import (
	"fmt"
	"github.com/google/uuid"
	"regexp"
	"strings"
	"time"
)

type Operations struct {
	Owner         string              `json:"owner"`
	Reason        string              `json:"reason"`
	Retention     ReviewRetention     `json:"retention"`
	Notifications ReviewNotifications `json:"notifications"`
	Automation    AutomationPolicy    `json:"automation"`
}
type ReviewRetention struct {
	Mode      string `json:"mode"`
	Days      int    `json:"days"`
	LegalHold bool   `json:"legal_hold"`
}
type ReviewNotifications struct {
	Enabled     bool     `json:"enabled"`
	Channel     string   `json:"channel"`
	Recipients  []string `json:"recipients"`
	RepeatHours int      `json:"repeat_hours"`
}
type AutomationPolicy struct {
	Mode                      string     `json:"mode"`
	Enabled                   bool       `json:"enabled"`
	NodeIDs                   []string   `json:"node_ids"`
	CanaryNodeIDs             []string   `json:"canary_node_ids"`
	Packages                  []string   `json:"packages"`
	Exclusions                []string   `json:"exclusions"`
	WindowID                  string     `json:"window_id"`
	Transport                 string     `json:"transport"`
	MaxWaveSize               int        `json:"max_wave_size"`
	ObservationMinutes        int        `json:"observation_minutes"`
	HealthProfile             string     `json:"health_profile"`
	ValidUntil                *time.Time `json:"valid_until,omitempty"`
	RequireApproval           bool       `json:"require_approval"`
	RequireExactPlan          bool       `json:"require_exact_plan"`
	RequireFreshAssessment    bool       `json:"require_fresh_assessment"`
	RequireHealth             bool       `json:"require_health"`
	RequireRebootVerification bool       `json:"require_reboot_verification"`
	StopOnFailure             bool       `json:"stop_on_failure"`
}

func DefaultOperations() Operations {
	return Operations{Retention: ReviewRetention{Mode: "retain", Days: 90, LegalHold: true}, Notifications: ReviewNotifications{Channel: "in_app", Recipients: []string{}, RepeatHours: 24}, Automation: AutomationPolicy{ObservationMinutes: 60, Mode: "recommend_only", NodeIDs: []string{}, CanaryNodeIDs: []string{}, Packages: []string{}, Exclusions: []string{}, RequireApproval: true, RequireExactPlan: true, RequireFreshAssessment: true, RequireHealth: true, RequireRebootVerification: true, StopOnFailure: true}}
}
func (o Operations) Validate(now time.Time) error {
	if strings.TrimSpace(o.Owner) == "" || len(o.Owner) > 160 || strings.TrimSpace(o.Reason) == "" || len(o.Reason) > 2000 {
		return fmt.Errorf("accountable operations owner and reason are required")
	}
	if !oneOf(o.Retention.Mode, "retain", "archive_then_purge") || o.Retention.Days < 1 || o.Retention.Days > 3650 {
		return fmt.Errorf("retention requires retain or archive_then_purge and 1–3650 days")
	}
	n := o.Notifications
	if n.Channel != "in_app" || n.RepeatHours < 1 || n.RepeatHours > 168 || len(n.Recipients) > 20 {
		return fmt.Errorf("choose in-app delivery, 1–168 repeat hours and at most 20 tenant recipients")
	}
	if n.Enabled && len(n.Recipients) == 0 {
		return fmt.Errorf("enabled notifications require explicit tenant recipients")
	}
	if err := uniqueIDs(n.Recipients); err != nil {
		return err
	}
	a := o.Automation
	if !oneOf(a.Mode, "recommend_only", "approved_scheduled", "automatic") {
		return fmt.Errorf("invalid automation mode")
	}
	if err := a.ValidateActivation(); err != nil {
		return err
	}
	if a.Mode == "recommend_only" {
		return nil
	}
	if len(a.NodeIDs) < 1 || len(a.NodeIDs) > 100 || len(a.CanaryNodeIDs) < 1 || len(a.CanaryNodeIDs) > len(a.NodeIDs) {
		return fmt.Errorf("installation-policy drafts require 1–100 explicit targets and a canary subset")
	}
	if err := uniqueIDs(a.NodeIDs); err != nil {
		return err
	}
	if err := uniqueIDs(a.CanaryNodeIDs); err != nil {
		return err
	}
	targets := map[string]bool{}
	for _, id := range a.NodeIDs {
		targets[id] = true
	}
	for _, id := range a.CanaryNodeIDs {
		if !targets[id] {
			return fmt.Errorf("canary must belong to the explicit target scope")
		}
	}
	if _, err := uuid.Parse(a.WindowID); err != nil {
		return fmt.Errorf("a tenant maintenance window is required")
	}
	if !oneOf(a.Transport, "direct", "proxy", "airgapped") || a.MaxWaveSize < 1 || a.MaxWaveSize > len(a.NodeIDs) || strings.TrimSpace(a.HealthProfile) == "" || len(a.HealthProfile) > 160 {
		return fmt.Errorf("transport, bounded wave size and health/recovery profile are required")
	}
	if a.ValidUntil == nil || !a.ValidUntil.After(now) || a.ValidUntil.After(now.Add(90*24*time.Hour)) {
		return fmt.Errorf("installation-policy draft must expire within 90 days")
	}
	if !a.RequireExactPlan || !a.RequireFreshAssessment || !a.RequireHealth || !a.RequireRebootVerification || !a.StopOnFailure || (a.Mode == "approved_scheduled" && !a.RequireApproval) {
		return fmt.Errorf("exact-plan, assessment, health, reboot, failure-stop and scheduled approval controls cannot be disabled")
	}
	if len(a.Packages) < 1 || len(a.Packages) > 100 || len(a.Exclusions) > 100 {
		return fmt.Errorf("choose an explicit package scope of 1–100 names")
	}
	seen := map[string]bool{}
	name := regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9_-]+)?$`)
	for _, p := range a.Packages {
		if !name.MatchString(p) || seen[p] {
			return fmt.Errorf("invalid or duplicate package name")
		}
		seen[p] = true
	}
	excluded := map[string]bool{}
	for _, p := range a.Exclusions {
		if !name.MatchString(p) || excluded[p] || seen[p] {
			return fmt.Errorf("invalid, duplicate or contradictory package exclusion")
		}
		excluded[p] = true
	}
	return nil
}
func uniqueIDs(ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil || parsed.String() != id || seen[id] {
			return fmt.Errorf("recipient and target IDs must be unique canonical UUIDs")
		}
		seen[id] = true
	}
	return nil
}
