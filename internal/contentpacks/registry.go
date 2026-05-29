package contentpacks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PackStatus string

type PackRecord struct {
	PackID             string            `json:"pack_id"`
	PackVersion        string            `json:"pack_version"`
	DisplayName        string            `json:"display_name,omitempty"`
	Status             PackStatus        `json:"status"`
	Compatible         bool              `json:"compatible"`
	CompatibilityError string            `json:"compatibility_error,omitempty"`
	InstalledAt        time.Time         `json:"installed_at"`
	EnabledAt          *time.Time        `json:"enabled_at,omitempty"`
	DisabledAt         *time.Time        `json:"disabled_at,omitempty"`
	QuarantinedAt      *time.Time        `json:"quarantined_at,omitempty"`
	QuarantineReason   string            `json:"quarantine_reason,omitempty"`
	SourceCount        int               `json:"source_count"`
	ParserCount        int               `json:"parser_count"`
	DetectionCount     int               `json:"detection_count"`
	SampleCount        int               `json:"sample_count"`
	Labels             map[string]string `json:"labels,omitempty"`
	Manifest           Manifest          `json:"manifest"`
}

type RegistrySnapshot struct {
	SchemaVersion     int          `json:"schema_version"`
	ControlOneVersion string       `json:"control_one_version,omitempty"`
	ExportedAt        time.Time    `json:"exported_at"`
	Packs             []PackRecord `json:"packs"`
}

type Registry struct {
	controlOneVersion string
	packs             map[packKey]*PackRecord
}

type ResolvedSource struct {
	PackID        string
	PackVersion   string
	Source        SourceProfile
	Parsers       []ParserProfile
	Detections    []Detection
	Samples       []SampleCase
	Manifest      Manifest
	ContentStatus PackStatus
}

type packKey struct {
	id      string
	version string
}

func NewRegistry(controlOneVersion string) *Registry {
	return &Registry{
		controlOneVersion: strings.TrimSpace(controlOneVersion),
		packs:             map[packKey]*PackRecord{},
	}
}

func NewRegistryFromSnapshot(snapshot RegistrySnapshot, controlOneVersion string) (*Registry, error) {
	if snapshot.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported content pack registry snapshot schema_version %d", snapshot.SchemaVersion)
	}
	controlOneVersion = strings.TrimSpace(controlOneVersion)
	if controlOneVersion == "" {
		controlOneVersion = strings.TrimSpace(snapshot.ControlOneVersion)
	}
	registry := NewRegistry(controlOneVersion)
	for _, record := range snapshot.Packs {
		restored, err := restorePackRecord(record, controlOneVersion)
		if err != nil {
			return nil, err
		}
		key := keyFor(restored.PackID, restored.PackVersion)
		if _, exists := registry.packs[key]; exists {
			return nil, fmt.Errorf("duplicate content pack snapshot record %s@%s", key.id, key.version)
		}
		registry.packs[key] = restored
	}
	if conflict := registry.enabledSourceConflictInSnapshot(); conflict != "" {
		return nil, fmt.Errorf("content pack registry snapshot has enabled source conflict %s", conflict)
	}
	return registry, nil
}

func (r *Registry) Snapshot(at time.Time) RegistrySnapshot {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	snapshot := RegistrySnapshot{
		SchemaVersion:     SchemaVersion,
		ControlOneVersion: "",
		ExportedAt:        at.UTC(),
	}
	if r == nil {
		return snapshot
	}
	snapshot.ControlOneVersion = r.controlOneVersion
	snapshot.Packs = r.List()
	return snapshot
}

func (r *Registry) Install(manifest Manifest, at time.Time) (*PackRecord, error) {
	if r == nil {
		return nil, fmt.Errorf("content pack registry is nil")
	}
	if err := Validate(manifest); err != nil {
		return nil, err
	}
	if err := CheckCompatibility(manifest, r.controlOneVersion); err != nil {
		return nil, err
	}
	key := keyFor(manifest.PackID, manifest.PackVersion)
	if _, exists := r.packs[key]; exists {
		return nil, fmt.Errorf("content pack %s@%s already installed", key.id, key.version)
	}
	record := newPackRecord(manifest, PackStatus(PackStatusInstalled), at)
	record.Compatible = true
	r.packs[key] = &record
	return clonePackRecord(record), nil
}

