package contentpacks

import "time"

func CloneManifest(in Manifest) Manifest {
	out := in
	out.Labels = cloneStringMap(in.Labels)
	out.Provenance.Sources = cloneStringSlice(in.Provenance.Sources)
	if in.Provenance.BuiltAt != nil {
		out.Provenance.BuiltAt = cloneTimePtr(*in.Provenance.BuiltAt)
	}
	out.Sources = make([]SourceProfile, len(in.Sources))
	for i, source := range in.Sources {
		out.Sources[i] = cloneSourceProfile(source)
	}
	out.Parsers = make([]ParserProfile, len(in.Parsers))
	for i, parser := range in.Parsers {
		out.Parsers[i] = cloneParserProfile(parser)
	}
	out.Detections = make([]Detection, len(in.Detections))
	for i, detection := range in.Detections {
		out.Detections[i] = detection
		out.Detections[i].Tags = cloneStringSlice(detection.Tags)
		if detection.Temporal != nil {
			temporal := *detection.Temporal
			temporal.GroupBy = cloneStringSlice(detection.Temporal.GroupBy)
			temporal.Sequence = cloneDetectionTemporalSteps(detection.Temporal.Sequence)
			temporal.Join = cloneDetectionTemporalSteps(detection.Temporal.Join)
			out.Detections[i].Temporal = &temporal
		}
	}
	out.Samples = append([]SampleCase(nil), in.Samples...)
	return out
}

func cloneSourceProfile(in SourceProfile) SourceProfile {
	out := in
	out.Versions = cloneStringSlice(in.Versions)
	out.CollectorModes = cloneStringSlice(in.CollectorModes)
	out.CollectorRecipes = make([]CollectorRecipe, len(in.CollectorRecipes))
	for i, recipe := range in.CollectorRecipes {
		out.CollectorRecipes[i] = recipe
		out.CollectorRecipes[i].Config = cloneAnyMap(recipe.Config)
	}
	out.RequiredPrivileges = cloneStringSlice(in.RequiredPrivileges)
	out.Parsers = cloneStringSlice(in.Parsers)
	out.Detections = cloneStringSlice(in.Detections)
	out.Samples = cloneStringSlice(in.Samples)
	out.Labels = cloneStringMap(in.Labels)
	out.Metadata = cloneStringMap(in.Metadata)
	out.Schemas.ExportAliases = cloneStringSlice(in.Schemas.ExportAliases)
	return out
}

func cloneParserProfile(in ParserProfile) ParserProfile {
	out := in
	out.Stages = make([]ParserStage, len(in.Stages))
	for i, stage := range in.Stages {
		out.Stages[i] = stage
		out.Stages[i].Config = cloneAnyMap(stage.Config)
	}
	out.Labels = cloneStringMap(in.Labels)
	return out
}

func cloneDetectionTemporalSteps(in []DetectionTemporalStep) []DetectionTemporalStep {
	if len(in) == 0 {
		return nil
	}
	out := make([]DetectionTemporalStep, len(in))
	for i, step := range in {
		out[i] = step
		out[i].Values = cloneAnySlice(step.Values)
	}
	return out
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAnySlice(in []any) []any {
	if len(in) == 0 {
		return nil
	}
	out := make([]any, len(in))
	for i, item := range in {
		out[i] = cloneAny(item)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case map[string]string:
		return cloneStringMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		return cloneStringSlice(typed)
	default:
		return typed
	}
}

func cloneTimePtr(in time.Time) *time.Time {
	out := in
	return &out
}
