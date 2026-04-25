package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type simulateRequest struct {
	TenantID   string         `json:"tenant_id"`
	RuleType   string         `json:"rule_type"` // port | log | compliance
	WindowDays int            `json:"window_days"`
	Rule       map[string]any `json:"rule"`
}

type simulateResponse struct {
	RuleType       string         `json:"rule_type"`
	WindowDays     int            `json:"window_days"`
	NodesWouldFail int            `json:"nodes_would_fail"`
	NodesWouldPass int            `json:"nodes_would_pass"`
	Summary        string         `json:"summary"`
	Sample         []map[string]any `json:"sample"`
}

// handleSimulate replays a proposed rule against stored observations and
// returns what-if counts. For the go-live MVP we support port rules (against
// port_observations) and compliance rules (against compliance_results).
func (s *Server) handleSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req simulateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	if req.WindowDays <= 0 {
		req.WindowDays = 30
	}

	switch req.RuleType {
	case "port":
		s.simulatePortRule(w, r, tenantID, req)
	case "compliance", "log":
		// Compliance + log rules: basic stub returning counts from existing results.
		s.simulateComplianceRule(w, r, tenantID, req)
	default:
		http.Error(w, "rule_type must be port, log, or compliance", http.StatusBadRequest)
	}
}

func (s *Server) simulatePortRule(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, req simulateRequest) {
	port, _ := req.Rule["port"].(float64)
	proto, _ := req.Rule["protocol"].(string)
	expected, _ := req.Rule["expected_state"].(string)
	if port == 0 || expected == "" {
		http.Error(w, "rule.port and rule.expected_state required", http.StatusBadRequest)
		return
	}
	since := time.Now().UTC().Add(-time.Duration(req.WindowDays) * 24 * time.Hour)
	stats, err := s.store.AggregatePortObservations(r.Context(), tenantID, since)
	if err != nil {
		s.logger.Warn("simulate aggregate", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	var match, mismatch int
	sample := make([]map[string]any, 0, 5)
	for _, st := range stats {
		if st.Port != int(port) {
			continue
		}
		if proto != "" && st.Protocol != proto {
			continue
		}
		if st.State == expected {
			match += st.Count
		} else {
			mismatch += st.Count
		}
		if len(sample) < 5 {
			sample = append(sample, map[string]any{
				"state": st.State, "count": st.Count, "protocol": st.Protocol,
			})
		}
	}
	resp := simulateResponse{
		RuleType:       "port",
		WindowDays:     req.WindowDays,
		NodesWouldFail: mismatch,
		NodesWouldPass: match,
		Summary:        fmt.Sprintf("Over %d days: %d observations would match %q, %d would trigger", req.WindowDays, match, expected, mismatch),
		Sample:         sample,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) simulateComplianceRule(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, req simulateRequest) {
	since := time.Now().UTC().Add(-time.Duration(req.WindowDays) * 24 * time.Hour)
	agg, err := s.store.GetComplianceAggregation(r.Context(), storage.ComplianceResultFilter{TenantID: tenantID, Since: &since})
	if err != nil || agg == nil {
		writeJSON(w, http.StatusOK, simulateResponse{RuleType: req.RuleType, WindowDays: req.WindowDays})
		return
	}
	resp := simulateResponse{
		RuleType:       req.RuleType,
		WindowDays:     req.WindowDays,
		NodesWouldFail: agg.Failed,
		NodesWouldPass: agg.Passed,
		Summary:        fmt.Sprintf("Simulated against %d compliance results (%d passed / %d failed)", agg.Total, agg.Passed, agg.Failed),
	}
	writeJSON(w, http.StatusOK, resp)
}
