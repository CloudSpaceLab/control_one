package storage

import (
	"testing"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
	"github.com/CloudSpaceLab/control_one/internal/detections"
)

func TestContentPackDetectionArtifactNormalizationRoundTrip(t *testing.T) {
	rule := detections.Rule{
		ID:       "windows.encoded",
		Title:    "Encoded PowerShell",
		Severity: "high",
		Expression: detections.All(
			detections.Field("process.executable", detections.OpEndsWith, `\powershell.exe`),
			detections.Field("process.command_line", detections.OpContains, "-enc"),
		),
	}
	artifact, detectionJSON, ruleJSON, err := normalizeContentPackDetectionArtifact(uuid.New(), uuid.New(), ContentPackDetectionArtifact{
		PackID:      " controlone.test ",
		PackVersion: " 1.0.0 ",
		SourceID:    " windows.sysmon ",
		DetectionID: " windows.encoded ",
		Detection: contentpacks.Detection{
			DetectionID: "windows.encoded",
			Title:       "Encoded PowerShell",
			Kind:        contentpacks.DetectionKindSigma,
		},
		Rule: rule,
	})
	if err != nil {
		t.Fatalf("normalize artifact: %v", err)
	}
	if artifact.PackID != "controlone.test" || artifact.SourceID != "windows.sysmon" || len(detectionJSON) == 0 || len(ruleJSON) == 0 {
		t.Fatalf("artifact=%#v detectionJSON=%s ruleJSON=%s", artifact, detectionJSON, ruleJSON)
	}
}
