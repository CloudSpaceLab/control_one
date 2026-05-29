package contentpacks

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{1,127}$`)
	semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	pathPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@/+:-]*$`)
)

type Issue struct {
	Path    string
	Message string
}

type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "content pack validation failed"
	}
	if len(e.Issues) == 1 {
		return fmt.Sprintf("content pack validation failed: %s: %s", e.Issues[0].Path, e.Issues[0].Message)
	}
	return fmt.Sprintf("content pack validation failed with %d issues: %s: %s", len(e.Issues), e.Issues[0].Path, e.Issues[0].Message)
}

func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse content pack manifest: %w", err)
	}
	if err := Validate(manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func Validate(manifest Manifest) error {
	v := validator{}
	v.validateManifest(manifest)
	if len(v.issues) > 0 {
		return &ValidationError{Issues: v.issues}
	}
	return nil
}

type validator struct {
	issues []Issue
}

func (v *validator) issue(path, message string) {
	v.issues = append(v.issues, Issue{Path: path, Message: message})
}

func (v *validator) validateManifest(manifest Manifest) {
	if manifest.SchemaVersion != SchemaVersion {
		v.issue("schema_version", fmt.Sprintf("must be %d", SchemaVersion))
	}
	v.requireID("pack_id", manifest.PackID)
	v.requireSemver("pack_version", manifest.PackVersion)
	if strings.TrimSpace(manifest.MinControlOneVersion) != "" {
		v.requireSemver("min_control_one_version", manifest.MinControlOneVersion)
	}
	if strings.TrimSpace(manifest.DisplayName) == "" {
		v.issue("display_name", "is required")
	}
	if strings.TrimSpace(manifest.License.SPDX) == "" && strings.TrimSpace(manifest.License.Name) == "" {
		v.issue("license", "requires either spdx or name")
	}
	if strings.TrimSpace(manifest.Provenance.Author) == "" && strings.TrimSpace(manifest.Provenance.Repository) == "" {
		v.issue("provenance", "requires author or repository")
	}
	if len(manifest.Sources) == 0 {
		v.issue("sources", "requires at least one source profile")
	}
	if len(manifest.Parsers) == 0 {
		v.issue("parsers", "requires at least one parser profile")
	}
	if len(manifest.Samples) == 0 {
		v.issue("samples", "requires at least one sample case with golden output")
	}

	parserIDs := map[string]struct{}{}
	for i, parser := range manifest.Parsers {
		path := fmt.Sprintf("parsers[%d]", i)
		if v.requireID(path+".parser_id", parser.ParserID) {
			v.requireUnique(parserIDs, path+".parser_id", parser.ParserID)
		}
		v.validateParser(path, parser)
	}

	detectionIDs := map[string]struct{}{}
	for i, detection := range manifest.Detections {
		path := fmt.Sprintf("detections[%d]", i)
		if v.requireID(path+".detection_id", detection.DetectionID) {
			v.requireUnique(detectionIDs, path+".detection_id", detection.DetectionID)
		}
		if strings.TrimSpace(detection.Kind) == "" {
			v.issue(path+".kind", "is required")
		} else {
			v.requireAllowed(path+".kind", detection.Kind, allowedDetectionKinds())
		}
		if detection.Kind == DetectionKindSigma && strings.TrimSpace(detection.Path) == "" {
			v.issue(path+".path", "is required for sigma detections")
		}
		if strings.TrimSpace(detection.Path) != "" && !pathPattern.MatchString(detection.Path) {
			v.issue(path+".path", "contains unsupported characters")
		}
		if detection.RiskScore < 0 || detection.RiskScore > 100 {
			v.issue(path+".risk_score", "must be 0-100")
		}
		v.validateDetectionTemporal(path+".temporal", detection.Temporal)
	}

	sourceIDs := map[string]struct{}{}
	sourceParsers := map[string][]string{}
	sourceParserRefs := map[string]map[string]struct{}{}
	sourceSamples := map[string][]string{}
	for i, source := range manifest.Sources {
		path := fmt.Sprintf("sources[%d]", i)
		if v.requireID(path+".source_id", source.SourceID) {
			v.requireUnique(sourceIDs, path+".source_id", source.SourceID)
		}
		v.validateSource(path, source, parserIDs, detectionIDs)
		sourceParsers[source.SourceID] = append([]string(nil), source.Parsers...)
		sourceParserRefs[source.SourceID] = stringSet(source.Parsers...)
		sourceSamples[source.SourceID] = append([]string(nil), source.Samples...)
	}

	sampleIDs := map[string]struct{}{}
	sampleSourceIDs := map[string]string{}
	sampleCoverage := map[string]map[string]struct{}{}
	for i, sample := range manifest.Samples {
		path := fmt.Sprintf("samples[%d]", i)
		if v.requireID(path+".case_id", sample.CaseID) {
			v.requireUnique(sampleIDs, path+".case_id", sample.CaseID)
		}
		v.validateSample(path, sample, sourceIDs, parserIDs, sourceParserRefs, sampleCoverage)
		sampleSourceIDs[strings.TrimSpace(sample.CaseID)] = strings.TrimSpace(sample.SourceID)
	}
	v.validateSourceSampleRefs(sourceSamples, sampleSourceIDs)
	v.validateSampleCoverage(sourceParsers, sampleCoverage)
}

func (v *validator) validateDetectionTemporal(path string, temporal *DetectionTemporal) {
	if temporal == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(temporal.Kind)) {
	case "":
		v.issue(path+".kind", "is required")
	case "threshold":
		if temporal.WindowSeconds <= 0 {
			v.issue(path+".window_seconds", "must be positive")
		}
		if temporal.Threshold <= 0 {
			v.issue(path+".threshold", "must be positive")
		}
		if temporal.SuppressForSeconds < 0 {
			v.issue(path+".suppress_for_seconds", "must be non-negative")
		}
		for i, field := range temporal.GroupBy {
			if strings.TrimSpace(field) == "" {
				v.issue(fmt.Sprintf("%s.group_by[%d]", path, i), "is required")
			}
		}
	case "sequence":
		if temporal.WindowSeconds <= 0 {
			v.issue(path+".window_seconds", "must be positive")
		}
		if temporal.SuppressForSeconds < 0 {
			v.issue(path+".suppress_for_seconds", "must be non-negative")
		}
		if len(temporal.Sequence) < 2 {
			v.issue(path+".sequence", "requires at least two steps")
		}
		for i, field := range temporal.GroupBy {
			if strings.TrimSpace(field) == "" {
				v.issue(fmt.Sprintf("%s.group_by[%d]", path, i), "is required")
			}
		}
		for i, step := range temporal.Sequence {
			stepPath := fmt.Sprintf("%s.sequence[%d]", path, i)
			if strings.TrimSpace(step.Field) == "" {
				v.issue(stepPath+".field", "is required")
			}
			op := strings.TrimSpace(step.Op)
			if op == "" {
				op = "equals"
			}
			if !allowedTemporalStepOps()[op] {
				v.issue(stepPath+".op", fmt.Sprintf("unsupported value %q", step.Op))
			}
			if op != "exists" && len(step.Values) == 0 {
				v.issue(stepPath+".values", "requires at least one value")
			}
		}
	case "join":
		if temporal.WindowSeconds <= 0 {
			v.issue(path+".window_seconds", "must be positive")
		}
		if temporal.SuppressForSeconds < 0 {
			v.issue(path+".suppress_for_seconds", "must be non-negative")
		}
		if len(temporal.Join) < 2 {
			v.issue(path+".join", "requires at least two steps")
		}
		for i, field := range temporal.GroupBy {
			if strings.TrimSpace(field) == "" {
				v.issue(fmt.Sprintf("%s.group_by[%d]", path, i), "is required")
			}
		}
		for i, step := range temporal.Join {
			v.validateDetectionTemporalStep(fmt.Sprintf("%s.join[%d]", path, i), step)
		}
	default:
		v.issue(path+".kind", "must be threshold, sequence, or join")
	}
}

