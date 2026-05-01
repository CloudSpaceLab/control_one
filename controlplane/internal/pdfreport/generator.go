// Package pdfreport generates styled HTML compliance reports that can be
// printed to PDF from any browser (File → Print → Save as PDF).
package pdfreport

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/compliance"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ReportData carries all data needed to render a compliance report.
type ReportData struct {
	TenantName   string
	Framework    string
	PeriodStart  time.Time
	PeriodEnd    time.Time
	GeneratedAt  time.Time
	Controls     []compliance.ControlMapping
	EvidenceList []storage.ComplianceEvidence
	OpenFindings int
	TotalPassed  int
	TotalFailed  int

	// Real-data inputs populated by handleDownloadAuditReport. Empty values
	// render as graceful "no data" sections rather than placeholders.
	ControlSummary []storage.ControlCoverage // one entry per control_id mapped to the framework
	PerNodeMatrix  []storage.NodeControlRow  // per-node × control results, capped at 500 rows
	GapAnalysis    []storage.ControlCoverage // subset of ControlSummary where Status == "NO_COVERAGE"
}

// GenerateHTML renders a styled HTML compliance report and returns the bytes.
func GenerateHTML(_ context.Context, data ReportData) ([]byte, error) {
	view := buildView(data)
	t, err := template.New("report").Funcs(templateFuncs).Parse(reportTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, view); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateText renders a plain-text equivalent of the report — used when a
// caller passes ?text_only=1 or when the HTML template fails. Deterministic
// output makes this a useful fixture target for tests as well.
func GenerateText(_ context.Context, data ReportData) ([]byte, error) {
	v := buildView(data)
	var b strings.Builder

	fmt.Fprintf(&b, "Control One — Compliance Audit Report\n")
	fmt.Fprintf(&b, "Organisation: %s\n", v.TenantName)
	fmt.Fprintf(&b, "Framework:    %s\n", v.Framework)
	fmt.Fprintf(&b, "Period:       %s — %s\n",
		v.PeriodStart.Format("2006-01-02"), v.PeriodEnd.Format("2006-01-02"))
	fmt.Fprintf(&b, "Generated:    %s\n\n", v.GeneratedAt.Format("2006-01-02 15:04 UTC"))

	fmt.Fprintf(&b, "Executive Summary\n")
	fmt.Fprintf(&b, "  Controls Passed:  %d\n", v.TotalPassed)
	fmt.Fprintf(&b, "  Controls Failed:  %d\n", v.TotalFailed)
	fmt.Fprintf(&b, "  Open Findings:    %d\n", v.OpenFindings)
	fmt.Fprintf(&b, "  Evidence Items:   %d\n\n", len(v.EvidenceList))

	if len(v.ControlSummary) > 0 {
		fmt.Fprintf(&b, "Control Status\n")
		for _, c := range v.ControlSummary {
			fmt.Fprintf(&b, "  [%s] %-14s nodes %d/%d  evidence %d  %s\n",
				c.Status, c.ControlID, c.NodesPassing, c.NodesChecked, c.EvidenceCount, c.Title)
		}
		fmt.Fprintln(&b)
	}

	if len(v.GapAnalysis) > 0 {
		fmt.Fprintf(&b, "Gap Analysis (controls without automated coverage)\n")
		for _, c := range v.GapAnalysis {
			fmt.Fprintf(&b, "  - %s — %s\n", c.ControlID, c.Title)
		}
		fmt.Fprintln(&b)
	}

	if len(v.EvidenceList) > 0 {
		fmt.Fprintf(&b, "Evidence Inventory\n")
		for _, e := range v.EvidenceList {
			ref := "—"
			if e.ControlRef != nil {
				ref = *e.ControlRef
			}
			fmt.Fprintf(&b, "  - %s (%s) [%s] uploaded %s\n",
				e.Title, e.EvidenceType, ref, e.UploadedAt.Format("2006-01-02"))
		}
		fmt.Fprintln(&b)
	}

	if len(v.PerNodeMatrix) > 0 {
		fmt.Fprintf(&b, "Per-Node Compliance Matrix (top %d rows)\n", len(v.PerNodeMatrix))
		for _, r := range v.PerNodeMatrix {
			fmt.Fprintf(&b, "  %-30s %-14s %s\n", r.NodeName, r.ControlID, r.Status)
		}
		fmt.Fprintln(&b)
	}

	return []byte(b.String()), nil
}

// reportView is the template-facing struct. It embeds ReportData and adds
// pre-computed presentation values so the HTML template stays declarative.
type reportView struct {
	ReportData
	StatusCounts statusCounts
	StatusBar    []barSegment
	FailingNodes map[string][]string // control_id → []node_name (for the Failed Controls Detail section)
}

type statusCounts struct {
	Pass       int
	Partial    int
	Fail       int
	NoCoverage int
	Total      int
}

type barSegment struct {
	Label   string
	Class   string
	Pct     int    // 0..100
	Width   string // "47%"
	Display bool
}

func buildView(data ReportData) reportView {
	v := reportView{ReportData: data}
	for _, c := range data.ControlSummary {
		v.StatusCounts.Total++
		switch c.Status {
		case "PASS":
			v.StatusCounts.Pass++
		case "PARTIAL":
			v.StatusCounts.Partial++
		case "FAIL":
			v.StatusCounts.Fail++
		case "NO_COVERAGE":
			v.StatusCounts.NoCoverage++
		}
	}
	if v.StatusCounts.Total > 0 {
		segs := []barSegment{
			{Label: "Pass", Class: "pass", Pct: pct(v.StatusCounts.Pass, v.StatusCounts.Total)},
			{Label: "Partial", Class: "partial", Pct: pct(v.StatusCounts.Partial, v.StatusCounts.Total)},
			{Label: "Fail", Class: "fail", Pct: pct(v.StatusCounts.Fail, v.StatusCounts.Total)},
			{Label: "No coverage", Class: "nocov", Pct: pct(v.StatusCounts.NoCoverage, v.StatusCounts.Total)},
		}
		for i := range segs {
			segs[i].Width = fmt.Sprintf("%d%%", segs[i].Pct)
			segs[i].Display = segs[i].Pct > 0
		}
		v.StatusBar = segs
	}

	// Failing nodes per control, derived from PerNodeMatrix. Sorted for a
	// stable golden-file diff.
	v.FailingNodes = make(map[string][]string)
	for _, r := range data.PerNodeMatrix {
		if r.Status == "FAIL" {
			v.FailingNodes[r.ControlID] = append(v.FailingNodes[r.ControlID], r.NodeName)
		}
	}
	for k := range v.FailingNodes {
		sort.Strings(v.FailingNodes[k])
	}
	return v
}

func pct(part, total int) int {
	if total <= 0 {
		return 0
	}
	return int(float64(part) * 100.0 / float64(total))
}

var templateFuncs = template.FuncMap{
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	"formatDate": func(t *time.Time) string {
		if t == nil {
			return "—"
		}
		return t.Format("2006-01-02")
	},
	"toneClass": func(status string) string {
		switch status {
		case "PASS":
			return "tag-green"
		case "PARTIAL":
			return "tag-amber"
		case "FAIL":
			return "tag-red"
		case "NO_COVERAGE":
			return "tag-gray"
		default:
			return "tag-gray"
		}
	},
	"join": func(sep string, ss []string) string {
		return strings.Join(ss, sep)
	},
	"failingNodes": func(m map[string][]string, key string) []string {
		return m[key]
	},
}

const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>Compliance Audit Report — {{.Framework}}</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: 'Segoe UI', Arial, sans-serif; font-size: 13px; color: #1a1a2e; background: #fff; }
  .page { max-width: 960px; margin: 0 auto; padding: 40px 48px; }
  .header { border-bottom: 3px solid #1e40af; padding-bottom: 20px; margin-bottom: 32px; }
  .header-top { display: flex; justify-content: space-between; align-items: flex-start; }
  .logo-area h1 { font-size: 22px; font-weight: 700; color: #1e40af; }
  .logo-area p { font-size: 12px; color: #64748b; margin-top: 4px; }
  .header-meta { text-align: right; font-size: 12px; color: #64748b; line-height: 1.8; }
  .badge { display: inline-block; padding: 2px 10px; border-radius: 9999px; font-size: 11px; font-weight: 600; background: #dbeafe; color: #1d4ed8; margin-top: 6px; }
  .section-title { font-size: 14px; font-weight: 700; color: #1e3a5f; text-transform: uppercase; letter-spacing: 0.05em; margin: 28px 0 12px; border-left: 4px solid #1e40af; padding-left: 10px; }
  .kpi-row { display: flex; gap: 16px; margin-bottom: 24px; }
  .kpi { flex: 1; border: 1px solid #e2e8f0; border-radius: 8px; padding: 16px; text-align: center; }
  .kpi .num { font-size: 28px; font-weight: 700; }
  .kpi .lbl { font-size: 11px; color: #64748b; margin-top: 4px; text-transform: uppercase; letter-spacing: 0.04em; }
  .kpi.pass .num { color: #16a34a; }
  .kpi.fail .num { color: #dc2626; }
  .kpi.open .num { color: #d97706; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 24px; }
  th { background: #f1f5f9; text-align: left; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; color: #475569; padding: 8px 12px; border: 1px solid #e2e8f0; }
  td { padding: 8px 12px; border: 1px solid #e2e8f0; vertical-align: top; font-size: 12px; }
  tr:nth-child(even) td { background: #f8fafc; }
  .tag { display: inline-block; padding: 1px 8px; border-radius: 9999px; font-size: 10px; font-weight: 600; }
  .tag-blue { background: #dbeafe; color: #1d4ed8; }
  .tag-green { background: #dcfce7; color: #15803d; }
  .tag-amber { background: #fef3c7; color: #b45309; }
  .tag-red { background: #fee2e2; color: #b91c1c; }
  .tag-gray { background: #f1f5f9; color: #475569; }
  .status-bar { display: flex; height: 28px; width: 100%; border: 1px solid #e2e8f0; border-radius: 6px; overflow: hidden; margin-bottom: 8px; }
  .status-bar .seg { display: flex; align-items: center; justify-content: center; color: #fff; font-size: 11px; font-weight: 600; min-width: 0; }
  .status-bar .seg.pass { background: #16a34a; }
  .status-bar .seg.partial { background: #f59e0b; }
  .status-bar .seg.fail { background: #dc2626; }
  .status-bar .seg.nocov { background: #94a3b8; }
  .status-bar-legend { display: flex; gap: 16px; font-size: 11px; color: #475569; margin-bottom: 24px; }
  .status-bar-legend .dot { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 4px; vertical-align: middle; }
  .gap-banner { border: 1px solid #fde68a; background: #fffbeb; border-radius: 8px; padding: 12px 16px; margin-bottom: 24px; font-size: 12px; color: #78350f; }
  .gap-banner h3 { font-size: 13px; margin-bottom: 6px; color: #92400e; }
  .gap-banner ul { padding-left: 20px; }
  .matrix th, .matrix td { font-size: 10px; padding: 4px 8px; }
  .matrix .ok { color: #16a34a; font-weight: 700; }
  .matrix .no { color: #dc2626; font-weight: 700; }
  .footer { margin-top: 48px; border-top: 1px solid #e2e8f0; padding-top: 16px; font-size: 11px; color: #94a3b8; display: flex; justify-content: space-between; }
  @media print {
    body { font-size: 11px; }
    .page { padding: 20px; }
    table { page-break-inside: auto; }
    tr { page-break-inside: avoid; }
    .section-title { page-break-after: avoid; }
  }
</style>
</head>
<body>
<div class="page">
  <div class="header">
    <div class="header-top">
      <div class="logo-area">
        <h1>Control One</h1>
        <p>Compliance Audit Report</p>
      </div>
      <div class="header-meta">
        <div><strong>Organisation:</strong> {{.TenantName}}</div>
        <div><strong>Framework:</strong> {{.Framework}}</div>
        <div><strong>Period:</strong> {{.PeriodStart.Format "2006-01-02"}} &ndash; {{.PeriodEnd.Format "2006-01-02"}}</div>
        <div><strong>Generated:</strong> {{.GeneratedAt.Format "2006-01-02 15:04 UTC"}}</div>
        <div><span class="badge">CONFIDENTIAL</span></div>
      </div>
    </div>
  </div>

  <div class="section-title">Executive Summary</div>
  <div class="kpi-row">
    <div class="kpi pass">
      <div class="num">{{.TotalPassed}}</div>
      <div class="lbl">Controls Passed</div>
    </div>
    <div class="kpi fail">
      <div class="num">{{.TotalFailed}}</div>
      <div class="lbl">Controls Failed</div>
    </div>
    <div class="kpi open">
      <div class="num">{{.OpenFindings}}</div>
      <div class="lbl">Open Findings</div>
    </div>
    <div class="kpi">
      <div class="num">{{len .EvidenceList}}</div>
      <div class="lbl">Evidence Items</div>
    </div>
  </div>

  {{if .StatusBar}}
  <div class="status-bar">
    {{range .StatusBar}}{{if .Display}}<div class="seg {{.Class}}" style="width:{{.Width}}">{{.Width}}</div>{{end}}{{end}}
  </div>
  <div class="status-bar-legend">
    <span><span class="dot" style="background:#16a34a"></span>Pass ({{.StatusCounts.Pass}})</span>
    <span><span class="dot" style="background:#f59e0b"></span>Partial ({{.StatusCounts.Partial}})</span>
    <span><span class="dot" style="background:#dc2626"></span>Fail ({{.StatusCounts.Fail}})</span>
    <span><span class="dot" style="background:#94a3b8"></span>No coverage ({{.StatusCounts.NoCoverage}})</span>
  </div>
  {{end}}

  {{if .GapAnalysis}}
  <div class="gap-banner">
    <h3>Gap Analysis — {{len .GapAnalysis}} control(s) without automated coverage</h3>
    <p>The controls below have no policy mapped or no scan results within the reporting period. Add evidence or assign a policy to close these gaps.</p>
    <ul>
      {{range .GapAnalysis}}
      <li><strong>{{.ControlID}}</strong> — {{.Title}}</li>
      {{end}}
    </ul>
  </div>
  {{end}}

  {{if .ControlSummary}}
  <div class="section-title">Control Status</div>
  <table>
    <thead>
      <tr>
        <th style="width:120px">Control ID</th>
        <th>Title</th>
        <th style="width:90px">Status</th>
        <th style="width:90px">Nodes</th>
        <th style="width:80px">Evidence</th>
        <th style="width:110px">Last Checked</th>
      </tr>
    </thead>
    <tbody>
      {{range .ControlSummary}}
      <tr>
        <td><span class="tag tag-blue">{{.ControlID}}</span></td>
        <td>{{.Title}}</td>
        <td><span class="tag {{toneClass .Status}}">{{.Status}}</span></td>
        <td>{{.NodesPassing}}/{{.NodesChecked}}</td>
        <td>{{.EvidenceCount}}</td>
        <td>{{formatDate .LastCheckedAt}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  {{$failingNodes := .FailingNodes}}
  {{$hasFailing := false}}
  {{range .ControlSummary}}{{if or (eq .Status "FAIL") (eq .Status "PARTIAL")}}{{$hasFailing = true}}{{end}}{{end}}
  {{if $hasFailing}}
  <div class="section-title">Failed Controls — Detail</div>
  <table>
    <thead>
      <tr>
        <th style="width:120px">Control ID</th>
        <th>Title</th>
        <th>Failing Nodes</th>
      </tr>
    </thead>
    <tbody>
      {{range .ControlSummary}}{{if or (eq .Status "FAIL") (eq .Status "PARTIAL")}}
      <tr>
        <td><span class="tag tag-blue">{{.ControlID}}</span></td>
        <td>{{.Title}}</td>
        <td>{{$nodes := failingNodes $failingNodes .ControlID}}{{if $nodes}}{{join ", " $nodes}}{{else}}&mdash;{{end}}</td>
      </tr>
      {{end}}{{end}}
    </tbody>
  </table>
  {{end}}
  {{else}}
  <div class="section-title">Framework Controls — {{.Framework}}</div>
  <table>
    <thead>
      <tr>
        <th style="width:120px">Control ID</th>
        <th>Title</th>
        <th>Description</th>
      </tr>
    </thead>
    <tbody>
      {{range .Controls}}
      <tr>
        <td><span class="tag tag-blue">{{.ControlID}}</span></td>
        <td>{{.Title}}</td>
        <td>{{.Description}}</td>
      </tr>
      {{else}}
      <tr><td colspan="3" style="text-align:center;color:#94a3b8;">No controls defined for this framework.</td></tr>
      {{end}}
    </tbody>
  </table>
  {{end}}

  {{if .EvidenceList}}
  <div class="section-title">Evidence Inventory</div>
  <table>
    <thead>
      <tr>
        <th>Title</th>
        <th>Type</th>
        <th>Control Ref</th>
        <th>Uploaded</th>
        <th>Expires</th>
      </tr>
    </thead>
    <tbody>
      {{range .EvidenceList}}
      <tr>
        <td>{{.Title}}</td>
        <td><span class="tag tag-gray">{{.EvidenceType}}</span></td>
        <td>{{if .ControlRef}}{{deref .ControlRef}}{{else}}&mdash;{{end}}</td>
        <td>{{.UploadedAt.Format "2006-01-02"}}</td>
        <td>{{if .ExpiresAt}}{{.ExpiresAt.Format "2006-01-02"}}{{else}}&mdash;{{end}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{end}}

  {{if .PerNodeMatrix}}
  <div class="section-title">Per-Node Compliance Matrix</div>
  <table class="matrix">
    <thead>
      <tr>
        <th>Node</th>
        <th>Control</th>
        <th>Status</th>
        <th>Last Checked</th>
      </tr>
    </thead>
    <tbody>
      {{range .PerNodeMatrix}}
      <tr>
        <td>{{.NodeName}}</td>
        <td><span class="tag tag-blue">{{.ControlID}}</span></td>
        <td>{{if eq .Status "PASS"}}<span class="ok">&#x2713; PASS</span>{{else}}<span class="no">&#x2717; FAIL</span>{{end}}</td>
        <td>{{formatDate .LastCheckedAt}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{end}}

  <div class="footer">
    <span>Control One &mdash; Compliance Platform</span>
    <span>Generated {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}</span>
  </div>
</div>
</body>
</html>`
