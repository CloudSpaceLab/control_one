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

// ChangeWindow describes a single recurring window during which auto-remediation
// is allowed to run. Semantics are intentionally simple: a window is active if
// "now" falls within any interval that overlaps `Days` of the week (0=Sunday)
// and the `[StartHour, EndHour)` range in the tenant's configured timezone.
// If EndHour <= StartHour the window wraps past midnight.
type ChangeWindow struct {
	Days      []int  `json:"days"`               // 0-6, Sunday=0. Empty = all days.
	StartHour int    `json:"start_hour"`         // 0-23 (inclusive).
	EndHour   int    `json:"end_hour"`           // 0-24 (exclusive). 24 means "end of day".
	Timezone  string `json:"timezone,omitempty"` // IANA tz; default UTC.
	Label     string `json:"label,omitempty"`    // Free-text description for UIs.
}

// TenantRemediationConfig captures the per-tenant safety configuration for
// auto-remediation. A "default" row is synthesised on GET when no explicit row
// exists — that default exactly matches the migration defaults.
type TenantRemediationConfig struct {
	TenantID                 uuid.UUID
	MinApprovalSeverity      string
	ChangeWindows            []ChangeWindow
	CriticalOverride         bool
	CircuitBreakerWindowMin  int
	CircuitBreakerFailPct    int
	CircuitBreakerMinSamples int
	// PatchRequiresApproval gates fleet patch deploys behind the proper
	// approve→dispatch loop (see migration 0092). Default: true — production
	// tenants land on the safe path. Set to false to keep the legacy
	// immediate-dispatch behaviour (lab / non-prod tenants).
	PatchRequiresApproval bool
	UpdatedAt             time.Time
}

// DefaultTenantRemediationConfig returns the baseline config for a tenant that
// has never customised their safety gates. Matches migration 0030 + 0092 defaults.
func DefaultTenantRemediationConfig(tenantID uuid.UUID) TenantRemediationConfig {
	return TenantRemediationConfig{
		TenantID:                 tenantID,
		MinApprovalSeverity:      "high",
		ChangeWindows:            []ChangeWindow{},
		CriticalOverride:         true,
		CircuitBreakerWindowMin:  15,
		CircuitBreakerFailPct:    30,
		CircuitBreakerMinSamples: 5,
		PatchRequiresApproval:    true,
	}
}

// UpdateTenantRemediationConfigParams narrows the patch surface for operator
// API writes.
type UpdateTenantRemediationConfigParams struct {
	MinApprovalSeverity      *string
	ChangeWindows            *[]ChangeWindow
	CriticalOverride         *bool
	CircuitBreakerWindowMin  *int
	CircuitBreakerFailPct    *int
	CircuitBreakerMinSamples *int
	PatchRequiresApproval    *bool
}