func (v *validator) validateDetectionTemporalStep(path string, step DetectionTemporalStep) {
	if strings.TrimSpace(step.Field) == "" {
		v.issue(path+".field", "is required")
	}
	op := strings.TrimSpace(step.Op)
	if op == "" {
		op = "equals"
	}
	if !allowedTemporalStepOps()[op] {
		v.issue(path+".op", fmt.Sprintf("unsupported value %q", step.Op))
	}
	if op != "exists" && len(step.Values) == 0 {
		v.issue(path+".values", "requires at least one value")
	}
}

func (v *validator) validateSource(path string, source SourceProfile, parserIDs, detectionIDs map[string]struct{}) {
	if strings.TrimSpace(source.DisplayName) == "" {
		v.issue(path+".display_name", "is required")
	}
	if strings.TrimSpace(source.Product) == "" {
		v.issue(path+".product", "is required")
	}
	if strings.TrimSpace(source.SourceClass) == "" {
		v.issue(path+".source_class", "is required")
	}
	v.requireAllowed(path+".risk_class", source.RiskClass, allowedRiskClasses())
	v.requireAllowed(path+".data_sensitivity", source.DataSensitivity, allowedDataSensitivity())
	if len(source.CollectorModes) == 0 {
		v.issue(path+".collector_modes", "requires at least one collector mode")
	}
	seenModes := map[string]struct{}{}
	for i, mode := range source.CollectorModes {
		modePath := fmt.Sprintf("%s.collector_modes[%d]", path, i)
		if v.requireAllowed(modePath, mode, allowedCollectorModes()) {
			v.requireUnique(seenModes, modePath, mode)
		}
	}
	for i, recipe := range source.CollectorRecipes {
		recipePath := fmt.Sprintf("%s.collector_recipes[%d]", path, i)
		v.requireAllowed(recipePath+".mode", recipe.Mode, allowedCollectorModes())
		if strings.TrimSpace(recipe.Receiver) == "" && strings.TrimSpace(recipe.Exporter) == "" {
			v.issue(recipePath, "requires receiver or exporter")
		}
	}
	if requiresApproval(source.RiskClass, source.DataSensitivity) && !source.ApprovalRequired {
		v.issue(path+".approval_required", "must be true for high/critical risk or high/restricted sensitivity sources")
	}
	if len(source.Parsers) == 0 {
		v.issue(path+".parsers", "requires at least one parser reference")
	}
	seenParsers := map[string]struct{}{}
	for i, parserID := range source.Parsers {
		refPath := fmt.Sprintf("%s.parsers[%d]", path, i)
		v.requireRef(refPath, parserID, parserIDs, "parser")
		v.requireUnique(seenParsers, refPath, parserID)
	}
	seenDetections := map[string]struct{}{}
	for i, detectionID := range source.Detections {
		refPath := fmt.Sprintf("%s.detections[%d]", path, i)
		v.requireRef(refPath, detectionID, detectionIDs, "detection")
		v.requireUnique(seenDetections, refPath, detectionID)
	}
	if len(source.Samples) == 0 {
		v.issue(path+".samples", "requires at least one sample case reference")
	}
	for i, sampleID := range source.Samples {
		v.requireID(fmt.Sprintf("%s.samples[%d]", path, i), sampleID)
	}
	v.validateSchemas(path+".schemas", source.Schemas)
	v.validateVolume(path+".expected_volume", source.ExpectedVolume)
}

