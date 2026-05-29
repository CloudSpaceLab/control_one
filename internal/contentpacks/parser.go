package contentpacks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	ParserStatusParsed      = "parsed"
	ParserStatusFailed      = "failed"
	ParserStatusPartial     = "partial"
	ParserStatusDropped     = "dropped"
	ParserStatusUnsupported = "unsupported"
)

type ParserInput struct {
	Raw       string            `json:"raw,omitempty"`
	RawRef    string            `json:"raw_ref,omitempty"`
	Timestamp time.Time         `json:"timestamp,omitempty"`
	Fields    map[string]any    `json:"fields,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type ParserEvent struct {
	Raw       string            `json:"raw,omitempty"`
	RawRef    string            `json:"raw_ref,omitempty"`
	Timestamp time.Time         `json:"timestamp,omitempty"`
	Fields    map[string]any    `json:"fields,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Dropped   bool              `json:"dropped,omitempty"`
}

type ParserOutput struct {
	ParserID    string             `json:"parser_id"`
	Status      string             `json:"status"`
	Event       ParserEvent        `json:"event"`
	StageErrors []ParserStageError `json:"stage_errors,omitempty"`
}

type ParserStageError struct {
	StageID string `json:"stage_id,omitempty"`
	Type    string `json:"type"`
	Error   string `json:"error"`
}

type CompiledParser struct {
	Profile ParserProfile
	stages  []compiledStage
}

type compiledStage struct {
	definition ParserStage
	runtime    CompiledParserStage
}

type ParserRuntimeRegistry struct {
	runtimes map[string]ParserStageRuntime
}

func NewParserRuntimeRegistry(runtimes ...ParserStageRuntime) *ParserRuntimeRegistry {
	registry := &ParserRuntimeRegistry{runtimes: map[string]ParserStageRuntime{}}
	for _, runtime := range runtimes {
		registry.Register(runtime)
	}
	return registry
}

func DefaultParserRuntimeRegistry() *ParserRuntimeRegistry {
	return NewParserRuntimeRegistry(
		jsonStageRuntime{},
		syslogStageRuntime{stageType: StageSyslogRFC3164},
		syslogStageRuntime{stageType: StageSyslogRFC5424},
		cefStageRuntime{},
		leefStageRuntime{},
		regexStageRuntime{},
		grokStageRuntime{},
		kvStageRuntime{stageType: StageKV},
		kvStageRuntime{stageType: StageLogfmt},
		xmlStageRuntime{},
		windowsEventDataStageRuntime{},
		timestampStageRuntime{},
		fieldMapStageRuntime{stageType: StageFieldMap},
		fieldMapStageRuntime{stageType: StageOCSFMap},
		fieldMapStageRuntime{stageType: StageECSAlias},
		fieldMapStageRuntime{stageType: StageEnrich},
		redactStageRuntime{},
		dropStageRuntime{},
	)
}

func (r *ParserRuntimeRegistry) Register(runtime ParserStageRuntime) {
	if r == nil || runtime == nil {
		return
	}
	stageType := strings.TrimSpace(runtime.Type())
	if stageType == "" {
		return
	}
	if r.runtimes == nil {
		r.runtimes = map[string]ParserStageRuntime{}
	}
	r.runtimes[stageType] = runtime
}

func (r *ParserRuntimeRegistry) Compile(profile ParserProfile) (*CompiledParser, error) {
	if r == nil {
		return nil, fmt.Errorf("parser runtime registry is nil")
	}
	if strings.TrimSpace(profile.ParserID) == "" {
		return nil, fmt.Errorf("parser_id is required")
	}
	if len(profile.Stages) == 0 {
		return nil, fmt.Errorf("parser %s has no stages", profile.ParserID)
	}
	compiled := &CompiledParser{Profile: cloneParserProfile(profile)}
	for _, stage := range profile.Stages {
		stageType := strings.TrimSpace(stage.Type)
		runtime := r.runtimes[stageType]
		if runtime == nil {
			return nil, fmt.Errorf("parser %s stage %s uses unsupported runtime %q", profile.ParserID, stageName(stage), stageType)
		}
		stageRuntime, err := runtime.Compile(stage)
		if err != nil {
			return nil, fmt.Errorf("compile parser %s stage %s: %w", profile.ParserID, stageName(stage), err)
		}
		compiled.stages = append(compiled.stages, compiledStage{
			definition: stage,
			runtime:    stageRuntime,
		})
	}
	return compiled, nil
}

