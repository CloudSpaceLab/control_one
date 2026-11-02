package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
)

type patchGovernanceStore interface {
	ListTenantPatchReviewFindings(context.Context, uuid.UUID) ([]storage.VulnerabilityFinding, int, error)
	GetPatchGovernancePolicy(context.Context, uuid.UUID) (*storage.PatchGovernancePolicy, error)
	SavePatchGovernancePolicy(context.Context, uuid.UUID, int64, patchgovernance.Policy, string) (*storage.PatchGovernancePolicy, error)
	SavePatchGovernanceTracking(context.Context, uuid.UUID, uuid.UUID, int64, patchgovernance.Tracking, string) (*storage.PatchGovernanceTracking, error)
	ListPatchGovernanceTracking(context.Context, uuid.UUID) ([]storage.PatchGovernanceTracking, error)
	ListPatchGovernanceEvents(context.Context, uuid.UUID, int) ([]storage.PatchGovernanceEvent, bool, error)
}
type patchGovernanceFinding struct {
	Finding            storage.VulnerabilityFinding     `json:"finding"`
	Tracking           *storage.PatchGovernanceTracking `json:"tracking,omitempty"`
	Decision           patchgovernance.Decision         `json:"decision"`
	ExceptionStatus    string                           `json:"exception_status"`
	EscalationRequired bool                             `json:"escalation_required"`
	EvidenceCurrent    bool                             `json:"evidence_current"`
}

type patchGovernancePagingStore interface {
	GetPatchReviewPage(context.Context, uuid.UUID, uuid.UUID, int) (storage.PatchReviewPage, error)
	GetPatchReviewEventPage(context.Context, uuid.UUID, int, int64, int) ([]storage.PatchGovernanceEvent, int64, error)
	WritePatchReviewExport(context.Context, uuid.UUID, io.Writer, *storage.PatchReviewArchiveInput) error
	RecordPatchReviewArchive(context.Context, uuid.UUID, string, string, storage.PatchReviewArchiveInput) (uuid.UUID, error)
}

type reviewExportWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *reviewExportWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("export exceeds 64 MiB; request an operator-managed archive")
	}
	n, e := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, e
}