func (r *Registry) Enable(packID, packVersion string, at time.Time) (*PackRecord, error) {
	record, err := r.getMutable(packID, packVersion)
	if err != nil {
		return nil, err
	}
	if record.Status == PackStatus(PackStatusQuarantined) {
		return nil, fmt.Errorf("content pack %s@%s is quarantined: %s", record.PackID, record.PackVersion, record.QuarantineReason)
	}
	if record.Status == PackStatus(PackStatusDeprecated) {
		return nil, fmt.Errorf("content pack %s@%s is deprecated", record.PackID, record.PackVersion)
	}
	if err := CheckCompatibility(record.Manifest, r.controlOneVersion); err != nil {
		record.Compatible = false
		record.CompatibilityError = err.Error()
		return nil, err
	}
	if conflict := r.enabledSourceConflict(record); conflict != "" {
		return nil, fmt.Errorf("content pack %s@%s conflicts with enabled source %s", record.PackID, record.PackVersion, conflict)
	}
	for key, existing := range r.packs {
		if key.id != strings.TrimSpace(record.PackID) || key.version == strings.TrimSpace(record.PackVersion) {
			continue
		}
		if existing.Status == PackStatus(PackStatusEnabled) {
			existing.Status = PackStatus(PackStatusRollbackAvailable)
			existing.DisabledAt = timePtr(at)
		}
	}
	record.Status = PackStatus(PackStatusEnabled)
	record.EnabledAt = timePtr(at)
	record.DisabledAt = nil
	record.Compatible = true
	record.CompatibilityError = ""
	return clonePackRecord(*record), nil
}

func (r *Registry) Disable(packID, packVersion string, at time.Time) (*PackRecord, error) {
	record, err := r.getMutable(packID, packVersion)
	if err != nil {
		return nil, err
	}
	if record.Status == PackStatus(PackStatusQuarantined) {
		return nil, fmt.Errorf("content pack %s@%s is quarantined", record.PackID, record.PackVersion)
	}
	record.Status = PackStatus(PackStatusDisabled)
	record.DisabledAt = timePtr(at)
	return clonePackRecord(*record), nil
}

func (r *Registry) Quarantine(packID, packVersion, reason string, at time.Time) (*PackRecord, error) {
	record, err := r.getMutable(packID, packVersion)
	if err != nil {
		return nil, err
	}
	record.Status = PackStatus(PackStatusQuarantined)
	record.QuarantinedAt = timePtr(at)
	record.QuarantineReason = strings.TrimSpace(reason)
	record.EnabledAt = nil
	return clonePackRecord(*record), nil
}

func (r *Registry) Deprecate(packID, packVersion string, at time.Time) (*PackRecord, error) {
	record, err := r.getMutable(packID, packVersion)
	if err != nil {
		return nil, err
	}
	record.Status = PackStatus(PackStatusDeprecated)
	record.DisabledAt = timePtr(at)
	return clonePackRecord(*record), nil
}

func (r *Registry) Get(packID, packVersion string) (PackRecord, bool) {
	if r == nil {
		return PackRecord{}, false
	}
	record := r.packs[keyFor(packID, packVersion)]
	if record == nil {
		return PackRecord{}, false
	}
	return *clonePackRecord(*record), true
}

func (r *Registry) List() []PackRecord {
	if r == nil {
		return nil
	}
	out := make([]PackRecord, 0, len(r.packs))
	for _, record := range r.packs {
		out = append(out, *clonePackRecord(*record))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PackID != out[j].PackID {
			return out[i].PackID < out[j].PackID
		}
		return CompareSemver(out[i].PackVersion, out[j].PackVersion) > 0
	})
	return out
}

func (r *Registry) ResolveSource(sourceID string) (ResolvedSource, bool) {
	sourceID = strings.TrimSpace(sourceID)
	if r == nil || sourceID == "" {
		return ResolvedSource{}, false
	}
	var best *PackRecord
	var bestSource SourceProfile
	for _, record := range r.packs {
		if record.Status != PackStatus(PackStatusEnabled) {
			continue
		}
		source, ok := sourceByID(record.Manifest, sourceID)
		if !ok {
			continue
		}
		if best == nil || CompareSemver(record.PackVersion, best.PackVersion) > 0 {
			best = record
			bestSource = source
		}
	}
	if best == nil {
		return ResolvedSource{}, false
	}
	return resolveFromRecord(*best, bestSource), true
}

