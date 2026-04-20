package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

// TestHandleAgentInstallScript covers the two installer variants the control
// plane serves from /api/v1/agent/install-script:
//   - default (no ?platform=): bash installer for linux/darwin
//   - ?platform=windows: PowerShell installer for Windows hosts
//
// The response body fragments asserted here are the ones the one-liner relies
// on: the bash script must still end up calling the agent with --install-service,
// and the PS1 script must pipe to PowerShell-safe code (Invoke-WebRequest, the
// agent binary suffix .exe, and the $env:TOKEN baked-in value).
func TestHandleAgentInstallScript(t *testing.T) {
	t.Parallel()

	srv := newInstallScriptTestServer(t)

	tests := []struct {
		name                string
		platform            string
		wantContentType     string
		wantFilenameSuffix  string
		wantBodyFragments   []string
		unwantBodyFragments []string
	}{
		{
			name:               "default serves bash installer",
			platform:           "",
			wantContentType:    "text/x-shellscript",
			wantFilenameSuffix: ".sh",
			wantBodyFragments: []string{
				"#!/usr/bin/env bash",
				`TOKEN="${TOKEN:-enroll-token-42}"`,
				"detect_os()",
				"--install-service",
			},
			unwantBodyFragments: []string{
				"Invoke-WebRequest",
				"$env:TOKEN",
			},
		},
		{
			name:               "platform=windows serves powershell installer",
			platform:           "windows",
			wantContentType:    "text/plain; charset=utf-8",
			wantFilenameSuffix: ".ps1",
			wantBodyFragments: []string{
				"Control One Agent Installer (PowerShell)",
				"$env:TOKEN",
				"enroll-token-42",
				"Invoke-WebRequest",
				"controlone-agent.exe",
				"--install-service",
			},
			unwantBodyFragments: []string{
				"#!/usr/bin/env bash",
				"detect_os()",
			},
		},
		{
			name:               "unknown platform falls back to bash",
			platform:           "solaris",
			wantContentType:    "text/x-shellscript",
			wantFilenameSuffix: ".sh",
			wantBodyFragments: []string{
				"#!/usr/bin/env bash",
				"enroll-token-42",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := "/api/v1/agent/install-script?token=enroll-token-42"
			if tc.platform != "" {
				path += "&platform=" + tc.platform
			}

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			srv.handleAgentInstallScript(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: body=%s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != tc.wantContentType {
				t.Fatalf("Content-Type = %q, want %q", ct, tc.wantContentType)
			}
			disp := rec.Header().Get("Content-Disposition")
			if !strings.HasSuffix(disp, tc.wantFilenameSuffix) {
				t.Fatalf("Content-Disposition = %q, want suffix %q", disp, tc.wantFilenameSuffix)
			}

			body := rec.Body.String()
			for _, frag := range tc.wantBodyFragments {
				if !strings.Contains(body, frag) {
					t.Fatalf("body missing fragment %q; got:\n%s", frag, body)
				}
			}
			for _, frag := range tc.unwantBodyFragments {
				if strings.Contains(body, frag) {
					t.Fatalf("body unexpectedly contains fragment %q", frag)
				}
			}
		})
	}
}

// TestHandleAgentInstallScript_RejectsNonGET ensures non-GET verbs are
// rejected symmetrically for both variants — the install-script path is
// meant to be idempotent and unauthenticated-friendly, which is only safe
// when mutating verbs are refused at the handler.
func TestHandleAgentInstallScript_RejectsNonGET(t *testing.T) {
	t.Parallel()

	srv := newInstallScriptTestServer(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(method, "/api/v1/agent/install-script?platform=windows", nil)
			rec := httptest.NewRecorder()
			srv.handleAgentInstallScript(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("method %s: status = %d, want 405", method, rec.Code)
			}
			if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
				t.Fatalf("method %s: Allow = %q, want GET", method, allow)
			}
		})
	}
}

// TestResolveInstallScriptVariant covers the pure variant-resolution logic in
// isolation. Cheap to run, catches case-sensitivity regressions.
func TestResolveInstallScriptVariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		platform         string
		wantFilenameHint string
	}{
		{"", ".sh"},
		{"linux", ".sh"},
		{"darwin", ".sh"},
		{"windows", ".ps1"},
		{"Windows", ".ps1"},
		{"WINDOWS", ".ps1"},
		{"  windows  ", ".ps1"},
		{"win", ".ps1"},
		{"powershell", ".ps1"},
		{"ps1", ".ps1"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.platform, func(t *testing.T) {
			t.Parallel()
			v := resolveInstallScriptVariant(tc.platform)
			if !strings.HasSuffix(v.contentDisposition, tc.wantFilenameHint) {
				t.Fatalf("platform=%q: Content-Disposition=%q, want suffix %q",
					tc.platform, v.contentDisposition, tc.wantFilenameHint)
			}
			if v.tmpl == nil {
				t.Fatalf("platform=%q: tmpl is nil", tc.platform)
			}
			if v.contentType == "" {
				t.Fatalf("platform=%q: contentType is empty", tc.platform)
			}
		})
	}
}

// newInstallScriptTestServer builds a minimal Server configured to serve the
// install-script handler without requiring a Store, worker, or auth. The
// handler reads only s.cfg.HTTP.Address and s.logger.
func newInstallScriptTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		logger: zap.NewNop(),
		cfg: &config.Config{
			HTTP: config.HTTPConfig{Address: "https://cp.example.test"},
		},
	}
}
