package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// recommendationResponse describes a proposed rule the operator can promote.
type recommendationResponse struct {
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Rationale  string         `json:"rationale"`
	Confidence float64        `json:"confidence"`
	Evidence   map[string]any `json:"evidence"`
	Draft      map[string]any `json:"draft"`
}

// handleRecommendations derives simple rule proposals from observed behavior.
//
// Current source of truth: port_observations aggregated over the request
// window. Logic:
//
//  1. For each (port, protocol), compute the dominant state ratio.
//  2. If >95% of observations over the window are in one state and the
//     port has been observed enough (>=50 samples), propose a port rule
//     locking in that state.
//
// This is intentionally a thin shell that future behavioral rollup can extend.
func (s *Server) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
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
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	stats, err := s.store.AggregatePortObservations(r.Context(), tenantID, since)
	if err != nil {
		s.logger.Warn("aggregate port observations", zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]any{"data": []recommendationResponse{}})
		return
	}

	// Fold into per-port totals.
	type portKey struct {
		Port     int
		Protocol string
	}
	totals := map[portKey]int{}
	stateCounts := map[portKey]map[string]int{}
	for _, s := range stats {
		k := portKey{Port: s.Port, Protocol: s.Protocol}
		totals[k] += s.Count
		if stateCounts[k] == nil {
			stateCounts[k] = map[string]int{}
		}
		stateCounts[k][s.State] = s.Count
	}

	const minSamples = 50
	const dominantThreshold = 0.95
	recs := make([]recommendationResponse, 0)
	for k, total := range totals {
		if total < minSamples {
			continue
		}
		for state, count := range stateCounts[k] {
			ratio := float64(count) / float64(total)
			if ratio >= dominantThreshold {
				recs = append(recs, recommendationResponse{
					Kind:       "port_rule",
					Title:      portRecTitle(k.Port, k.Protocol, state),
					Rationale:  portRecRationale(state, ratio, total),
					Confidence: ratio,
					Evidence: map[string]any{
						"samples":      total,
						"state_counts": stateCounts[k],
						"window_days":  30,
					},
					Draft: map[string]any{
						"port":           k.Port,
						"protocol":       k.Protocol,
						"expected_state": state,
						"severity":       severityForState(state),
						"action":         "notify",
					},
				})
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": recs})
}

func portRecTitle(port int, protocol, state string) string {
	return fmt.Sprintf("Port %d/%s stays %s", port, protocol, state)
}

func portRecRationale(state string, ratio float64, total int) string {
	return fmt.Sprintf("Dominant state %s observed in ~%.1f%% of %d samples over 30 days.", state, ratio*100, total)
}

func severityForState(state string) string {
	if state == "open" {
		return "low"
	}
	return "medium"
}

// _ is here so strconv stays imported when this file is the only consumer in
// some build tags; the recommender will grow more numeric helpers shortly.
var _ = strconv.Itoa