func (v *validator) validateParser(path string, parser ParserProfile) {
	if strings.TrimSpace(parser.DisplayName) == "" {
		v.issue(path+".display_name", "is required")
	}
	if strings.TrimSpace(parser.Version) != "" {
		v.requireSemver(path+".version", parser.Version)
	}
	if strings.TrimSpace(parser.Entrypoint) != "" && !pathPattern.MatchString(parser.Entrypoint) {
		v.issue(path+".entrypoint", "contains unsupported characters")
	}
	if len(parser.Stages) == 0 {
		v.issue(path+".stages", "requires at least one parser stage")
	}
	seenStages := map[string]struct{}{}
	for i, stage := range parser.Stages {
		stagePath := fmt.Sprintf("%s.stages[%d]", path, i)
		if strings.TrimSpace(stage.StageID) != "" {
			if v.requireID(stagePath+".stage_id", stage.StageID) {
				v.requireUnique(seenStages, stagePath+".stage_id", stage.StageID)
			}
		}
		v.requireAllowed(stagePath+".type", stage.Type, allowedStageTypes())
		if strings.TrimSpace(stage.OnError) != "" {
			v.requireAllowed(stagePath+".on_error", stage.OnError, allowedOnErrorModes())
		}
	}
}

func (v *validator) validateSample(path string, sample SampleCase, sourceIDs, parserIDs map[string]struct{}, sourceParserRefs map[string]map[string]struct{}, sampleCoverage map[string]map[string]struct{}) {
	v.requireRef(path+".source_id", sample.SourceID, sourceIDs, "source")
	v.requireRef(path+".parser_id", sample.ParserID, parserIDs, "parser")
	if parsers := sourceParserRefs[strings.TrimSpace(sample.SourceID)]; len(parsers) > 0 {
		if _, ok := parsers[strings.TrimSpace(sample.ParserID)]; !ok {
			v.issue(path+".parser_id", "is not listed by the sample source")
		}
	}
	if strings.TrimSpace(sample.InputPath) == "" {
		v.issue(path+".input_path", "is required")
	} else if !pathPattern.MatchString(sample.InputPath) {
		v.issue(path+".input_path", "contains unsupported characters")
	}
	if strings.TrimSpace(sample.GoldenPath) == "" {
		v.issue(path+".golden_path", "is required")
	} else if !pathPattern.MatchString(sample.GoldenPath) {
		v.issue(path+".golden_path", "contains unsupported characters")
	}
	sourceID := strings.TrimSpace(sample.SourceID)
	parserID := strings.TrimSpace(sample.ParserID)
	if sourceID != "" && parserID != "" {
		if sampleCoverage[sourceID] == nil {
			sampleCoverage[sourceID] = map[string]struct{}{}
		}
		sampleCoverage[sourceID][parserID] = struct{}{}
	}
}

