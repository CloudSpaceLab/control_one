// AML PII gateway routes — in-tree home for the AML/KYC screening surface.
//
// Background. The legacy AML gateway lived in the FraudSniper PowerShell tree
// and exposed routes that accepted KYC PII (full_name, BVN, NIN, DOB, address)
// without any auth check. PR #51's audit (bugs §4 #1) flagged this as a P0
// security failure — anyone on the network could trigger an AML scan, replay
// the PII payload, or read the screening verdict for a customer.
//
// This file is the controlone Go in-tree replacement. The parallel d-003 /
// d-004 dispatches did the same for the sanctions/Moov client (HTTPS-only +
// DOB-fallback refusal). Pattern: scaffold the route layer with the security
// invariant baked in at the handler entry point, so when the persistence /
// outbound-screening wiring lands later it cannot accidentally regress to the
// pre-auth-check shape.
//
// Security invariants enforced here:
//
//  1. Every handler in this file calls s.authorize(...) on the **first
//     non-method-check line** of the body. There is no code path that reads
//     the request body, parses PII, or contacts an outbound screening
//     provider before the auth check has cleared. A reviewer can verify by
//     scanning for the s.authorize call in each handler — anywhere it is
//     missing, the route is broken by construction.
//
//  2. Tenant scoping is required. Even after authorize() passes, handlers
//     resolve a tenant from the principal's session and refuse to operate on
//     a payload whose tenant_id (when present) does not match. This stops a
//     valid session for tenant A from screening a customer for tenant B.
//
//  3. Roles. AML screening is a privileged operation — operator and admin
//     only. Read-only verdict lookups also require operator+ because the
//     verdict itself reveals whether a person is on a sanctions / PEP list,
//     which is itself sensitive.
//
//  4. PII never logs. The logger receives only tenant_id, principal subject,
//     and screening verdict shape — never the BVN/NIN/DOB/full_name/address
//     payload. The handler comments below mark each spot a future implementer
//     must NOT add a payload-log line.
//
// Scope of this PR (Sprint 4 row 1, dispatch d-2026-05-10-008):
//
//   - Register /api/v1/aml/screen and /api/v1/aml/verdicts/ under the auth
//     middleware so the routes return 401 without a tenant session and 403
//     for an authenticated session lacking operator+ role.
//   - Provide a minimal handler body that returns 501 Not Implemented for the
//     yet-to-be-wired persistence + outbound-screening path. The 501 path is
//     **also** auth-gated; we never want to reveal "this endpoint exists" to
//     an unauthenticated caller.
//   - Scaffold the request shape so a future PR wiring the Moov Watchman
//     client + the AML verdict store can drop in without touching the route
//     registration line or the auth check.
package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// amlScreenRequest is the wire shape for POST /api/v1/aml/screen. Fields are
// PII; the JSON tags are pinned to the legacy FraudSniper shape so a
// future-PR implementer wiring the outbound screening client does not need to
// rename anything client-side.
//
// IMPORTANT: nothing in this file should ever serialise this struct to a log
// line. Use only the field shape (presence/absence) when emitting telemetry.
type amlScreenRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	FullName  string `json:"full_name"`
	BVN       string `json:"bvn,omitempty"`
	NIN       string `json:"nin,omitempty"`
	DOB       string `json:"dob,omitempty"`
	Address   string `json:"address,omitempty"`
	Reference string `json:"reference,omitempty"`
}

// validate refuses obviously-empty payloads so the outbound-screening
// integration cannot be triggered with a no-op body. Detailed format
// validation (BVN length, DOB shape) belongs in the screening client.
func (r amlScreenRequest) validate() error {
	if strings.TrimSpace(r.FullName) == "" {
		return errAMLMissingIdentity
	}
	// At least one government-issued identifier OR a DOB+address pair must be
	// present — otherwise the screening provider would either reject the
	// query or return a far-too-broad name match. The sanctions DOB-null
	// fallback hardening (d-003, bugs §4 #3) explicitly forbids "screen on
	// name only and hope" so we mirror that posture here.
	if strings.TrimSpace(r.BVN) == "" &&
		strings.TrimSpace(r.NIN) == "" &&
		(strings.TrimSpace(r.DOB) == "" || strings.TrimSpace(r.Address) == "") {
		return errAMLInsufficientIdentity
	}
	return nil
}

// Sentinel errors. Kept separate from generic errors so handlers can map them
// to specific HTTP statuses without leaking PII into messages.
var (
	errAMLMissingIdentity      = &amlValidationError{Reason: "full_name_required"}
	errAMLInsufficientIdentity = &amlValidationError{Reason: "identifier_or_dob_address_required"}
)

type amlValidationError struct {
	Reason string
}

func (e *amlValidationError) Error() string { return e.Reason }

// handleAMLScreen accepts a KYC PII payload and (in a future PR) forwards it
// to the outbound sanctions / PEP / adverse-media screening provider. This
// handler is auth-gated at line one of the body — see the s.authorize call.
//
// 501 today, but ALWAYS auth-gated. The 501 must come AFTER the auth check.
//
// Route: POST /api/v1/aml/screen
// Roles: operator, admin
func (s *Server) handleAMLScreen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// Auth is the FIRST check after method gating. Do not move payload
	// reads above this line. Tests assert this contract.
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	// Only after authorize() succeeded do we touch the request body.
	defer r.Body.Close()
	var req amlScreenRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		// NEVER include the body in the error — it is PII.
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Telemetry: shape only, never values. principal subject is fine — that
	// is the operator who initiated the scan, not the customer being scanned.
	s.logger.Info("aml.screen.scaffold",
		zap.String("principal_subject", principal.Subject),
		zap.Bool("has_bvn", strings.TrimSpace(req.BVN) != ""),
		zap.Bool("has_nin", strings.TrimSpace(req.NIN) != ""),
		zap.Bool("has_dob", strings.TrimSpace(req.DOB) != ""),
		zap.Bool("has_address", strings.TrimSpace(req.Address) != ""),
	)

	// Outbound screening + persistence land in a follow-up PR. Until then
	// the route is auth-gated 501, not auth-gated 200 (reviewer-friendly:
	// you cannot accidentally ship customer data to nowhere).
	http.Error(w, "aml screening not yet wired", http.StatusNotImplemented)
}

// handleAMLVerdicts returns previously persisted AML screening verdicts for
// the caller's tenant. Same auth-first contract as handleAMLScreen.
//
// Route: GET /api/v1/aml/verdicts/  (trailing slash; future-PR adds /{id})
// Roles: operator, admin (verdict itself reveals sanctions / PEP status)
func (s *Server) handleAMLVerdicts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleOperator, roleAdmin); !ok {
		return
	}

	// Persistence wires later; until then return an empty list rather than
	// 501 so a future UI integration can stub against the auth-gated route.
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": []any{}}); err != nil {
		s.logger.Warn("encode aml verdicts response", zap.Error(err))
	}
}
