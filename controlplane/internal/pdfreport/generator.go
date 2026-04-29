// Package pdfreport generates styled HTML compliance reports that can be
// printed to PDF from any browser (File → Print → Save as PDF).
package pdfreport

import (
	"bytes"
	"context"
	"html/template"
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
}

// GenerateHTML renders a styled HTML compliance report and returns the bytes.
func GenerateHTML(_ context.Context, data ReportData) ([]byte, error) {
	funcs := template.FuncMap{
		"deref": func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		},
	}
	t, err := template.New("report").Funcs(funcs).Parse(reportTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
  .tag-gray { background: #f1f5f9; color: #475569; }
  .footer { margin-top: 48px; border-top: 1px solid #e2e8f0; padding-top: 16px; font-size: 11px; color: #94a3b8; display: flex; justify-content: space-between; }
  @media print {
    body { font-size: 11px; }
    .page { padding: 20px; }
    table { page-break-inside: auto; }
    tr { page-break-inside: avoid; }
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

  <div class="footer">
    <span>Control One &mdash; Compliance Platform</span>
    <span>Generated {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}</span>
  </div>
</div>
</body>
</html>`
