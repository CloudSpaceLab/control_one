package contentpacks

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/detections"
)

const defaultMaxDetectionBytes = 1 << 20

type DetectionLoadOptions struct {
	SigmaFieldMap     map[string]string
	MaxDetectionBytes int64
}

type LoadedDetection struct {
	Manifest Detection
	Rule     detections.Rule
	Path     string
}

type DetectionReplayOptions struct {
	DetectionLoadOptions
	MaxSampleBytes int64
}

type DetectionReplayReport struct {
	PackID           string                      `json:"pack_id"`
	PackVersion      string                      `json:"pack_version"`
	TotalRules       int                         `json:"total_rules"`
	TotalCases       int                         `json:"total_cases"`
	TotalEvents      int                         `json:"total_events"`
	TotalEvaluations int                         `json:"total_evaluations"`
	TotalMatches     int                         `json:"total_matches"`
	Results          []DetectionReplayCaseResult `json:"results"`
	Failures         []DetectionReplayFailure    `json:"failures,omitempty"`
}

type DetectionReplayCaseResult struct {
	CaseID      string                   `json:"case_id"`
	SourceID    string                   `json:"source_id"`
	GoldenPath  string                   `json:"golden_path"`
	EventCount  int                      `json:"event_count"`
	Evaluations int                      `json:"evaluations"`
	Matches     []DetectionReplayMatch   `json:"matches,omitempty"`
	Failures    []DetectionReplayFailure `json:"failures,omitempty"`
}