// GetTenantRemediationConfig returns the tenant's config, falling back to a
// synthesised default row when no explicit configuration exists. A nil return
// only happens on true database error.
func (s *Store) GetTenantRemediationConfig(ctx context.Context, tenantID uuid.UUID) (*TenantRemediationConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, min_approval_severity, change_windows, critical_override,
		       circuit_breaker_window_min, circuit_breaker_fail_pct,
		       circuit_breaker_min_samples, patch_requires_approval, updated_at
		FROM tenant_remediation_config
		WHERE tenant_id = $1
	`, tenantID)

	var (
		cfg           TenantRemediationConfig
		changeWindows []byte
	)

	if err := row.Scan(
		&cfg.TenantID,
		&cfg.MinApprovalSeverity,
		&changeWindows,
		&cfg.CriticalOverride,
		&cfg.CircuitBreakerWindowMin,
		&cfg.CircuitBreakerFailPct,
		&cfg.CircuitBreakerMinSamples,
		&cfg.PatchRequiresApproval,
		&cfg.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			defaults := DefaultTenantRemediationConfig(tenantID)
			return &defaults, nil
		}
		return nil, fmt.Errorf("get tenant remediation config: %w", err)
	}

	if len(changeWindows) > 0 {
		if err := json.Unmarshal(changeWindows, &cfg.ChangeWindows); err != nil {
			return nil, fmt.Errorf("decode change_windows: %w", err)
		}
	}
	if cfg.ChangeWindows == nil {
		cfg.ChangeWindows = []ChangeWindow{}
	}

	return &cfg, nil
}

// UpsertTenantRemediationConfig upserts the full config for a tenant. Callers
// that want to patch individual fields should load, modify, then call this.
func (s *Store) UpsertTenantRemediationConfig(ctx context.Context, cfg TenantRemediationConfig) (*TenantRemediationConfig, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if cfg.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}

	minSev := strings.TrimSpace(strings.ToLower(cfg.MinApprovalSeverity))
	switch minSev {
	case "low", "medium", "high", "critical":
		// valid
	case "":
		minSev = "high"
	default:
		return nil, fmt.Errorf("invalid min_approval_severity %q (must be low|medium|high|critical)", cfg.MinApprovalSeverity)
	}
	cfg.MinApprovalSeverity = minSev

	if cfg.CircuitBreakerWindowMin <= 0 {
		cfg.CircuitBreakerWindowMin = 15
	}
	if cfg.CircuitBreakerFailPct < 0 || cfg.CircuitBreakerFailPct > 100 {
		return nil, errors.New("circuit_breaker_fail_pct must be between 0 and 100")
	}
	if cfg.CircuitBreakerMinSamples <= 0 {
		cfg.CircuitBreakerMinSamples = 5
	}

	if cfg.ChangeWindows == nil {
		cfg.ChangeWindows = []ChangeWindow{}
	}

	changeWindowsJSON, err := json.Marshal(cfg.ChangeWindows)
	if err != nil {
		return nil, fmt.Errorf("encode change_windows: %w", err)
	}

	now := s.clock().UTC()

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO tenant_remediation_config (
			tenant_id, min_approval_severity, change_windows, critical_override,
			circuit_breaker_window_min, circuit_breaker_fail_pct, circuit_breaker_min_samples,
			patch_requires_approval, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id) DO UPDATE SET
			min_approval_severity       = EXCLUDED.min_approval_severity,
			change_windows              = EXCLUDED.change_windows,
			critical_override           = EXCLUDED.critical_override,
			circuit_breaker_window_min  = EXCLUDED.circuit_breaker_window_min,
			circuit_breaker_fail_pct    = EXCLUDED.circuit_breaker_fail_pct,
			circuit_breaker_min_samples = EXCLUDED.circuit_breaker_min_samples,
			patch_requires_approval     = EXCLUDED.patch_requires_approval,
			updated_at                  = EXCLUDED.updated_at
		RETURNING tenant_id, min_approval_severity, change_windows, critical_override,
		          circuit_breaker_window_min, circuit_breaker_fail_pct,
		          circuit_breaker_min_samples, patch_requires_approval, updated_at
	`,
		cfg.TenantID,
		cfg.MinApprovalSeverity,
		changeWindowsJSON,
		cfg.CriticalOverride,
		cfg.CircuitBreakerWindowMin,
		cfg.CircuitBreakerFailPct,
		cfg.CircuitBreakerMinSamples,
		cfg.PatchRequiresApproval,
		now,
	)

	var (
		out           TenantRemediationConfig
		changeWindows []byte
	)

	if err := row.Scan(
		&out.TenantID,
		&out.MinApprovalSeverity,
		&changeWindows,
		&out.CriticalOverride,
		&out.CircuitBreakerWindowMin,
		&out.CircuitBreakerFailPct,
		&out.CircuitBreakerMinSamples,
		&out.PatchRequiresApproval,
		&out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("upsert tenant remediation config: %w", err)
	}

	if len(changeWindows) > 0 {
		if err := json.Unmarshal(changeWindows, &out.ChangeWindows); err != nil {
			return nil, fmt.Errorf("decode change_windows: %w", err)
		}
	}
	if out.ChangeWindows == nil {
		out.ChangeWindows = []ChangeWindow{}
	}

	return &out, nil
}