func CompileResolvedSource(source ResolvedSource, registry *ParserRuntimeRegistry) ([]*CompiledParser, error) {
	if registry == nil {
		registry = DefaultParserRuntimeRegistry()
	}
	out := make([]*CompiledParser, 0, len(source.Parsers))
	for _, parser := range source.Parsers {
		compiled, err := registry.Compile(parser)
		if err != nil {
			return nil, err
		}
		out = append(out, compiled)
	}
	return out, nil
}

func (p *CompiledParser) Parse(input ParserInput) (ParserOutput, error) {
	if p == nil {
		return ParserOutput{Status: ParserStatusUnsupported}, fmt.Errorf("compiled parser is nil")
	}
	event := ParserEvent{
		Raw:       input.Raw,
		RawRef:    input.RawRef,
		Timestamp: input.Timestamp,
		Fields:    cloneAnyMap(input.Fields),
		Labels:    cloneStringMap(input.Labels),
	}
	if event.Fields == nil {
		event.Fields = map[string]any{}
	}
	output := ParserOutput{
		ParserID: strings.TrimSpace(p.Profile.ParserID),
		Status:   ParserStatusParsed,
		Event:    event,
	}
	for _, stage := range p.stages {
		if err := stage.runtime.Apply(&output.Event); err != nil {
			stageErr := ParserStageError{StageID: strings.TrimSpace(stage.definition.StageID), Type: strings.TrimSpace(stage.definition.Type), Error: err.Error()}
			switch strings.TrimSpace(stage.definition.OnError) {
			case OnErrorKeepRaw:
				output.StageErrors = append(output.StageErrors, stageErr)
				output.Status = ParserStatusPartial
				continue
			case OnErrorDrop:
				output.StageErrors = append(output.StageErrors, stageErr)
				output.Event.Dropped = true
				output.Status = ParserStatusDropped
				return output, nil
			default:
				output.StageErrors = append(output.StageErrors, stageErr)
				output.Status = ParserStatusFailed
				return output, err
			}
		}
		if output.Event.Dropped {
			output.Status = ParserStatusDropped
			return output, nil
		}
	}
	return output, nil
}

type jsonStageRuntime struct{}

func (jsonStageRuntime) Type() string { return StageJSON }

func (jsonStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	sourceField := stringConfig(stage.Config, "source_field")
	targetField := stringConfig(stage.Config, "target_field")
	return jsonCompiledStage{sourceField: sourceField, targetField: targetField}, nil
}

type jsonCompiledStage struct {
	sourceField string
	targetField string
}

func (s jsonCompiledStage) Type() string { return StageJSON }

func (s jsonCompiledStage) Apply(event *ParserEvent) error {
	raw := event.Raw
	if s.sourceField != "" {
		value, ok := getField(event.Fields, s.sourceField)
		if !ok {
			return fmt.Errorf("source field %q not found", s.sourceField)
		}
		raw = fmt.Sprint(value)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty JSON input")
	}
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return err
	}
	if s.targetField != "" {
		setField(event.Fields, s.targetField, decoded)
		return nil
	}
	obj, ok := decoded.(map[string]any)
	if !ok {
		return fmt.Errorf("JSON input is %T, want object when target_field is empty", decoded)
	}
	for key, value := range obj {
		event.Fields[key] = value
	}
	return nil
}

type regexStageRuntime struct{}

func (regexStageRuntime) Type() string { return StageRegex }

func (regexStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	pattern := stringConfig(stage.Config, "pattern")
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return regexCompiledStage{
		pattern:      compiled,
		sourceField:  stringConfig(stage.Config, "source_field"),
		targetPrefix: stringConfig(stage.Config, "target_prefix"),
	}, nil
}

type regexCompiledStage struct {
	pattern      *regexp.Regexp
	sourceField  string
	targetPrefix string
}

func (s regexCompiledStage) Type() string { return StageRegex }

func (s regexCompiledStage) Apply(event *ParserEvent) error {
	input := event.Raw
	if s.sourceField != "" {
		value, ok := getField(event.Fields, s.sourceField)
		if !ok {
			return fmt.Errorf("source field %q not found", s.sourceField)
		}
		input = fmt.Sprint(value)
	}
	matches := s.pattern.FindStringSubmatch(input)
	if len(matches) == 0 {
		return fmt.Errorf("pattern did not match")
	}
	names := s.pattern.SubexpNames()
	for i := 1; i < len(matches) && i < len(names); i++ {
		name := strings.TrimSpace(names[i])
		if name == "" {
			continue
		}
		setField(event.Fields, s.targetPrefix+name, matches[i])
	}
	return nil
}

