package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type networkOnboardingPolicyStore interface {
	GetNetworkOnboardingPolicy(context.Context) (*storage.NetworkOnboardingPolicy, error)
	UpsertNetworkOnboardingPolicy(context.Context, []string) error
}

type networkOnboardingPolicyResponse struct {
	AllowedCIDRs []string `json:"allowed_cidrs"`
	Source       string   `json:"source"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

func normalizeNetworkOnboardingCIDRs(values []string) ([]string, error) {
	if len(values) > 64 {
		return nil, fmt.Errorf("a maximum of 64 allowed IPs or CIDR blocks is supported")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("remove blank entries before saving")
		}
		var cidr *net.IPNet
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			_, cidr, _ = net.ParseCIDR(fmt.Sprintf("%s/%d", ip.String(), bits))
		} else {
			_, cidr, _ = net.ParseCIDR(value)
		}
		if cidr == nil {
			return nil, fmt.Errorf("%q is not a valid IP address or CIDR block", value)
		}
		ones, bits := cidr.Mask.Size()
		if ones == 0 || bits == 0 {
			return nil, fmt.Errorf("default routes (0.0.0.0/0 and ::/0) are not allowed")
		}
		canonical := cidr.String()
		if _, exists := seen[canonical]; exists {
			return nil, fmt.Errorf("%q is listed more than once", canonical)
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result, nil
}

func (s *Server) networkOnboardingAllowedCIDRs(ctx context.Context) ([]string, error) {
	if policyStore, ok := s.store.(interface {
		GetNetworkOnboardingPolicy(context.Context) (*storage.NetworkOnboardingPolicy, error)
	}); ok {
		policy, err := policyStore.GetNetworkOnboardingPolicy(ctx)
		if err != nil {
			return nil, err
		}
		if policy != nil {
			return append([]string{}, policy.AllowedCIDRs...), nil
		}
	}
	if s.cfg == nil {
		return []string{}, nil
	}
	return append([]string{}, s.cfg.NetworkOnboarding.AllowedCIDRs...), nil
}

func (s *Server) handleNetworkOnboardingPolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(networkOnboardingPolicyStore)
	if !ok {
		http.Error(w, "network onboarding policy storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodPut {
		var request struct {
			AllowedCIDRs []string `json:"allowed_cidrs"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid network onboarding policy", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "request must contain one JSON object", http.StatusBadRequest)
			return
		}
		cidrs, err := normalizeNetworkOnboardingCIDRs(request.AllowedCIDRs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.UpsertNetworkOnboardingPolicy(r.Context(), cidrs); err != nil {
			http.Error(w, "unable to save network onboarding policy", http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, uuid.Nil, "network_onboarding.policy.updated", "network_onboarding_policy", "allowed_cidrs", map[string]any{"allowed_cidrs": cidrs})
		writeJSON(w, http.StatusOK, networkOnboardingPolicyResponse{AllowedCIDRs: cidrs, Source: "database"})
		return
	}

	policy, err := store.GetNetworkOnboardingPolicy(r.Context())
	if err != nil {
		http.Error(w, "unable to load network onboarding policy", http.StatusInternalServerError)
		return
	}
	response := networkOnboardingPolicyResponse{AllowedCIDRs: []string{}, Source: "server_config"}
	if s.cfg != nil {
		response.AllowedCIDRs = append(response.AllowedCIDRs, s.cfg.NetworkOnboarding.AllowedCIDRs...)
	}
	if policy != nil {
		response.AllowedCIDRs = append([]string{}, policy.AllowedCIDRs...)
		response.Source = "database"
		response.UpdatedAt = policy.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	writeJSON(w, http.StatusOK, response)
}
