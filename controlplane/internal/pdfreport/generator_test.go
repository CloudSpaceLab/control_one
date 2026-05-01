package pdfreport

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/compliance"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

var update = flag.Bool("update", false, "rewrite golden fixtures")

// fixedReportData returns a deterministic ReportData payload so the golden-file
// test is reproducible across machines and clocks.
func fixedReportData() ReportData {
	at := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	t := func(d int) *time.Time {
		x := at.Add(time.Duration(-d) * time.Hour)
		return &x
	}
	control := func(id, title, status string, nc, np, nf, ev int, last *time.Time) storage.ControlCoverage {
		return storage.ControlCoverage{
			Framework:     "HIPAA",
			ControlID:     id,
			Title:         title,
			Status:        status,
			NodesChecked:  nc,
			NodesPassing:  np,
			NodesFailing:  nf,
			EvidenceCount: ev,
			LastCheckedAt: last,
		}
	}
	row := func(node, control, status string, last *time.Time) storage.NodeControlRow {
		return storage.NodeControlRow{NodeName: node, ControlID: control, Status: status, LastCheckedAt: last}
	}

	cf := "HIPAA"
	cr := "164.312(b)"
	ev := storage.ComplianceEvidence{
		ID:           uuid.MustParse("00000000-0000-0000-0000-00000000abcd"),
		TenantID:     uuid.MustParse("00000000-0000-0000-0000-000000001111"),
		EvidenceType: "policy-document",
		Framework:    &cf,
		ControlRef:   &cr,
		Title:        "Audit log retention policy",
		UploadedBy:   uuid.MustParse("00000000-0000-0000-0000-000000002222"),
		UploadedAt:   at.Add(-72 * time.Hour),
	}

	return ReportData{
		TenantName:   "Acme Bank",
		Framework:    "HIPAA",
		PeriodStart:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		GeneratedAt:  at,
		Controls:     compliance.FrameworkControls["HIPAA"],
		EvidenceList: []storage.ComplianceEvidence{ev},
		OpenFindings: 1,
		TotalPassed:  4,
		TotalFailed:  1,
		ControlSummary: []storage.ControlCoverage{
			control("164.308(a)(1)", "Security Management Process", "PASS", 3, 3, 0, 1, t(2)),
			control("164.312(a)(1)", "Access Control", "PARTIAL", 3, 2, 1, 0, t(3)),
			control("164.312(b)", "Audit Controls", "PASS", 3, 3, 0, 1, t(4)),
			control("164.312(d)", "Person or Entity Authentication", "FAIL", 3, 0, 3, 0, t(5)),
			control("164.310(a)", "Facility Access Controls", "NO_COVERAGE", 0, 0, 0, 0, nil),
		},
		PerNodeMatrix: []storage.NodeControlRow{
			row("node-a", "164.308(a)(1)", "PASS", t(2)),
			row("node-a", "164.312(d)", "FAIL", t(5)),
			row("node-b", "164.312(a)(1)", "FAIL", t(3)),
			row("node-b", "164.312(d)", "FAIL", t(5)),
			row("node-c", "164.312(d)", "FAIL", t(5)),
		},
		GapAnalysis: []storage.ControlCoverage{
			control("164.310(a)", "Facility Access Controls", "NO_COVERAGE", 0, 0, 0, 0, nil),
		},
	}
}

func TestGenerateHTML_Golden(t *testing.T) {
	data := fixedReportData()
	got, err := GenerateHTML(context.Background(), data)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	got = normalize(got)

	path := filepath.Join("testdata", "report.golden.html")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(normalize(want)) != string(got) {
		t.Fatalf("HTML drift; rerun with -update to refresh.\n--- want\n%s\n--- got\n%s",
			snippet(want, 800), snippet(got, 800))
	}
}

func TestGenerateText_Golden(t *testing.T) {
	data := fixedReportData()
	got, err := GenerateText(context.Background(), data)
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	got = normalize(got)

	path := filepath.Join("testdata", "report.golden.txt")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(normalize(want)) != string(got) {
		t.Fatalf("text drift; rerun with -update to refresh.\n--- want\n%s\n--- got\n%s",
			snippet(want, 800), snippet(got, 800))
	}
}

// TestGenerateHTML_EmptyControlSummary confirms the legacy code path still
// renders when no real-data inputs are present (ControlSummary is empty), so
// the rewrite is backward-compatible with any callers we miss.
func TestGenerateHTML_EmptyControlSummary(t *testing.T) {
	data := ReportData{
		TenantName:  "Acme Bank",
		Framework:   "SOC2",
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Controls:    compliance.FrameworkControls["SOC2"],
	}
	got, err := GenerateHTML(context.Background(), data)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if !strings.Contains(string(got), "Framework Controls — SOC2") {
		t.Fatalf("expected legacy 'Framework Controls' fallback section when ControlSummary empty")
	}
	if strings.Contains(string(got), "Gap Analysis") {
		t.Fatalf("did not expect Gap Analysis with empty ControlSummary")
	}
}

func normalize(b []byte) []byte {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return []byte(s)
}

func snippet(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