func CheckCompatibility(manifest Manifest, controlOneVersion string) error {
	minVersion := strings.TrimSpace(manifest.MinControlOneVersion)
	if minVersion == "" {
		return nil
	}
	if !semverPattern.MatchString(minVersion) {
		return fmt.Errorf("content pack %s@%s has invalid min_control_one_version %q", manifest.PackID, manifest.PackVersion, minVersion)
	}
	controlOneVersion = strings.TrimSpace(controlOneVersion)
	if controlOneVersion == "" {
		return fmt.Errorf("content pack %s@%s requires Control One >= %s but current version is unknown", manifest.PackID, manifest.PackVersion, minVersion)
	}
	if !semverPattern.MatchString(controlOneVersion) {
		return fmt.Errorf("current Control One version %q is not semantic", controlOneVersion)
	}
	if CompareSemver(controlOneVersion, minVersion) < 0 {
		return fmt.Errorf("content pack %s@%s requires Control One >= %s, current %s", manifest.PackID, manifest.PackVersion, minVersion, controlOneVersion)
	}
	return nil
}

func CompareSemver(a, b string) int {
	av := parseSemverCore(a)
	bv := parseSemverCore(b)
	for i := 0; i < 3; i++ {
		if av[i] > bv[i] {
			return 1
		}
		if av[i] < bv[i] {
			return -1
		}
	}
	apre := semverPrerelease(a)
	bpre := semverPrerelease(b)
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	default:
		return strings.Compare(apre, bpre)
	}
}

func (r *Registry) getMutable(packID, packVersion string) (*PackRecord, error) {
	if r == nil {
		return nil, fmt.Errorf("content pack registry is nil")
	}
	key := keyFor(packID, packVersion)
	record := r.packs[key]
	if record == nil {
		return nil, fmt.Errorf("content pack %s@%s is not installed", key.id, key.version)
	}
	return record, nil
}

func (r *Registry) enabledSourceConflict(target *PackRecord) string {
	if target == nil {
		return ""
	}
	targetSources := map[string]struct{}{}
	for _, source := range target.Manifest.Sources {
		targetSources[strings.TrimSpace(source.SourceID)] = struct{}{}
	}
	for _, existing := range r.packs {
		if existing.Status != PackStatus(PackStatusEnabled) {
			continue
		}
		if existing.PackID == target.PackID {
			continue
		}
		for _, source := range existing.Manifest.Sources {
			sourceID := strings.TrimSpace(source.SourceID)
			if _, ok := targetSources[sourceID]; ok {
				return fmt.Sprintf("%s from %s@%s", sourceID, existing.PackID, existing.PackVersion)
			}
		}
	}
	return ""
}

func (r *Registry) enabledSourceConflictInSnapshot() string {
	if r == nil {
		return ""
	}
	owners := map[string]string{}
	for _, record := range r.packs {
		if record.Status != PackStatus(PackStatusEnabled) {
			continue
		}
		for _, source := range record.Manifest.Sources {
			sourceID := strings.TrimSpace(source.SourceID)
			if sourceID == "" {
				continue
			}
			owner := fmt.Sprintf("%s@%s", record.PackID, record.PackVersion)
			if existing := owners[sourceID]; existing != "" && existing != owner {
				return fmt.Sprintf("%s from %s and %s", sourceID, existing, owner)
			}
			owners[sourceID] = owner
		}
	}
	return ""
}

func newPackRecord(manifest Manifest, status PackStatus, at time.Time) PackRecord {
	manifest = CloneManifest(manifest)
	return PackRecord{
		PackID:         strings.TrimSpace(manifest.PackID),
		PackVersion:    strings.TrimSpace(manifest.PackVersion),
		DisplayName:    strings.TrimSpace(manifest.DisplayName),
		Status:         status,
		InstalledAt:    at.UTC(),
		SourceCount:    len(manifest.Sources),
		ParserCount:    len(manifest.Parsers),
		DetectionCount: len(manifest.Detections),
		SampleCount:    len(manifest.Samples),
		Labels:         cloneStringMap(manifest.Labels),
		Manifest:       manifest,
	}
}