// IsInsideChangeWindow reports whether `now` falls inside at least one of the
// configured change windows. When no windows are configured the result is
// true (no gating).
func IsInsideChangeWindow(windows []ChangeWindow, now time.Time) bool {
	if len(windows) == 0 {
		return true
	}
	for _, w := range windows {
		if changeWindowContains(w, now) {
			return true
		}
	}
	return false
}

// NextChangeWindowStart returns the next time at which at least one change
// window opens, looking ahead up to 8 days to cover any weekly schedule. If no
// windows are configured the function returns `now` (always open).
func NextChangeWindowStart(windows []ChangeWindow, now time.Time) time.Time {
	if len(windows) == 0 {
		return now
	}

	best := time.Time{}
	horizon := now.Add(8 * 24 * time.Hour)
	for _, w := range windows {
		candidate := nextWindowOpen(w, now)
		if candidate.IsZero() || candidate.After(horizon) {
			continue
		}
		if best.IsZero() || candidate.Before(best) {
			best = candidate
		}
	}
	if best.IsZero() {
		return now
	}
	return best
}

func changeWindowContains(w ChangeWindow, now time.Time) bool {
	loc := changeWindowLocation(w)
	local := now.In(loc)
	if !dayMatches(w.Days, int(local.Weekday())) {
		return false
	}
	return hourInRange(w.StartHour, w.EndHour, local.Hour(), local.Minute())
}

func nextWindowOpen(w ChangeWindow, now time.Time) time.Time {
	loc := changeWindowLocation(w)
	local := now.In(loc)

	start, end := normaliseHours(w.StartHour, w.EndHour)

	for offset := 0; offset <= 8; offset++ {
		candidate := local.AddDate(0, 0, offset)
		weekday := int(candidate.Weekday())
		if !dayMatches(w.Days, weekday) {
			continue
		}

		openTime := time.Date(candidate.Year(), candidate.Month(), candidate.Day(),
			start, 0, 0, 0, loc)

		if offset == 0 {
			// For today: if we're already inside the window treat now as the
			// opening. Otherwise pick the upcoming open today, or next qualifying day.
			if hourInRange(start, end, local.Hour(), local.Minute()) {
				return local
			}
			if local.Before(openTime) {
				return openTime
			}
			continue
		}
		return openTime
	}
	return time.Time{}
}

func changeWindowLocation(w ChangeWindow) *time.Location {
	tz := strings.TrimSpace(w.Timezone)
	if tz == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

func dayMatches(days []int, weekday int) bool {
	if len(days) == 0 {
		return true
	}
	for _, d := range days {
		if d == weekday {
			return true
		}
	}
	return false
}

func hourInRange(startHour, endHour, hour, minute int) bool {
	start, end := normaliseHours(startHour, endHour)
	if end == start {
		return false
	}
	minutes := hour*60 + minute
	startMin := start * 60
	endMin := end * 60

	if endMin > startMin {
		return minutes >= startMin && minutes < endMin
	}
	// Wraps past midnight.
	return minutes >= startMin || minutes < endMin
}

func normaliseHours(startHour, endHour int) (int, int) {
	if startHour < 0 {
		startHour = 0
	}
	if startHour > 23 {
		startHour = 23
	}
	if endHour <= 0 {
		endHour = 24
	}
	if endHour > 24 {
		endHour = 24
	}
	return startHour, endHour
}

// SeverityAtLeast returns true when `actual` >= `minimum` in the low<medium<high<critical
// ranking. Unknown strings are treated as below low.
func SeverityAtLeast(actual, minimum string) bool {
	return severityRank(actual) >= severityRank(minimum)
}

func severityRank(sev string) int {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 0
	}
}