func (v *validator) validateSampleCoverage(sourceParsers map[string][]string, sampleCoverage map[string]map[string]struct{}) {
	sourceIDs := sortedKeys(sourceParsers)
	for _, sourceID := range sourceIDs {
		parserIDs := dedupeSorted(sourceParsers[sourceID])
		for _, parserID := range parserIDs {
			if _, ok := sampleCoverage[sourceID][parserID]; !ok {
				v.issue("samples", fmt.Sprintf("missing sample coverage for source %q parser %q", sourceID, parserID))
			}
		}
	}
}

func (v *validator) validateSourceSampleRefs(sourceSamples map[string][]string, sampleSourceIDs map[string]string) {
	sourceIDs := sortedKeys(sourceSamples)
	for _, sourceID := range sourceIDs {
		for _, sampleID := range dedupeSorted(sourceSamples[sourceID]) {
			sampleSourceID, ok := sampleSourceIDs[sampleID]
			if !ok {
				v.issue("sources."+sourceID+".samples", fmt.Sprintf("references unknown sample %q", sampleID))
				continue
			}
			if sampleSourceID != sourceID {
				v.issue("sources."+sourceID+".samples", fmt.Sprintf("references sample %q owned by source %q", sampleID, sampleSourceID))
			}
		}
	}
}

func (v *validator) validateSchemas(path string, schemas SchemaBinding) {
	if !v.requireAllowed(path+".primary", schemas.Primary, allowedSchemas()) {
		return
	}
	seen := map[string]struct{}{}
	for i, alias := range schemas.ExportAliases {
		aliasPath := fmt.Sprintf("%s.export_aliases[%d]", path, i)
		if v.requireAllowed(aliasPath, alias, allowedSchemas()) {
			v.requireUnique(seen, aliasPath, alias)
		}
	}
	if strings.TrimSpace(schemas.Primary) == SchemaOCSF {
		if strings.TrimSpace(schemas.OCSF.Category) == "" {
			v.issue(path+".ocsf.category", "is required when primary schema is ocsf")
		}
		if strings.TrimSpace(schemas.OCSF.Class) == "" {
			v.issue(path+".ocsf.class", "is required when primary schema is ocsf")
		}
	}
}

