package contentpacks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/securityschema"
)

const defaultMaxSampleBytes = 10 << 20

type SampleReplayOptions struct {
	RuntimeRegistry         *ParserRuntimeRegistry
	MaxSampleBytes          int64
	AllowExtraFields        bool
	DisableSchemaValidation bool
}

type SampleReplayReport struct {
	PackID      string                   `json:"pack_id"`
	PackVersion string                   `json:"pack_version"`
	TotalCases  int                      `json:"total_cases"`
	PassedCases int                      `json:"passed_cases"`
	FailedCases int                      `json:"failed_cases"`
	TotalEvents int                      `json:"total_events"`
	Results     []SampleReplayCaseResult `json:"results"`
	Failures    []SampleReplayFailure    `json:"failures,omitempty"`
}

type SampleReplayCaseResult struct {
	CaseID     string                `json:"case_id"`
	SourceID   string                `json:"source_id"`
	ParserID   string                `json:"parser_id"`
	InputPath  string                `json:"input_path"`
	GoldenPath string                `json:"golden_path"`
	EventCount int                   `json:"event_count"`
	Passed     bool                  `json:"passed"`
	Failures   []SampleReplayFailure `json:"failures,omitempty"`
}

type SampleReplayFailure struct {
	CaseID string `json:"case_id"`
	Index  int    `json:"index"`
	Field  string `json:"field,omitempty"`
	Want   string `json:"want,omitempty"`
	Got    string `json:"got,omitempty"`
	Error  string `json:"error,omitempty"`
}

type sampleGoldenRecord struct {
	ParserID    string             `json:"parser_id,omitempty"`
	Status      string             `json:"status"`
	RawRef      string             `json:"raw_ref,omitempty"`
	Timestamp   string             `json:"timestamp,omitempty"`
	Fields      map[string]any     `json:"fields,omitempty"`
	Labels      map[string]string  `json:"labels,omitempty"`
	Dropped     *bool              `json:"dropped,omitempty"`
	StageErrors []ParserStageError `json:"stage_errors,omitempty"`
	Event       *sampleGoldenEvent `json:"event,omitempty"`
}