func restorePackRecord(record PackRecord, controlOneVersion string) (*PackRecord, error) {
	if err := Validate(record.Manifest); err != nil {
		return nil, err
	}
	record.Manifest = CloneManifest(record.Manifest)
	record.PackID = strings.TrimSpace(record.Manifest.PackID)
	record.PackVersion = strings.TrimSpace(record.Manifest.PackVersion)
	record.DisplayName = strings.TrimSpace(record.Manifest.DisplayName)
	record.SourceCount = len(record.Manifest.Sources)
	record.ParserCount = len(record.Manifest.Parsers)
	record.DetectionCount = len(record.Manifest.Detections)
	record.SampleCount = len(record.Manifest.Samples)
	record.Labels = cloneStringMap(record.Manifest.Labels)
	if _, ok := allowedPackStatuses()[string(record.Status)]; !ok {
		return nil, fmt.Errorf("content pack %s@%s has unsupported snapshot status %q", record.PackID, record.PackVersion, record.Status)
	}
	record.CompatibilityError = ""
	record.Compatible = true
	if err := CheckCompatibility(record.Manifest, controlOneVersion); err != nil {
		record.Compatible = false
		record.CompatibilityError = err.Error()
	}
	return clonePackRecord(record), nil
}

func allowedPackStatuses() map[string]struct{} {
	return stringSet(
		PackStatusAvailable,
		PackStatusInstalled,
		PackStatusEnabled,
		PackStatusDisabled,
		PackStatusQuarantined,
		PackStatusDeprecated,
		PackStatusRollbackAvailable,
	)
}

func resolveFromRecord(record PackRecord, source SourceProfile) ResolvedSource {
	parserIDs := stringSet(source.Parsers...)
	detectionIDs := stringSet(source.Detections...)
	sampleIDs := stringSet(source.Samples...)
	out := ResolvedSource{
		PackID:        record.PackID,
		PackVersion:   record.PackVersion,
		Source:        cloneSourceProfile(source),
		ContentStatus: record.Status,
		Manifest:      CloneManifest(record.Manifest),
	}
	for _, parser := range record.Manifest.Parsers {
		if _, ok := parserIDs[strings.TrimSpace(parser.ParserID)]; ok {
			out.Parsers = append(out.Parsers, cloneParserProfile(parser))
		}
	}
	for _, detection := range record.Manifest.Detections {
		if _, ok := detectionIDs[strings.TrimSpace(detection.DetectionID)]; ok {
			copyDetection := detection
			copyDetection.Tags = cloneStringSlice(detection.Tags)
			out.Detections = append(out.Detections, copyDetection)
		}
	}
	for _, sample := range record.Manifest.Samples {
		if _, ok := sampleIDs[strings.TrimSpace(sample.CaseID)]; ok {
			out.Samples = append(out.Samples, sample)
		}
	}
	return out
}

func sourceByID(manifest Manifest, sourceID string) (SourceProfile, bool) {
	for _, source := range manifest.Sources {
		if strings.TrimSpace(source.SourceID) == sourceID {
			return source, true
		}
	}
	return SourceProfile{}, false
}

func keyFor(packID, packVersion string) packKey {
	return packKey{
		id:      strings.TrimSpace(packID),
		version: strings.TrimSpace(packVersion),
	}
}

func clonePackRecord(record PackRecord) *PackRecord {
	record.Labels = cloneStringMap(record.Labels)
	record.Manifest = CloneManifest(record.Manifest)
	return &record
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	utc := t.UTC()
	return &utc
}

func parseSemverCore(value string) [3]int {
	value = strings.TrimSpace(value)
	if idx := strings.IndexAny(value, "-+"); idx >= 0 {
		value = value[:idx]
	}
	parts := strings.Split(value, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

func semverPrerelease(value string) string {
	value = strings.TrimSpace(value)
	dash := strings.Index(value, "-")
	if dash < 0 {
		return ""
	}
	pre := value[dash+1:]
	if plus := strings.Index(pre, "+"); plus >= 0 {
		pre = pre[:plus]
	}
	return pre
}
