package contentpacks

import (
	"fmt"
	"strings"
	"time"
)

type CoverageState string

type CoverageStateDefinition struct {
	State       CoverageState `json:"state"`
	Description string        `json:"description"`
	Healthy     bool          `json:"healthy"`
	Degraded    bool          `json:"degraded"`
	Terminal    bool          `json:"terminal"`
}

type SourceRuntimeState struct {
	SourceInstanceID string               `json:"source_instance_id"`
	PackID           string               `json:"pack_id"`
	PackVersion      string               `json:"pack_version"`
	SourceID         string               `json:"source_id"`
	DisplayName      string               `json:"display_name,omitempty"`
	NodeID           string               `json:"node_id,omitempty"`
	CollectorID      string               `json:"collector_id,omitempty"`
	CollectorMode    string               `json:"collector_mode,omitempty"`
	ParserID         string               `json:"parser_id,omitempty"`
	CoverageState    CoverageState        `json:"coverage_state"`
	ApprovalRequired bool                 `json:"approval_required"`
	ApprovalID       string               `json:"approval_id,omitempty"`
	ConfigVersion    string               `json:"config_version,omitempty"`
	ContentVersion   string               `json:"content_version,omitempty"`
	LastEventAt      *time.Time           `json:"last_event_at,omitempty"`
	LastParsedAt     *time.Time           `json:"last_parsed_at,omitempty"`
	LastHealthAt     *time.Time           `json:"last_health_at,omitempty"`
	LastError        string               `json:"last_error,omitempty"`
	Metrics          SourceRuntimeMetrics `json:"metrics,omitempty"`
	Labels           map[string]string    `json:"labels,omitempty"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type SourceRuntimeMetrics struct {
	EventsReceived  int64 `json:"events_received,omitempty"`
	EventsParsed    int64 `json:"events_parsed,omitempty"`
	EventsDropped   int64 `json:"events_dropped,omitempty"`
	ParseFailures   int64 `json:"parse_failures,omitempty"`
	LagMillis       int64 `json:"lag_millis,omitempty"`
	QueueDepth      int64 `json:"queue_depth,omitempty"`
	CursorAgeMillis int64 `json:"cursor_age_millis,omitempty"`
	RetryCount      int64 `json:"retry_count,omitempty"`
}

var coverageStateDefinitions = []CoverageStateDefinition{
	{
		State:       CoverageState(CoverageDiscovered),
		Description: "A source was observed locally or through an edge collector, but no connector proposal has been created.",
	},
	{
		State:       CoverageState(CoverageProposed),
		Description: "A connector proposal exists and is waiting for policy or operator decision.",
	},
	{
		State:       CoverageState(CoverageApprovalRequired),
		Description: "Collection is blocked until an operator approval or bank policy grant exists.",
		Degraded:    true,
	},
	{
		State:       CoverageState(CoverageApproved),
		Description: "The source is approved for collection, but collector configuration has not been rendered yet.",
	},
	{
		State:       CoverageState(CoverageConfigRendered),
		Description: "Collector configuration has been generated and is ready for deployment.",
	},
	{
		State:       CoverageState(CoverageDeployed),
		Description: "Collector configuration was deployed, but event flow has not been proven yet.",
	},
	{
		State:       CoverageState(CoverageCollecting),
		Description: "Events are arriving from the source, but parser health has not been fully proven.",
		Healthy:     true,
	},
	{
		State:       CoverageState(CoverageParserHealthy),
		Description: "Events are arriving and parsing successfully through the configured content pack.",
		Healthy:     true,
	},
	{
		State:       CoverageState(CoverageParserFailed),
		Description: "Events are arriving or replaying, but the configured parser is failing.",
		Degraded:    true,
	},
	{
		State:       CoverageState(CoverageSilent),
		Description: "The source is deployed but not producing events inside the expected freshness window.",
		Degraded:    true,
	},
	{
		State:       CoverageState(CoverageBackpressured),
		Description: "Collection exists but queue, retry, lag, or drop signals show delivery pressure.",
		Degraded:    true,
	},
	{
		State:       CoverageState(CoverageCollectionConflict),
		Description: "The same source instance appears active through more than one collection owner without explicit migration dual-write approval.",
		Degraded:    true,
	},
	{
		State:       CoverageState(CoverageUnsupported),
		Description: "The source has no supported collection or parsing path in the active content set.",
		Degraded:    true,
		Terminal:    true,
	},
	{
		State:       CoverageState(CoveragePrivacyBlocked),
		Description: "Collection is intentionally blocked by privacy, sensitivity, or bank policy.",
		Degraded:    true,
		Terminal:    true,
	},
	{
		State:       CoverageState(CoverageStale),
		Description: "Previously valid coverage is stale because heartbeats, events, configs, or content versions aged out.",
		Degraded:    true,
	},
}

var coverageTransitions = map[CoverageState]map[CoverageState]struct{}{
	"": {
		CoverageState(CoverageDiscovered):     {},
		CoverageState(CoverageProposed):       {},
		CoverageState(CoverageUnsupported):    {},
		CoverageState(CoveragePrivacyBlocked): {},
	},
	CoverageState(CoverageDiscovered): {
		CoverageState(CoverageProposed):         {},
		CoverageState(CoverageApprovalRequired): {},
		CoverageState(CoverageUnsupported):      {},
		CoverageState(CoveragePrivacyBlocked):   {},
		CoverageState(CoverageStale):            {},
	},
	CoverageState(CoverageProposed): {
		CoverageState(CoverageApprovalRequired): {},
		CoverageState(CoverageApproved):         {},
		CoverageState(CoverageUnsupported):      {},
		CoverageState(CoveragePrivacyBlocked):   {},
		CoverageState(CoverageStale):            {},
	},
	CoverageState(CoverageApprovalRequired): {
		CoverageState(CoverageApproved):       {},
		CoverageState(CoverageUnsupported):    {},
		CoverageState(CoveragePrivacyBlocked): {},
		CoverageState(CoverageStale):          {},
	},
	CoverageState(CoverageApproved): {
		CoverageState(CoverageConfigRendered): {},
		CoverageState(CoverageUnsupported):    {},
		CoverageState(CoveragePrivacyBlocked): {},
		CoverageState(CoverageStale):          {},
	},
	CoverageState(CoverageConfigRendered): {
		CoverageState(CoverageDeployed):           {},
		CoverageState(CoverageApprovalRequired):   {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoverageUnsupported):        {},
		CoverageState(CoveragePrivacyBlocked):     {},
		CoverageState(CoverageStale):              {},
	},
	CoverageState(CoverageDeployed): {
		CoverageState(CoverageCollecting):         {},
		CoverageState(CoverageParserFailed):       {},
		CoverageState(CoverageSilent):             {},
		CoverageState(CoverageBackpressured):      {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoveragePrivacyBlocked):     {},
		CoverageState(CoverageStale):              {},
	},
	CoverageState(CoverageCollecting): {
		CoverageState(CoverageParserHealthy):      {},
		CoverageState(CoverageParserFailed):       {},
		CoverageState(CoverageSilent):             {},
		CoverageState(CoverageBackpressured):      {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoveragePrivacyBlocked):     {},
		CoverageState(CoverageStale):              {},
	},
	CoverageState(CoverageParserHealthy): {
		CoverageState(CoverageCollecting):         {},
		CoverageState(CoverageParserFailed):       {},
		CoverageState(CoverageSilent):             {},
		CoverageState(CoverageBackpressured):      {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoveragePrivacyBlocked):     {},
		CoverageState(CoverageStale):              {},
	},
	CoverageState(CoverageParserFailed): {
		CoverageState(CoverageCollecting):         {},
		CoverageState(CoverageParserHealthy):      {},
		CoverageState(CoverageSilent):             {},
		CoverageState(CoverageBackpressured):      {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoveragePrivacyBlocked):     {},
		CoverageState(CoverageStale):              {},
	},
	CoverageState(CoverageSilent): {
		CoverageState(CoverageCollecting):         {},
		CoverageState(CoverageParserHealthy):      {},
		CoverageState(CoverageBackpressured):      {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoveragePrivacyBlocked):     {},
		CoverageState(CoverageStale):              {},
	},
	CoverageState(CoverageBackpressured): {
		CoverageState(CoverageCollecting):         {},
		CoverageState(CoverageParserHealthy):      {},
		CoverageState(CoverageParserFailed):       {},
		CoverageState(CoverageSilent):             {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoveragePrivacyBlocked):     {},
		CoverageState(CoverageStale):              {},
	},
	CoverageState(CoverageCollectionConflict): {
		CoverageState(CoverageConfigRendered): {},
		CoverageState(CoverageDeployed):       {},
		CoverageState(CoverageCollecting):     {},
		CoverageState(CoverageParserHealthy):  {},
		CoverageState(CoverageParserFailed):   {},
		CoverageState(CoverageSilent):         {},
		CoverageState(CoverageBackpressured):  {},
		CoverageState(CoverageStale):          {},
	},
	CoverageState(CoverageStale): {
		CoverageState(CoverageProposed):           {},
		CoverageState(CoverageCollecting):         {},
		CoverageState(CoverageParserHealthy):      {},
		CoverageState(CoverageParserFailed):       {},
		CoverageState(CoverageSilent):             {},
		CoverageState(CoverageBackpressured):      {},
		CoverageState(CoverageCollectionConflict): {},
		CoverageState(CoverageUnsupported):        {},
		CoverageState(CoveragePrivacyBlocked):     {},
	},
	CoverageState(CoverageUnsupported): {
		CoverageState(CoverageDiscovered): {},
		CoverageState(CoverageProposed):   {},
	},
	CoverageState(CoveragePrivacyBlocked): {
		CoverageState(CoverageProposed):         {},
		CoverageState(CoverageApprovalRequired): {},
	},
}

func CoverageStateDefinitions() []CoverageStateDefinition {
	out := make([]CoverageStateDefinition, len(coverageStateDefinitions))
	copy(out, coverageStateDefinitions)
	return out
}

func NormalizeCoverageState(state string) CoverageState {
	return CoverageState(strings.ToLower(strings.TrimSpace(state)))
}

func ValidCoverageState(state string) bool {
	normalized := NormalizeCoverageState(state)
	if normalized == "" {
		return false
	}
	for _, definition := range coverageStateDefinitions {
		if definition.State == normalized {
			return true
		}
	}
	return false
}

func CanTransitionCoverage(from, to string) bool {
	next := NormalizeCoverageState(to)
	if !ValidCoverageState(string(next)) {
		return false
	}
	current := NormalizeCoverageState(from)
	if current == next {
		return true
	}
	allowed := coverageTransitions[current]
	if len(allowed) == 0 {
		return false
	}
	_, ok := allowed[next]
	return ok
}

func ValidateCoverageTransition(from, to string) error {
	if !ValidCoverageState(to) {
		return fmt.Errorf("unsupported coverage state %q", to)
	}
	if !CanTransitionCoverage(from, to) {
		return fmt.Errorf("invalid coverage transition %q -> %q", from, to)
	}
	return nil
}

func CoverageStateIsHealthy(state string) bool {
	normalized := NormalizeCoverageState(state)
	for _, definition := range coverageStateDefinitions {
		if definition.State == normalized {
			return definition.Healthy
		}
	}
	return false
}

func CoverageStateIsDegraded(state string) bool {
	normalized := NormalizeCoverageState(state)
	for _, definition := range coverageStateDefinitions {
		if definition.State == normalized {
			return definition.Degraded
		}
	}
	return false
}

func CoverageStateRequiresCollector(state string) bool {
	switch NormalizeCoverageState(state) {
	case CoverageState(CoverageDeployed),
		CoverageState(CoverageCollecting),
		CoverageState(CoverageParserHealthy),
		CoverageState(CoverageParserFailed),
		CoverageState(CoverageSilent),
		CoverageState(CoverageBackpressured),
		CoverageState(CoverageCollectionConflict):
		return true
	default:
		return false
	}
}

func CoverageStateRequiresParser(state string) bool {
	switch NormalizeCoverageState(state) {
	case CoverageState(CoverageCollecting),
		CoverageState(CoverageParserHealthy),
		CoverageState(CoverageParserFailed),
		CoverageState(CoverageSilent),
		CoverageState(CoverageBackpressured):
		return true
	default:
		return false
	}
}

func CoverageStateRequiresApprovalRef(state string) bool {
	switch NormalizeCoverageState(state) {
	case CoverageState(CoverageApproved),
		CoverageState(CoverageConfigRendered),
		CoverageState(CoverageDeployed),
		CoverageState(CoverageCollecting),
		CoverageState(CoverageParserHealthy),
		CoverageState(CoverageParserFailed),
		CoverageState(CoverageSilent),
		CoverageState(CoverageBackpressured):
		return true
	default:
		return false
	}
}

func NewSourceRuntimeState(manifest Manifest, source SourceProfile, sourceInstanceID string, now time.Time) SourceRuntimeState {
	sourceInstanceID = strings.TrimSpace(sourceInstanceID)
	if sourceInstanceID == "" {
		sourceInstanceID = defaultSourceInstanceID(manifest.PackID, source.SourceID)
	}
	state := SourceRuntimeState{
		SourceInstanceID: sourceInstanceID,
		PackID:           strings.TrimSpace(manifest.PackID),
		PackVersion:      strings.TrimSpace(manifest.PackVersion),
		SourceID:         strings.TrimSpace(source.SourceID),
		DisplayName:      strings.TrimSpace(source.DisplayName),
		CoverageState:    CoverageState(CoverageDiscovered),
		ApprovalRequired: source.ApprovalRequired,
		ContentVersion:   strings.TrimSpace(manifest.PackVersion),
		UpdatedAt:        now.UTC(),
	}
	if len(source.CollectorModes) > 0 {
		state.CollectorMode = strings.TrimSpace(source.CollectorModes[0])
	}
	if len(source.Parsers) > 0 {
		state.ParserID = strings.TrimSpace(source.Parsers[0])
	}
	return state
}

func (s SourceRuntimeState) Validate() error {
	v := validator{}
	v.requireID("source_instance_id", s.SourceInstanceID)
	v.requireID("pack_id", s.PackID)
	v.requireSemver("pack_version", s.PackVersion)
	v.requireID("source_id", s.SourceID)
	if !ValidCoverageState(string(s.CoverageState)) {
		v.issue("coverage_state", fmt.Sprintf("unsupported value %q", s.CoverageState))
	}
	if strings.TrimSpace(s.CollectorMode) != "" {
		v.requireAllowed("collector_mode", s.CollectorMode, allowedCollectorModes())
	}
	if CoverageStateRequiresCollector(string(s.CoverageState)) {
		if strings.TrimSpace(s.CollectorID) == "" {
			v.issue("collector_id", "is required once source configuration is deployed")
		}
		if strings.TrimSpace(s.CollectorMode) == "" {
			v.issue("collector_mode", "is required once source configuration is deployed")
		}
	}
	if CoverageStateRequiresParser(string(s.CoverageState)) && strings.TrimSpace(s.ParserID) == "" {
		v.issue("parser_id", "is required once collection or parser health is claimed")
	}
	if s.ApprovalRequired && CoverageStateRequiresApprovalRef(string(s.CoverageState)) && strings.TrimSpace(s.ApprovalID) == "" {
		v.issue("approval_id", "is required for approved high-risk or high-sensitivity sources")
	}
	if s.CoverageState == CoverageState(CoverageApprovalRequired) && !s.ApprovalRequired {
		v.issue("approval_required", "must be true when coverage_state is approval_required")
	}
	if s.CoverageState == CoverageState(CoverageParserHealthy) && s.LastParsedAt == nil {
		v.issue("last_parsed_at", "is required when parser health is claimed")
	}
	if s.CoverageState == CoverageState(CoverageParserFailed) && strings.TrimSpace(s.LastError) == "" {
		v.issue("last_error", "is required when parser_failed is claimed")
	}
	if s.CoverageState == CoverageState(CoverageCollectionConflict) && strings.TrimSpace(s.LastError) == "" {
		v.issue("last_error", "is required when collection_conflict is claimed")
	}
	v.validateRuntimeMetrics("metrics", s.Metrics)
	if len(v.issues) > 0 {
		return &ValidationError{Issues: v.issues}
	}
	return nil
}

func (s SourceRuntimeState) Transition(to string, at time.Time) (SourceRuntimeState, error) {
	if err := ValidateCoverageTransition(string(s.CoverageState), to); err != nil {
		return SourceRuntimeState{}, err
	}
	next := s
	next.CoverageState = NormalizeCoverageState(to)
	next.UpdatedAt = at.UTC()
	return next, nil
}

func (v *validator) validateRuntimeMetrics(path string, metrics SourceRuntimeMetrics) {
	if metrics.EventsReceived < 0 {
		v.issue(path+".events_received", "must not be negative")
	}
	if metrics.EventsParsed < 0 {
		v.issue(path+".events_parsed", "must not be negative")
	}
	if metrics.EventsDropped < 0 {
		v.issue(path+".events_dropped", "must not be negative")
	}
	if metrics.ParseFailures < 0 {
		v.issue(path+".parse_failures", "must not be negative")
	}
	if metrics.LagMillis < 0 {
		v.issue(path+".lag_millis", "must not be negative")
	}
	if metrics.QueueDepth < 0 {
		v.issue(path+".queue_depth", "must not be negative")
	}
	if metrics.CursorAgeMillis < 0 {
		v.issue(path+".cursor_age_millis", "must not be negative")
	}
	if metrics.RetryCount < 0 {
		v.issue(path+".retry_count", "must not be negative")
	}
	if metrics.EventsParsed > metrics.EventsReceived {
		v.issue(path+".events_parsed", "must not exceed events_received")
	}
}

func defaultSourceInstanceID(packID, sourceID string) string {
	parts := []string{}
	for _, part := range []string{packID, sourceID} {
		part = strings.Trim(strings.ToLower(strings.TrimSpace(part)), ".-_")
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "source.unknown"
	}
	return strings.Join(parts, ".")
}