func (v *validator) validateVolume(path string, volume VolumeHint) {
	if volume.EventsPerSecond < 0 {
		v.issue(path+".events_per_second", "must not be negative")
	}
	if volume.BytesPerSecond < 0 {
		v.issue(path+".bytes_per_second", "must not be negative")
	}
}

func (v *validator) requireID(path, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		v.issue(path, "is required")
		return false
	}
	if !idPattern.MatchString(value) {
		v.issue(path, "must be lowercase and contain only letters, digits, dot, underscore, or hyphen")
		return false
	}
	return true
}

func (v *validator) requireSemver(path, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		v.issue(path, "is required")
		return false
	}
	if !semverPattern.MatchString(value) {
		v.issue(path, "must be semantic version x.y.z with optional prerelease/build suffix")
		return false
	}
	return true
}

func (v *validator) requireAllowed(path, value string, allowed map[string]struct{}) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		v.issue(path, "is required")
		return false
	}
	if _, ok := allowed[value]; !ok {
		v.issue(path, fmt.Sprintf("unsupported value %q", value))
		return false
	}
	return true
}

func (v *validator) requireUnique(seen map[string]struct{}, path, value string) bool {
	key := strings.TrimSpace(value)
	if key == "" {
		return false
	}
	if _, ok := seen[key]; ok {
		v.issue(path, fmt.Sprintf("duplicate value %q", key))
		return false
	}
	seen[key] = struct{}{}
	return true
}

func (v *validator) requireRef(path, value string, refs map[string]struct{}, kind string) bool {
	if !v.requireID(path, value) {
		return false
	}
	if _, ok := refs[strings.TrimSpace(value)]; !ok {
		v.issue(path, fmt.Sprintf("references unknown %s %q", kind, value))
		return false
	}
	return true
}

func requiresApproval(risk, sensitivity string) bool {
	switch strings.TrimSpace(risk) {
	case RiskHigh, RiskCritical:
		return true
	}
	switch strings.TrimSpace(sensitivity) {
	case SensitivityHigh, SensitivityRestricted:
		return true
	}
	return false
}

func allowedRiskClasses() map[string]struct{} {
	return stringSet(RiskLow, RiskMedium, RiskHigh, RiskCritical)
}

func allowedDataSensitivity() map[string]struct{} {
	return stringSet(SensitivityLow, SensitivityModerate, SensitivityHigh, SensitivityRestricted)
}

func allowedSchemas() map[string]struct{} {
	return stringSet(SchemaOCSF, SchemaECS)
}

func allowedCollectorModes() map[string]struct{} {
	return stringSet(
		CollectorNodeFileLog,
		CollectorOTelFileLog,
		CollectorSyslog,
		CollectorWindowsEvent,
		CollectorSplunkHEC,
		CollectorKafka,
		CollectorOTLP,
		CollectorVendorAPI,
		CollectorWEF,
		CollectorArchive,
		CollectorPrometheus,
		CollectorDatabase,
		CollectorPrivateAccess,
		CollectorControlOneNode,
	)
}

func allowedStageTypes() map[string]struct{} {
	return stringSet(
		StageJSON,
		StageSyslogRFC3164,
		StageSyslogRFC5424,
		StageCEF,
		StageLEEF,
		StageRegex,
		StageGrok,
		StageKV,
		StageLogfmt,
		StageXML,
		StageWindowsEventData,
		StageTimestamp,
		StageFieldMap,
		StageRedact,
		StageDrop,
		StageEnrich,
		StageOCSFMap,
		StageECSAlias,
	)
}

func allowedOnErrorModes() map[string]struct{} {
	return stringSet(OnErrorFail, OnErrorKeepRaw, OnErrorDrop)
}

func allowedDetectionKinds() map[string]struct{} {
	return stringSet(DetectionKindSigma, DetectionKindControlOne)
}

func allowedTemporalStepOps() map[string]bool {
	return map[string]bool{
		"equals":      true,
		"contains":    true,
		"starts_with": true,
		"ends_with":   true,
		"exists":      true,
		"in":          true,
		"gt":          true,
		"gte":         true,
		"lt":          true,
		"lte":         true,
	}
}

func stringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func sortedKeys(values map[string][]string) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func dedupeSorted(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
