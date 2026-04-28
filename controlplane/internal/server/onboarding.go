package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/connect"
)

// Onboarding endpoints power the operator "add a server" wizard. They
// expose the connect package over JSON without leaking credentials into
// audit rows; only the host + protocol + outcome get logged.

type testConnectionRequest struct {
	Protocol   connect.Protocol   `json:"protocol"`
	Host       string             `json:"host"`
	Port       int                `json:"port,omitempty"`
	Username   string             `json:"username"`
	Auth       connect.AuthMethod `json:"auth"`
	Password   string             `json:"password,omitempty"`
	PrivateKey string             `json:"private_key,omitempty"`
	Passphrase string             `json:"passphrase,omitempty"`
	HTTPS      bool               `json:"https,omitempty"`
	SkipVerify bool               `json:"skip_verify,omitempty"`
	TimeoutMs  int                `json:"timeout_ms,omitempty"`
}

type testConnectionResponse struct {
	OK    bool           `json:"ok"`
	Probe *connect.Probe `json:"probe,omitempty"`
	Error string         `json:"error,omitempty"`
}

func (s *Server) handleOnboardingTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator)
	if !ok {
		return
	}

	var req testConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateOnboardingRequest(req); err != nil {
		writeJSON(w, http.StatusBadRequest, testConnectionResponse{OK: false, Error: err.Error()})
		return
	}

	target := connect.Target{
		Protocol:   req.Protocol,
		Host:       strings.TrimSpace(req.Host),
		Port:       req.Port,
		Username:   strings.TrimSpace(req.Username),
		Auth:       req.Auth,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
		HTTPS:      req.HTTPS,
		SkipVerify: req.SkipVerify,
	}
	if req.TimeoutMs > 0 {
		target.Timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	registry := s.connectRegistry()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	probe, err := registry.Test(ctx, target)
	resp := testConnectionResponse{OK: err == nil, Probe: probe}
	if err != nil {
		resp.Error = err.Error()
	}

	// Audit: protocol + host + outcome only. Credentials never persist.
	s.recordAudit(r.Context(), principal, uuid.Nil, "onboarding.test_connection", "server", target.Host,
		map[string]any{
			"protocol": string(target.Protocol),
			"port":     target.Port,
			"ok":       resp.OK,
			"latency":  probeLatency(probe),
		})

	writeJSON(w, http.StatusOK, resp)
}

// onboardingProtocolsResponse advertises what the wizard can probe.
type onboardingProtocolsResponse struct {
	Protocols []connect.Protocol `json:"protocols"`
}

func (s *Server) handleOnboardingProtocols(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	writeJSON(w, http.StatusOK, onboardingProtocolsResponse{
		Protocols: s.connectRegistry().Supported(),
	})
}

func validateOnboardingRequest(r testConnectionRequest) error {
	if r.Host == "" {
		return errors.New("host required")
	}
	switch r.Protocol {
	case connect.ProtoSSH, connect.ProtoWinRM, connect.ProtoRDP:
	default:
		return errors.New("unsupported protocol")
	}
	if r.Protocol == connect.ProtoRDP {
		return nil // TCP-only probe, no credentials needed
	}
	if r.Username == "" {
		return errors.New("username required")
	}
	switch r.Auth {
	case connect.AuthPassword:
		if r.Password == "" {
			return errors.New("password required")
		}
	case connect.AuthPrivateKey:
		if r.PrivateKey == "" {
			return errors.New("private_key required")
		}
		if r.Protocol != connect.ProtoSSH {
			return errors.New("private_key auth is SSH-only")
		}
	default:
		return errors.New("unsupported auth method")
	}
	return nil
}

func probeLatency(p *connect.Probe) int64 {
	if p == nil {
		return 0
	}
	return p.LatencyMs
}

// connectRegistry returns the Server's lazily-initialised connect Registry.
// Tests can override Server.connectRegistryOverride to swap in stubs.
func (s *Server) connectRegistry() *connect.Registry {
	if s == nil {
		return connect.NewRegistry()
	}
	if s.connectRegistryOverride != nil {
		return s.connectRegistryOverride
	}
	s.connectRegistryOnce.Do(func() {
		s.connectRegistryInst = connect.NewRegistry()
	})
	return s.connectRegistryInst
}