func (s *Server) downloadPatchReviewExport(w http.ResponseWriter, r *http.Request, tenant uuid.UUID, actor string, store patchGovernancePagingStore) {
	file, err := os.CreateTemp("", "controlone-patch-review-*.json")
	if err != nil {
		http.Error(w, "cannot prepare evidence export", 503)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	digest := sha256.New()
	var manifest storage.PatchReviewArchiveInput
	if err = store.WritePatchReviewExport(ctx, tenant, &reviewExportWriter{writer: io.MultiWriter(file, digest), remaining: 64 << 20}, &manifest); err != nil {
		http.Error(w, "complete export could not be produced; no partial archive delivered", 503)
		return
	}
	if err = file.Sync(); err != nil {
		http.Error(w, "cannot finalize evidence export", 503)
		return
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	id, err := store.RecordPatchReviewArchive(ctx, tenant, actor, hash, manifest)
	if err != nil {
		http.Error(w, "cannot record complete export receipt", 503)
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "cannot open complete export", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="controlone-patch-review.json"`)
	w.Header().Set("X-Patch-Archive-ID", id.String())
	w.Header().Set("X-Content-SHA256", hash)
	http.ServeContent(w, r, "controlone-patch-review.json", time.Now().UTC(), file)
}

// Reviews are read-only recommendations. Saving an exception never suppresses
// a finding, bypasses a safety gate or authorizes a package job.
func (s *Server) handlePatchGovernance(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if principal.Type == "agent" {
		http.Error(w, "operator access required", 403)
		return
	}
	tenant, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, "tenant_id must be UUID", 400)
		return
	}
	roles := []string{roleViewer, roleOperator, roleAdmin}
	if r.Method == http.MethodPut {
		roles = []string{roleAdmin}
	}
	if r.Method == http.MethodPost {
		roles = []string{roleOperator, roleAdmin}
	}
	if !s.requireTenantAccess(w, r, principal, tenant, roles...) {
		return
	}
	store, ok := s.store.(patchGovernanceStore)
	if !ok {
		http.Error(w, "patch review storage unavailable", 503)
		return
	}
	policy, err := store.GetPatchGovernancePolicy(r.Context(), tenant)
	if err != nil {
		http.Error(w, "cannot read patch review policy", 503)
		return
	}
	action := r.URL.Query().Get("action")
	if r.Method == http.MethodPut {
		var req struct {
			ExpectedRevision int64                  `json:"expected_revision"`
			Policy           patchgovernance.Policy `json:"policy"`
		}
		if !decodeGovernance(w, r, &req) {
			return
		}
		if err = req.Policy.Validate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		saved, e := store.SavePatchGovernancePolicy(r.Context(), tenant, req.ExpectedRevision, req.Policy, principal.Subject)
		if e != nil {
			governanceWriteError(w, e)
			return
		}
		s.recordAudit(r.Context(), principal, tenant, "patch.review.policy_updated", "tenant", tenant.String(), map[string]any{"revision": saved.Revision, "mode": saved.Policy.Mode})
		writeJSON(w, 200, saved)
		return
	}
	if r.Method == http.MethodPost {
		if action == "simulate" {
			var input patchgovernance.Input
			if !decodeGovernance(w, r, &input) {
				return
			}
			if policy == nil {
				http.Error(w, "save a tenant policy before simulation", 409)
				return
			}
			writeJSON(w, 200, map[string]any{"simulation": true, "not_observed_machine_evidence": true, "policy_revision": policy.Revision, "decision": patchgovernance.Evaluate(policy.Policy, input, time.Now().UTC())})
			return
		}
		if action != "tracking" {
			http.Error(w, "unknown review action", 400)
			return
		}
		var req struct {
			FindingID        uuid.UUID                `json:"finding_id"`
			ExpectedRevision int64                    `json:"expected_revision"`
			Tracking         patchgovernance.Tracking `json:"tracking"`
		}
		if !decodeGovernance(w, r, &req) {
			return
		}
		if req.FindingID == uuid.Nil {
			http.Error(w, "finding_id required", 400)
			return
		}
		if err = req.Tracking.Validate(time.Now().UTC()); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		// A policy admin is required for every review mutation carrying an exception,
		// including its renewal. Existing findings and deployment gates are untouched.
		if req.Tracking.ExceptionUntil != nil && !s.requireTenantAccess(w, r, principal, tenant, roleAdmin) {
			return
		}
		saved, e := store.SavePatchGovernanceTracking(r.Context(), tenant, req.FindingID, req.ExpectedRevision, req.Tracking, principal.Subject)
		if e != nil {
			governanceWriteError(w, e)
			return
		}
		s.recordAudit(r.Context(), principal, tenant, "patch.review.finding_updated", "finding", req.FindingID.String(), map[string]any{"revision": saved.Revision})
		writeJSON(w, 200, saved)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, PUT, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	paging, paged := s.store.(patchGovernancePagingStore)
	if action == "export" && paged {
		s.recordAudit(r.Context(), principal, tenant, "patch.review.export_requested", "tenant", tenant.String(), nil)
		s.downloadPatchReviewExport(w, r, tenant, principal.Subject, paging)
		return
	}
	if action == "export" && !paged {
		http.Error(w, "complete snapshot export is unavailable with this storage implementation", 503)
		return
	}
	pageSize := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > 100 {
			http.Error(w, "limit must be 1–100", 400)
			return
		}
	}
	after := uuid.Nil
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		after, err = uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid finding cursor", 400)
			return
		}
	}
	before := int64(0)
	if raw := r.URL.Query().Get("history_cursor"); raw != "" {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			http.Error(w, "invalid history cursor", 400)
			return
		}
	}
	var tracking []storage.PatchGovernanceTracking
	var findings []storage.VulnerabilityFinding
	total := 0
	next := ""
	historyNext := int64(0)
	if paged {
		var page storage.PatchReviewPage
		page, err = paging.GetPatchReviewPage(r.Context(), tenant, after, pageSize)
		tracking, findings, total, next = page.Tracking, page.Findings, page.Total, page.Next
	} else {
		tracking, err = store.ListPatchGovernanceTracking(r.Context(), tenant)
		if err == nil {
			findings, total, err = store.ListTenantPatchReviewFindings(r.Context(), tenant)
		}
	}
	if err != nil {
		http.Error(w, "cannot read finding reviews", 503)
		return
	}
	byID := map[uuid.UUID]*storage.PatchGovernanceTracking{}
	for i := range tracking {
		byID[tracking[i].FindingID] = &tracking[i]
	}
	now := time.Now().UTC()
	out := []patchGovernanceFinding{}
	fresh := map[uuid.UUID]map[string]bool{}
	checked := map[uuid.UUID]bool{}
	for _, finding := range findings {
		if !checked[finding.NodeID] {
			checked[finding.NodeID] = true
			node, e := s.store.GetNode(r.Context(), finding.NodeID)
			if e == nil && node != nil && node.TenantID == tenant {
				a, e := s.latestNodeAssessment(r.Context(), node)
				if e == nil && a != nil && a.CurrentStatus == "partial" && a.CompletedAt.After(now.Add(-24*time.Hour)) {
					var evidence nodeAssessmentEvidence
					if json.Unmarshal(a.Evidence, &evidence) == nil && evidence.FindingsUpdated && len(evidence.Feeds) > 0 {
						fresh[finding.NodeID] = map[string]bool{}
						for _, id := range evidence.AssessedAdvisoryIDs {
							fresh[finding.NodeID][id] = true
						}
					}
				}
			}
		}
		current := fresh[finding.NodeID][finding.CVEID] && !finding.LastSeenAt.After(now) && now.Sub(finding.LastSeenAt) <= 24*time.Hour
		input := patchgovernance.Input{Severity: strings.ToLower(finding.Severity), KEV: finding.KEV, Exposure: "unknown", Importance: "unknown", Compatibility: "unknown", EvidenceCurrent: current, FirstSeen: finding.FirstSeenAt}
		t := byID[finding.ID]
		if t != nil {
			if t.Tracking.ContextExpiresAt != nil && t.Tracking.ContextExpiresAt.After(now) {
				input.Exposure = t.Tracking.Exposure
				input.Importance = t.Tracking.Importance
				input.Compatibility = t.Tracking.Compatibility
			}
		}
		var p patchgovernance.Policy
		if policy != nil {
			p = policy.Policy
		}
		d := patchgovernance.Evaluate(p, input, now)
		if t != nil && (t.Tracking.ContextExpiresAt == nil || !t.Tracking.ContextExpiresAt.After(now)) {
			d.Reasons = append(d.Reasons, "Reviewed context is unavailable or expired; exposure, importance and compatibility are treated as unknown.")
		}
		if t != nil && t.Tracking.DueAt != nil {
			d.DueAt = t.Tracking.DueAt
			d.Overdue = !d.DueAt.After(now)
			d.Reasons = append(d.Reasons, "An operator-reviewed due date overrides the policy-derived date; revision history retains the reason.")
		}
		exception := "none"
		if t != nil && t.Tracking.ExceptionUntil != nil {
			exception = "expired"
			if t.Tracking.ExceptionUntil.After(now) {
				exception = "active"
			}
			d.Reasons = append(d.Reasons, "The recorded review exception never resolves this finding or bypasses deployment approval.")
		}
		out = append(out, patchGovernanceFinding{Finding: finding, Tracking: t, Decision: d, ExceptionStatus: exception, EscalationRequired: t == nil || d.Overdue || exception == "expired", EvidenceCurrent: current})
	}
	days := 0
	events := []storage.PatchGovernanceEvent{}
	truncated := false
	if policy != nil {
		days = policy.Policy.RetentionDays
		if paged {
			events, historyNext, err = paging.GetPatchReviewEventPage(r.Context(), tenant, days, before, pageSize)
			truncated = historyNext > 0
		} else {
			events, truncated, err = store.ListPatchGovernanceEvents(r.Context(), tenant, days)
		}
		if err != nil {
			http.Error(w, "cannot read review history", 503)
			return
		}
	}
	if policy == nil && paged {
		events, historyNext, err = paging.GetPatchReviewEventPage(r.Context(), tenant, 0, before, pageSize)
		if err != nil {
			http.Error(w, "cannot read review history", 503)
			return
		}
		truncated = historyNext > 0
	}
	response := map[string]any{"tenant_id": tenant, "generated_at": now, "policy": policy, "findings": out, "total_findings": total, "findings_truncated": total > len(out), "events": events, "history_truncated": truncated, "history_window_days": days, "retention": "Configured window controls API/export visibility. Durable history is retained; automatic deletion is not enabled.", "automation": map[string]any{"effective_mode": "recommend_only", "installation_authorized": false, "scheduled_installation_available": false, "automatic_installation_available": false, "stop_conditions": []string{"unknown or expired evidence", "unreviewed compatibility/service/reboot impact", "missing exact plan or approval", "changed scope, sources, transport or maintenance window", "circuit breaker or failed health/verification"}}}
	response["retention"] = "The history window filters this view. Complete exports include all retained history. The operations policy controls legal hold and archive-verified retention; no automatic purge runs."
	response["permissions"] = map[string]bool{"manage_policy": s.checkTenantAccess(r.Context(), principal, tenant, roleAdmin) == nil, "review_findings": s.checkTenantAccess(r.Context(), principal, tenant, roleOperator, roleAdmin) == nil}
	if operations, ok := s.store.(patchReviewOperationsStore); ok {
		ops, e := operations.GetPatchReviewOperations(r.Context(), tenant)
		if e != nil {
			http.Error(w, "cannot verify current automation policy", 503)
			return
		}
		mode := "recommend_only"
		if ops.Policy.Automation.Enabled {
			mode = ops.Policy.Automation.Mode
		}
		response["automation"].(map[string]any)["effective_mode"] = mode
		response["automation"].(map[string]any)["scheduled_installation_available"] = true
		response["automation"].(map[string]any)["automatic_installation_available"] = true
	}
	response["pagination"] = map[string]any{"limit": pageSize, "cursor": r.URL.Query().Get("cursor"), "next_cursor": next, "history_cursor": before, "next_history_cursor": historyNext}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, response)
}
func decodeGovernance(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		http.Error(w, "invalid review request", 400)
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		http.Error(w, "one JSON value required", 400)
		return false
	}
	return true
}
func governanceWriteError(w http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrGovernanceConflict) {
		http.Error(w, err.Error(), 409)
		return
	}
	http.Error(w, "review could not be saved; refresh and verify target ownership", 409)
}