type sampleGoldenEvent struct {
	RawRef    string            `json:"raw_ref,omitempty"`
	Timestamp string            `json:"timestamp,omitempty"`
	Fields    map[string]any    `json:"fields,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Dropped   *bool             `json:"dropped,omitempty"`
}

func ReplayManifestSamples(ctx context.Context, manifest Manifest, root fs.FS, opts SampleReplayOptions) (SampleReplayReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if root == nil {
		return SampleReplayReport{}, fmt.Errorf("sample filesystem is nil")
	}
	if err := Validate(manifest); err != nil {
		return SampleReplayReport{}, err
	}
	if opts.RuntimeRegistry == nil {
		opts.RuntimeRegistry = DefaultParserRuntimeRegistry()
	}
	if opts.MaxSampleBytes <= 0 {
		opts.MaxSampleBytes = defaultMaxSampleBytes
	}
	report := SampleReplayReport{
		PackID:      strings.TrimSpace(manifest.PackID),
		PackVersion: strings.TrimSpace(manifest.PackVersion),
		TotalCases:  len(manifest.Samples),
		Results:     make([]SampleReplayCaseResult, 0, len(manifest.Samples)),
	}
	parsers := parserProfileMap(manifest)
	for _, sample := range manifest.Samples {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		result, err := replaySampleCase(ctx, sample, parsers, root, opts)
		if err != nil {
			result = SampleReplayCaseResult{
				CaseID:     strings.TrimSpace(sample.CaseID),
				SourceID:   strings.TrimSpace(sample.SourceID),
				ParserID:   strings.TrimSpace(sample.ParserID),
				InputPath:  strings.TrimSpace(sample.InputPath),
				GoldenPath: strings.TrimSpace(sample.GoldenPath),
				Passed:     false,
				Failures:   []SampleReplayFailure{{CaseID: strings.TrimSpace(sample.CaseID), Index: -1, Error: err.Error()}},
			}
		}
		report.TotalEvents += result.EventCount
		if result.Passed {
			report.PassedCases++
		} else {
			report.FailedCases++
			report.Failures = append(report.Failures, result.Failures...)
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func (r SampleReplayReport) Passed() bool {
	return r.FailedCases == 0 && len(r.Failures) == 0
}

func replaySampleCase(ctx context.Context, sample SampleCase, parsers map[string]ParserProfile, root fs.FS, opts SampleReplayOptions) (SampleReplayCaseResult, error) {
	result := SampleReplayCaseResult{
		CaseID:     strings.TrimSpace(sample.CaseID),
		SourceID:   strings.TrimSpace(sample.SourceID),
		ParserID:   strings.TrimSpace(sample.ParserID),
		InputPath:  strings.TrimSpace(sample.InputPath),
		GoldenPath: strings.TrimSpace(sample.GoldenPath),
	}
	profile, ok := parsers[result.ParserID]
	if !ok {
		return result, fmt.Errorf("parser %q not found", result.ParserID)
	}
	compiled, err := opts.RuntimeRegistry.Compile(profile)
	if err != nil {
		return result, err
	}
	inputs, err := readSampleInputs(root, result.InputPath, opts.MaxSampleBytes)
	if err != nil {
		return result, err
	}
	goldens, err := readSampleGoldens(root, result.GoldenPath, opts.MaxSampleBytes)
	if err != nil {
		return result, err
	}
	result.EventCount = len(inputs)
	if len(inputs) != len(goldens) {
		result.Failures = append(result.Failures, SampleReplayFailure{
			CaseID: result.CaseID,
			Index:  -1,
			Field:  "record_count",
			Want:   fmt.Sprint(len(goldens)),
			Got:    fmt.Sprint(len(inputs)),
			Error:  "input/golden record count mismatch",
		})
		result.Passed = false
		return result, nil
	}
	for i, input := range inputs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		output, parseErr := compiled.Parse(input)
		failures := compareGoldenOutput(result.CaseID, i, sample.ParserID, goldens[i], output, parseErr, opts.AllowExtraFields)
		if !opts.DisableSchemaValidation {
			failures = append(failures, validateSampleSchema(result.CaseID, i, output)...)
		}
		result.Failures = append(result.Failures, failures...)
	}
	result.Passed = len(result.Failures) == 0
	return result, nil
}

func readSampleInputs(root fs.FS, samplePath string, maxBytes int64) ([]ParserInput, error) {
	data, cleanPath, err := readSampleFile(root, samplePath, maxBytes)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(path.Ext(cleanPath))
	switch ext {
	case ".jsonl", ".ndjson":
		lines := splitNonEmptyLines(data)
		inputs := make([]ParserInput, 0, len(lines))
		for i, line := range lines {
			input, err := parseSampleInputJSON(line)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, i+1, err)
			}
			inputs = append(inputs, input)
		}
		return inputs, nil
	case ".json":
		return parseSampleInputJSONDocument(data)
	case ".xml":
		raw := strings.TrimSpace(string(data))
		if raw == "" {
			return nil, fmt.Errorf("%s is empty", cleanPath)
		}
		return []ParserInput{{Raw: raw}}, nil
	default:
		lines := splitNonEmptyLines(data)
		inputs := make([]ParserInput, 0, len(lines))
		for _, line := range lines {
			inputs = append(inputs, ParserInput{Raw: string(bytes.TrimRight(line, "\r"))})
		}
		return inputs, nil
	}
}

func readSampleGoldens(root fs.FS, goldenPath string, maxBytes int64) ([]sampleGoldenRecord, error) {
	data, cleanPath, err := readSampleFile(root, goldenPath, maxBytes)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(path.Ext(cleanPath))
	switch ext {
	case ".jsonl", ".ndjson":
		lines := splitNonEmptyLines(data)
		goldens := make([]sampleGoldenRecord, 0, len(lines))
		for i, line := range lines {
			golden, err := parseGoldenRecord(line)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", cleanPath, i+1, err)
			}
			goldens = append(goldens, golden)
		}
		return goldens, nil
	case ".json":
		return parseGoldenDocument(data, cleanPath)
	default:
		return nil, fmt.Errorf("%s must be .json, .jsonl, or .ndjson", cleanPath)
	}
}

func readSampleFile(root fs.FS, samplePath string, maxBytes int64) ([]byte, string, error) {
	cleanPath, err := cleanPackPath(samplePath)
	if err != nil {
		return nil, "", err
	}
	data, err := fs.ReadFile(root, cleanPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", cleanPath, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("%s is %d bytes, exceeds sample limit %d", cleanPath, len(data), maxBytes)
	}
	return data, cleanPath, nil
}

func cleanPackPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("sample path is required")
	}
	if strings.Contains(value, "\x00") {
		return "", fmt.Errorf("sample path contains NUL byte")
	}
	value = strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("sample path %q must be relative", value)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", fmt.Errorf("sample path %q escapes content pack root", value)
		}
	}
	clean := path.Clean(value)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("sample path %q escapes content pack root", value)
	}
	return clean, nil
}

func splitNonEmptyLines(data []byte) [][]byte {
	rawLines := bytes.Split(data, []byte{'\n'})
	out := make([][]byte, 0, len(rawLines))
	for _, line := range rawLines {
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

func parseSampleInputJSONDocument(data []byte) ([]ParserInput, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("input JSON is empty")
	}
	if trimmed[0] == '[' {
		var raw []json.RawMessage
		if err := decodeJSON(trimmed, &raw); err != nil {
			return nil, err
		}
		out := make([]ParserInput, 0, len(raw))
		for i, record := range raw {
			input, err := parseSampleInputJSON(record)
			if err != nil {
				return nil, fmt.Errorf("record %d: %w", i, err)
			}
			out = append(out, input)
		}
		return out, nil
	}
	input, err := parseSampleInputJSON(trimmed)
	if err != nil {
		return nil, err
	}
	return []ParserInput{input}, nil
}

func parseSampleInputJSON(data []byte) (ParserInput, error) {
	var rawString string
	if err := decodeJSON(data, &rawString); err == nil {
		return ParserInput{Raw: rawString}, nil
	}
	var input ParserInput
	if err := decodeJSON(data, &input); err != nil {
		return ParserInput{}, err
	}
	if input.Raw == "" && input.RawRef == "" && input.Timestamp.IsZero() && len(input.Fields) == 0 && len(input.Labels) == 0 {
		return ParserInput{}, fmt.Errorf("input record is empty")
	}
	return input, nil
}

func parseGoldenDocument(data []byte, cleanPath string) ([]sampleGoldenRecord, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%s is empty", cleanPath)
	}
	if trimmed[0] == '[' {
		var raw []json.RawMessage
		if err := decodeJSON(trimmed, &raw); err != nil {
			return nil, err
		}
		out := make([]sampleGoldenRecord, 0, len(raw))
		for i, record := range raw {
			golden, err := parseGoldenRecord(record)
			if err != nil {
				return nil, fmt.Errorf("%s:record %d: %w", cleanPath, i, err)
			}
			out = append(out, golden)
		}
		return out, nil
	}
	var envelope struct {
		Records []json.RawMessage `json:"records"`
		Outputs []json.RawMessage `json:"outputs"`
	}
	if err := decodeJSON(trimmed, &envelope); err == nil {
		rawRecords := envelope.Records
		if len(rawRecords) == 0 {
			rawRecords = envelope.Outputs
		}
		if len(rawRecords) > 0 {
			out := make([]sampleGoldenRecord, 0, len(rawRecords))
			for i, record := range rawRecords {
				golden, err := parseGoldenRecord(record)
				if err != nil {
					return nil, fmt.Errorf("%s:record %d: %w", cleanPath, i, err)
				}
				out = append(out, golden)
			}
			return out, nil
		}
	}
	golden, err := parseGoldenRecord(trimmed)
	if err != nil {
		return nil, err
	}
	return []sampleGoldenRecord{golden}, nil
}

func parseGoldenRecord(data []byte) (sampleGoldenRecord, error) {
	var golden sampleGoldenRecord
	if err := decodeJSON(data, &golden); err != nil {
		return sampleGoldenRecord{}, err
	}
	if golden.Event != nil {
		if golden.RawRef == "" {
			golden.RawRef = golden.Event.RawRef
		}
		if golden.Timestamp == "" {
			golden.Timestamp = golden.Event.Timestamp
		}
		if golden.Fields == nil {
			golden.Fields = golden.Event.Fields
		}
		if golden.Labels == nil {
			golden.Labels = golden.Event.Labels
		}
		if golden.Dropped == nil {
			golden.Dropped = golden.Event.Dropped
		}
	}
	if strings.TrimSpace(golden.Status) == "" {
		return sampleGoldenRecord{}, fmt.Errorf("golden status is required")
	}
	return golden, nil
}

func compareGoldenOutput(caseID string, index int, parserID string, golden sampleGoldenRecord, output ParserOutput, parseErr error, allowExtraFields bool) []SampleReplayFailure {
	failures := []SampleReplayFailure{}
	if parseErr != nil && strings.TrimSpace(golden.Status) != ParserStatusFailed {
		failures = append(failures, SampleReplayFailure{CaseID: caseID, Index: index, Field: "parse_error", Error: parseErr.Error()})
	}
	if golden.ParserID != "" && golden.ParserID != output.ParserID {
		failures = append(failures, diffFailure(caseID, index, "parser_id", golden.ParserID, output.ParserID))
	} else if output.ParserID != strings.TrimSpace(parserID) {
		failures = append(failures, diffFailure(caseID, index, "parser_id", strings.TrimSpace(parserID), output.ParserID))
	}
	if output.Status != strings.TrimSpace(golden.Status) {
		failures = append(failures, diffFailure(caseID, index, "status", golden.Status, output.Status))
	}
	if golden.RawRef != "" && output.Event.RawRef != golden.RawRef {
		failures = append(failures, diffFailure(caseID, index, "raw_ref", golden.RawRef, output.Event.RawRef))
	}
	if golden.Timestamp != "" {
		want, err := time.Parse(time.RFC3339Nano, golden.Timestamp)
		if err != nil {
			failures = append(failures, SampleReplayFailure{CaseID: caseID, Index: index, Field: "timestamp", Error: err.Error()})
		} else if !output.Event.Timestamp.Equal(want) {
			failures = append(failures, diffFailure(caseID, index, "timestamp", want.Format(time.RFC3339Nano), output.Event.Timestamp.Format(time.RFC3339Nano)))
		}
	}
	if golden.Fields != nil {
		if allowExtraFields {
			failures = append(failures, compareFieldSubset(caseID, index, "fields", golden.Fields, output.Event.Fields)...)
		} else if !jsonEqual(golden.Fields, output.Event.Fields) {
			failures = append(failures, diffFailure(caseID, index, "fields", canonicalJSONString(golden.Fields), canonicalJSONString(output.Event.Fields)))
		}
	}
	if golden.Labels != nil && !reflect.DeepEqual(golden.Labels, output.Event.Labels) {
		failures = append(failures, diffFailure(caseID, index, "labels", canonicalJSONString(golden.Labels), canonicalJSONString(output.Event.Labels)))
	}
	if golden.Dropped != nil && output.Event.Dropped != *golden.Dropped {
		failures = append(failures, diffFailure(caseID, index, "dropped", fmt.Sprint(*golden.Dropped), fmt.Sprint(output.Event.Dropped)))
	}
	if golden.StageErrors != nil && !reflect.DeepEqual(golden.StageErrors, output.StageErrors) {
		failures = append(failures, diffFailure(caseID, index, "stage_errors", canonicalJSONString(golden.StageErrors), canonicalJSONString(output.StageErrors)))
	}
	return failures
}

func validateSampleSchema(caseID string, index int, output ParserOutput) []SampleReplayFailure {
	if output.Status == ParserStatusFailed || output.Event.Dropped || len(output.Event.Fields) == 0 {
		return nil
	}
	violations := securityschema.Validate(output.Event.Fields)
	if len(violations) == 0 {
		return nil
	}
	failures := make([]SampleReplayFailure, 0, len(violations))
	for _, violation := range violations {
		failures = append(failures, SampleReplayFailure{
			CaseID: caseID,
			Index:  index,
			Field:  "schema." + violation.Field,
			Error:  violation.Message,
		})
	}
	return failures
}

func compareFieldSubset(caseID string, index int, prefix string, want, got map[string]any) []SampleReplayFailure {
	failures := []SampleReplayFailure{}
	keys := make([]string, 0, len(want))
	for key := range want {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		wantValue := want[key]
		gotValue, ok := got[key]
		if !ok {
			failures = append(failures, SampleReplayFailure{CaseID: caseID, Index: index, Field: prefix + "." + key, Error: "missing field"})
			continue
		}
		wantMap, wantIsMap := wantValue.(map[string]any)
		gotMap, gotIsMap := gotValue.(map[string]any)
		if wantIsMap && gotIsMap {
			failures = append(failures, compareFieldSubset(caseID, index, prefix+"."+key, wantMap, gotMap)...)
			continue
		}
		if !jsonEqual(wantValue, gotValue) {
			failures = append(failures, diffFailure(caseID, index, prefix+"."+key, canonicalJSONString(wantValue), canonicalJSONString(gotValue)))
		}
	}
	return failures
}

func diffFailure(caseID string, index int, field, want, got string) SampleReplayFailure {
	return SampleReplayFailure{CaseID: caseID, Index: index, Field: field, Want: want, Got: got}
}

func parserProfileMap(manifest Manifest) map[string]ParserProfile {
	out := make(map[string]ParserProfile, len(manifest.Parsers))
	for _, parser := range manifest.Parsers {
		out[strings.TrimSpace(parser.ParserID)] = cloneParserProfile(parser)
	}
	return out
}

func decodeJSON(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON contains multiple values")
		}
		return err
	}
	return nil
}

func jsonEqual(a, b any) bool {
	return canonicalJSONString(a) == canonicalJSONString(b)
}

func canonicalJSONString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}
