package registration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// Registrar handles node bootstrap and persisted state management.
type Registrar struct {
	client *api.Client
	log    *zap.Logger
}

// RegisterRequest matches the bootstrap payload sent to the control plane.
type RegisterRequest struct {
	BootstrapToken string `json:"bootstrap_token"`
	Hostname       string `json:"hostname"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	PublicIP       string `json:"public_ip"`
	Fingerprint    string `json:"fingerprint"`
}

// RegisterResponse captures relevant fields from control plane registration reply.
type RegisterResponse struct {
	NodeID    string            `json:"node_id"`
	ClientCRT string            `json:"client_cert"`
	ClientKey string            `json:"client_key"`
	CACert    string            `json:"ca_cert"`
	PolicySet []string          `json:"policy_set"`
	Intervals map[string]int64  `json:"intervals"`
	Metadata  map[string]string `json:"metadata"`
}

// State persisted locally to avoid duplicate registration.
type State struct {
	NodeID        string            `json:"node_id"`
	RegisteredAt  time.Time         `json:"registered_at"`
	PolicySet     []string          `json:"policy_set"`
	Intervals     map[string]int64  `json:"intervals"`
	Metadata      map[string]string `json:"metadata"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
}

// NewRegistrar constructs a Registrar.
func NewRegistrar(client *api.Client, log *zap.Logger) *Registrar {
	return &Registrar{client: client, log: log}
}

// Register loads persisted state if present, otherwise performs remote registration and saves state.
func (r *Registrar) Register(ctx context.Context, req *RegisterRequest, stateFile string) (*State, error) {
	if stateFile != "" {
		if cached, err := loadState(stateFile); err == nil && cached.NodeID != "" {
			r.log.Info("node already registered", zap.String("node_id", cached.NodeID))
			return cached, nil
		}
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}

	resp, err := r.client.Do(ctx, httpMethodPost, "/api/v1/register", payload)
	if err != nil {
		return nil, fmt.Errorf("registration request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registration failed: status %d", resp.StatusCode)
	}

	var regResp RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return nil, fmt.Errorf("decode registration response: %w", err)
	}

	r.log.Info("node registered", zap.String("node_id", regResp.NodeID))

	state := &State{
		NodeID:       regResp.NodeID,
		RegisteredAt: time.Now().UTC(),
		PolicySet:    regResp.PolicySet,
		Intervals:    regResp.Intervals,
		Metadata:     regResp.Metadata,
	}

	if stateFile != "" {
		if err := saveState(stateFile, state); err != nil {
			return nil, err
		}
	}

	return state, nil
}

const (
	httpMethodPost = "POST"
)

func loadState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func saveState(path string, state *State) error {
	if path == "" {
		return errors.New("state path empty")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}