type fieldMapStageRuntime struct {
	stageType string
}

func (r fieldMapStageRuntime) Type() string { return r.stageType }

func (r fieldMapStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	return fieldMapCompiledStage{
		stageType: r.stageType,
		mappings:  stringMapConfig(stage.Config, "mappings"),
		set:       anyMapConfig(stage.Config, "set"),
	}, nil
}

type fieldMapCompiledStage struct {
	stageType string
	mappings  map[string]string
	set       map[string]any
}

func (s fieldMapCompiledStage) Type() string { return s.stageType }

func (s fieldMapCompiledStage) Apply(event *ParserEvent) error {
	for target, source := range s.mappings {
		value, ok := getField(event.Fields, source)
		if !ok {
			continue
		}
		setField(event.Fields, target, value)
	}
	for target, value := range s.set {
		setField(event.Fields, target, value)
	}
	return nil
}

type redactStageRuntime struct{}

func (redactStageRuntime) Type() string { return StageRedact }

func (redactStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	fields := stringSliceConfig(stage.Config, "fields")
	if len(fields) == 0 {
		return nil, fmt.Errorf("fields is required")
	}
	replacement := stringConfig(stage.Config, "replacement")
	if replacement == "" {
		replacement = "[redacted]"
	}
	return redactCompiledStage{fields: fields, replacement: replacement}, nil
}

type redactCompiledStage struct {
	fields      []string
	replacement string
}

func (s redactCompiledStage) Type() string { return StageRedact }

func (s redactCompiledStage) Apply(event *ParserEvent) error {
	for _, field := range s.fields {
		if _, ok := getField(event.Fields, field); ok {
			setField(event.Fields, field, s.replacement)
		}
	}
	return nil
}

type dropStageRuntime struct{}

func (dropStageRuntime) Type() string { return StageDrop }

func (dropStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	field := stringConfig(stage.Config, "when_field")
	if field == "" {
		return nil, fmt.Errorf("when_field is required")
	}
	return dropCompiledStage{field: field, equals: stage.Config["equals"]}, nil
}

type dropCompiledStage struct {
	field  string
	equals any
}

func (s dropCompiledStage) Type() string { return StageDrop }

func (s dropCompiledStage) Apply(event *ParserEvent) error {
	value, ok := getField(event.Fields, s.field)
	if !ok {
		return nil
	}
	if fmt.Sprint(value) == fmt.Sprint(s.equals) {
		event.Dropped = true
	}
	return nil
}

func stageName(stage ParserStage) string {
	if stage.StageID != "" {
		return stage.StageID
	}
	if stage.Type != "" {
		return stage.Type
	}
	return "<unnamed>"
}

func stringConfig(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSliceConfig(config map[string]any, key string) []string {
	if len(config) == 0 {
		return nil
	}
	switch values := config[key].(type) {
	case []string:
		return cloneStringSlice(values)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := strings.TrimSpace(fmt.Sprint(value)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(values) == "" {
			return nil
		}
		return []string{strings.TrimSpace(values)}
	default:
		return nil
	}
}

func stringMapConfig(config map[string]any, key string) map[string]string {
	if len(config) == 0 {
		return nil
	}
	switch values := config[key].(type) {
	case map[string]string:
		return cloneStringMap(values)
	case map[string]any:
		out := make(map[string]string, len(values))
		for target, source := range values {
			sourceValue := strings.TrimSpace(fmt.Sprint(source))
			if strings.TrimSpace(target) != "" && sourceValue != "" {
				out[strings.TrimSpace(target)] = sourceValue
			}
		}
		return out
	default:
		return nil
	}
}

func anyMapConfig(config map[string]any, key string) map[string]any {
	if len(config) == 0 {
		return nil
	}
	if values, ok := config[key].(map[string]any); ok {
		return cloneAnyMap(values)
	}
	return nil
}

func getField(fields map[string]any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}
	if value, ok := fields[path]; ok {
		return value, true
	}
	parts := strings.Split(path, ".")
	var current any = fields
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setField(fields map[string]any, path string, value any) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	parts := strings.Split(path, ".")
	if len(parts) == 1 {
		fields[parts[0]] = value
		return
	}
	current := fields
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}
