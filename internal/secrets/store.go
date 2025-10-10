package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

type BackendType string

const (
	BackendMemory BackendType = "memory"
	BackendVault  BackendType = "vault"
)

type Options struct {
	Backend      BackendType
	Endpoint     string
	Groups       []string
	SyncInterval time.Duration
	NodeID       string
}

type Secret struct {
	Name      string            `json:"name"`
	Value     string            `json:"value"`
	Labels    map[string]string `json:"labels"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type Store struct {
	log      *zap.Logger
	client   *api.Client
	opts     Options
	mu       sync.RWMutex
	secrets  map[string]Secret
	lastSync time.Time
}

func NewStore(log *zap.Logger, client *api.Client, opts Options) *Store {
	return &Store{
		log:     log,
		client:  client,
		opts:    opts,
		secrets: make(map[string]Secret),
	}
}

func (s *Store) Sync(ctx context.Context) error {
	if s.opts.SyncInterval <= 0 {
		s.opts.SyncInterval = 15 * time.Minute
	}
	backend := string(s.opts.Backend)
	s.log.Info("syncing secrets", zap.String("backend", backend))

	if s.client == nil {
		s.mu.Lock()
		s.lastSync = time.Now().UTC()
		s.mu.Unlock()
		s.log.Warn("secrets client unavailable; skipping remote sync")
		return nil
	}

	payload := map[string]any{
		"backend": backend,
		"groups":  s.opts.Groups,
		"node_id": s.opts.NodeID,
	}
	if s.opts.Endpoint != "" {
		payload["endpoint"] = s.opts.Endpoint
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal secrets payload: %w", err)
	}

	resp, err := s.client.Do(ctx, "POST", "/api/v1/secrets/sync", body)
	if err != nil {
		return fmt.Errorf("secrets sync request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("secrets sync rejected: status %d", resp.StatusCode)
	}

	var data struct {
		SyncedAt time.Time `json:"synced_at"`
		Secrets  []Secret  `json:"secrets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("decode secrets response: %w", err)
	}
	if data.SyncedAt.IsZero() {
		data.SyncedAt = time.Now().UTC()
	}

	secretMap := make(map[string]Secret, len(data.Secrets))
	for _, sec := range data.Secrets {
		secretMap[sec.Name] = sec
	}

	s.mu.Lock()
	s.secrets = secretMap
	s.lastSync = data.SyncedAt
	s.mu.Unlock()

	return nil
}

func (s *Store) Get(name string) (Secret, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc, ok := s.secrets[name]
	return sc, ok
}

func (s *Store) List() []Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Secret, 0, len(s.secrets))
	for _, sc := range s.secrets {
		out = append(out, sc)
	}
	return out
}

func (s *Store) LastSync() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSync
}