type DetectionReplayMatch struct {
	CaseID      string   `json:"case_id"`
	SourceID    string   `json:"source_id"`
	Index       int      `json:"index"`
	DetectionID string   `json:"detection_id"`
	Title       string   `json:"title,omitempty"`
	Severity    string   `json:"severity,omitempty"`
	RiskScore   int      `json:"risk_score,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type DetectionReplayFailure struct {
	CaseID      string `json:"case_id"`
	Index       int    `json:"index"`
	DetectionID string `json:"detection_id,omitempty"`
	Error       string `json:"error"`
}

func LoadManifestDetections(ctx context.Context, manifest Manifest, root fs.FS, opts DetectionLoadOptions) ([]LoadedDetection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := Validate(manifest); err != nil {
		return nil, err
	}
	if opts.MaxDetectionBytes <= 0 {
		opts.MaxDetectionBytes = defaultMaxDetectionBytes
	}
	loaded := make([]LoadedDetection, 0, len(manifest.Detections))
	for i, detection := range manifest.Detections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch strings.TrimSpace(detection.Kind) {
		case DetectionKindSigma:
			item, err := loadSigmaDetection(root, i, detection, opts)
			if err != nil {
				return nil, err
			}
			loaded = append(loaded, item)
		case DetectionKindControlOne:
			return nil, fmt.Errorf("detections[%d] %s: controlone detections are not loadable yet", i, strings.TrimSpace(detection.DetectionID))
		default:
			return nil, fmt.Errorf("detections[%d] %s: unsupported detection kind %q", i, strings.TrimSpace(detection.DetectionID), detection.Kind)
		}
	}
	return loaded, nil
}

func DefaultSigmaFieldMap() map[string]string {
	return map[string]string{
		"AccountName":       "user.name",
		"CommandLine":       "process.command_line",
		"Computer":          "host.hostname",
		"DestinationIp":     "destination.ip",
		"DestinationIP":     "destination.ip",
		"DestinationPort":   "destination.port",
		"EventID":           "event.code",
		"EventId":           "event.code",
		"Hostname":          "host.hostname",
		"Image":             "process.executable",
		"ParentCommandLine": "process.parent.command_line",
		"ParentImage":       "process.parent.executable",
		"ProcessId":         "process.pid",
		"Provider_Name":     "event.provider",
		"SourceIp":          "source.ip",
		"SourceIP":          "source.ip",
		"SourcePort":        "source.port",
		"TargetUserName":    "user.name",
		"User":              "user.name",
		"UserName":          "user.name",
	}
}

func ReplayManifestDetections(ctx context.Context, manifest Manifest, root fs.FS, opts DetectionReplayOptions) (DetectionReplayReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if root == nil {
		return DetectionReplayReport{}, fmt.Errorf("detection replay filesystem is nil")
	}
	if opts.MaxSampleBytes <= 0 {
		opts.MaxSampleBytes = defaultMaxSampleBytes
	}
	loaded, err := LoadManifestDetections(ctx, manifest, root, opts.DetectionLoadOptions)
	if err != nil {
		return DetectionReplayReport{}, err
	}
	report := DetectionReplayReport{
		PackID:      strings.TrimSpace(manifest.PackID),
		PackVersion: strings.TrimSpace(manifest.PackVersion),
		TotalRules:  len(loaded),
		TotalCases:  len(manifest.Samples),
		Results:     make([]DetectionReplayCaseResult, 0, len(manifest.Samples)),
	}
	rulesBySource := detectionRulesBySource(manifest, loaded)
	for _, sample := range manifest.Samples {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		result := replayDetectionsForSample(sample, rulesBySource[strings.TrimSpace(sample.SourceID)], root, opts.MaxSampleBytes)
		report.TotalEvents += result.EventCount
		report.TotalEvaluations += result.Evaluations
		report.TotalMatches += len(result.Matches)
		report.Failures = append(report.Failures, result.Failures...)
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func (r DetectionReplayReport) Passed() bool {
	return len(r.Failures) == 0
}

func loadSigmaDetection(root fs.FS, index int, detection Detection, opts DetectionLoadOptions) (LoadedDetection, error) {
	data, cleanPath, err := readDetectionFile(root, detection.Path, opts.MaxDetectionBytes)
	if err != nil {
		return LoadedDetection{}, fmt.Errorf("detections[%d] %s: %w", index, strings.TrimSpace(detection.DetectionID), err)
	}
	rule, err := detections.ImportSigma(data, detections.SigmaImportOptions{
		FieldMap:             opts.SigmaFieldMap,
		AllowMissingMetadata: true,
	})
	if err != nil {
		return LoadedDetection{}, fmt.Errorf("detections[%d] %s: import sigma %s: %w", index, strings.TrimSpace(detection.DetectionID), cleanPath, err)
	}
	rule = applyManifestDetectionMetadata(rule, detection)
	if err := rule.Validate(); err != nil {
		return LoadedDetection{}, fmt.Errorf("detections[%d] %s: loaded rule is invalid: %w", index, strings.TrimSpace(detection.DetectionID), err)
	}
	return LoadedDetection{
		Manifest: cloneDetectionMetadata(detection),
		Rule:     rule,
		Path:     cleanPath,
	}, nil
}

func TemporalRuleForDetection(detection Detection, rule detections.Rule) detections.TemporalRule {
	return detections.TemporalRule{
		Rule:     rule,
		Temporal: TemporalForDetection(detection),
	}
}

func TemporalForDetection(detection Detection) detections.Temporal {
	if detection.Temporal == nil {
		return detections.Temporal{}
	}
	return detections.Temporal{
		Kind:               strings.TrimSpace(detection.Temporal.Kind),
		WindowSeconds:      detection.Temporal.WindowSeconds,
		Threshold:          detection.Temporal.Threshold,
		GroupBy:            append([]string(nil), detection.Temporal.GroupBy...),
		SuppressForSeconds: detection.Temporal.SuppressForSeconds,
		Sequence:           temporalSequenceForDetection(detection.Temporal.Sequence),
		Join:               temporalSequenceForDetection(detection.Temporal.Join),
	}
}

func temporalSequenceForDetection(steps []DetectionTemporalStep) []detections.TemporalStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]detections.TemporalStep, len(steps))
	for i, step := range steps {
		out[i] = detections.TemporalStep{
			Field:         strings.TrimSpace(step.Field),
			Op:            strings.TrimSpace(step.Op),
			Values:        append([]any(nil), step.Values...),
			CaseSensitive: step.CaseSensitive,
		}
	}
	return out
}

func detectionRulesBySource(manifest Manifest, loaded []LoadedDetection) map[string][]LoadedDetection {
	loadedByID := make(map[string]LoadedDetection, len(loaded))
	for _, item := range loaded {
		loadedByID[strings.TrimSpace(item.Manifest.DetectionID)] = item
	}
	out := map[string][]LoadedDetection{}
	for _, source := range manifest.Sources {
		sourceID := strings.TrimSpace(source.SourceID)
		for _, detectionID := range source.Detections {
			if item, ok := loadedByID[strings.TrimSpace(detectionID)]; ok {
				out[sourceID] = append(out[sourceID], item)
			}
		}
	}
	return out
}

func replayDetectionsForSample(sample SampleCase, rules []LoadedDetection, root fs.FS, maxBytes int64) DetectionReplayCaseResult {
	result := DetectionReplayCaseResult{
		CaseID:     strings.TrimSpace(sample.CaseID),
		SourceID:   strings.TrimSpace(sample.SourceID),
		GoldenPath: strings.TrimSpace(sample.GoldenPath),
	}
	goldens, err := readSampleGoldens(root, result.GoldenPath, maxBytes)
	if err != nil {
		result.Failures = append(result.Failures, DetectionReplayFailure{CaseID: result.CaseID, Index: -1, Error: err.Error()})
		return result
	}
	result.EventCount = len(goldens)
	evaluator := detections.NewStatefulEvaluator()
	for i, golden := range goldens {
		if strings.TrimSpace(golden.Status) == ParserStatusFailed || strings.TrimSpace(golden.Status) == ParserStatusDropped || (golden.Dropped != nil && *golden.Dropped) {
			continue
		}
		event := detectionReplayEvent(golden, i)
		for _, item := range rules {
			match := evaluateLoadedDetection(item, event, evaluator)
			result.Evaluations++
			if !match.Matched {
				continue
			}
			result.Matches = append(result.Matches, DetectionReplayMatch{
				CaseID:      result.CaseID,
				SourceID:    result.SourceID,
				Index:       i,
				DetectionID: match.RuleID,
				Title:       match.Title,
				Severity:    match.Severity,
				RiskScore:   match.RiskScore,
				Tags:        append([]string(nil), match.Tags...),
			})
		}
	}
	return result
}

func evaluateLoadedDetection(item LoadedDetection, event detections.Event, evaluator *detections.StatefulEvaluator) detections.Match {
	temporalRule := TemporalRuleForDetection(item.Manifest, item.Rule)
	if temporalRule.Temporal.Enabled() {
		return evaluator.Evaluate(temporalRule, event)
	}
	return item.Rule.Evaluate(event)
}

func detectionReplayEvent(golden sampleGoldenRecord, index int) detections.Event {
	raw := ""
	if message, ok := golden.Fields["message"]; ok {
		raw = fmt.Sprint(message)
	}
	ts := time.Unix(int64(index), 0).UTC()
	if strings.TrimSpace(golden.Timestamp) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(golden.Timestamp)); err == nil {
			ts = parsed
		}
	}
	return detections.Event{
		Raw:       raw,
		Fields:    golden.Fields,
		Timestamp: ts,
	}
}

func readDetectionFile(root fs.FS, detectionPath string, maxBytes int64) ([]byte, string, error) {
	cleanPath, err := cleanPackPath(detectionPath)
	if err != nil {
		return nil, "", fmt.Errorf("detection path: %w", err)
	}
	data, err := fs.ReadFile(root, cleanPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", cleanPath, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("%s is %d bytes, exceeds detection limit %d", cleanPath, len(data), maxBytes)
	}
	return data, cleanPath, nil
}

func applyManifestDetectionMetadata(rule detections.Rule, detection Detection) detections.Rule {
	if id := strings.TrimSpace(detection.DetectionID); id != "" {
		rule.ID = id
	}
	if title := strings.TrimSpace(detection.Title); title != "" {
		rule.Title = title
	}
	if severity := strings.TrimSpace(detection.Severity); severity != "" {
		rule.Severity = severity
	}
	if detection.RiskScore > 0 {
		rule.RiskScore = detection.RiskScore
	}
	rule.RiskScore = detections.RiskScoreForSeverity(rule.RiskScore, rule.Severity)
	rule.Tags = mergeDetectionTags(rule.Tags, detection.Tags)
	return rule
}

func mergeDetectionTags(ruleTags, manifestTags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ruleTags)+len(manifestTags))
	for _, tags := range [][]string{ruleTags, manifestTags} {
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			out = append(out, tag)
		}
	}
	return out
}

func cloneDetectionMetadata(detection Detection) Detection {
	detection.Tags = append([]string(nil), detection.Tags...)
	if detection.Temporal != nil {
		temporal := *detection.Temporal
		temporal.GroupBy = append([]string(nil), temporal.GroupBy...)
		temporal.Sequence = cloneDetectionTemporalSteps(temporal.Sequence)
		temporal.Join = cloneDetectionTemporalSteps(temporal.Join)
		detection.Temporal = &temporal
	}
	return detection
}
