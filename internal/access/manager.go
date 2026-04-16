package access

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

type ProviderType string

const (
	ProviderLocal ProviderType = "local"
	ProviderAD    ProviderType = "active_directory"
	ProviderAPI   ProviderType = "api"
)

type Options struct {
	Provider     ProviderType
	SyncInterval time.Duration
	DefaultRole  string
	APIEndpoint  string
	NodeID       string
}

type Manager struct {
	log      *zap.Logger
	client   *api.Client
	opts     Options
	mu       sync.RWMutex
	groups   map[string][]string
	users    map[string]string
	roles    map[string]string
	lastSync time.Time
}

func NewManager(log *zap.Logger, client *api.Client, opts Options) *Manager {
	return &Manager{
		log:    log,
		client: client,
		opts:   opts,
		groups: make(map[string][]string),
		users:  make(map[string]string),
		roles:  make(map[string]string),
	}
}

func (m *Manager) Sync(ctx context.Context) error {
	if m.opts.SyncInterval <= 0 {
		m.opts.SyncInterval = 30 * time.Minute
	}

	m.log.Info("synchronizing access control", zap.String("provider", string(m.opts.Provider)))

	if m.client == nil {
		m.mu.Lock()
		m.lastSync = time.Now().UTC()
		m.mu.Unlock()
		m.log.Warn("access client unavailable; skipping remote sync")
		return nil
	}

	payload := map[string]any{
		"provider":     m.opts.Provider,
		"default_role": m.opts.DefaultRole,
		"node_id":      m.opts.NodeID,
	}
	if m.opts.APIEndpoint != "" {
		payload["api_endpoint"] = m.opts.APIEndpoint
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal access sync payload: %w", err)
	}

	resp, err := m.client.Do(ctx, "POST", "/api/v1/access/sync", body)
	if err != nil {
		return fmt.Errorf("access sync request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("access sync rejected: status %d", resp.StatusCode)
	}

	var data struct {
		SyncedAt time.Time `json:"synced_at"`
		Users    []struct {
			ID     string   `json:"id"`
			Role   string   `json:"role"`
			Groups []string `json:"groups"`
		} `json:"users"`
		Groups []struct {
			Name    string   `json:"name"`
			Members []string `json:"members"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("decode access sync response: %w", err)
	}

	if data.SyncedAt.IsZero() {
		data.SyncedAt = time.Now().UTC()
	}

	userRoles := make(map[string]string, len(data.Users))
	userGroups := make(map[string]string, len(data.Users))
	groupMembers := make(map[string][]string, len(data.Groups))

	for _, grp := range data.Groups {
		membersCopy := make([]string, len(grp.Members))
		copy(membersCopy, grp.Members)
		groupMembers[grp.Name] = membersCopy
	}

	for _, user := range data.Users {
		role := user.Role
		if role == "" {
			role = m.opts.DefaultRole
		}
		userRoles[user.ID] = role
		if len(user.Groups) > 0 {
			userGroups[user.ID] = user.Groups[0]
		}
	}

	m.mu.Lock()
	m.roles = userRoles
	m.users = userGroups
	m.groups = groupMembers
	m.lastSync = data.SyncedAt
	m.mu.Unlock()

	return nil
}

func (m *Manager) LastSync() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastSync
}

func (m *Manager) Users() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.users))
	for k, v := range m.users {
		out[k] = v
	}
	return out
}

func (m *Manager) Roles() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.roles))
	for k, v := range m.roles {
		out[k] = v
	}
	return out
}

func (m *Manager) Groups() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]string, len(m.groups))
	for name, members := range m.groups {
		copyMembers := make([]string, len(members))
		copy(copyMembers, members)
		out[name] = copyMembers
	}
	return out
}
