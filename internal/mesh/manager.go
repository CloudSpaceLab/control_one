package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

type Peer struct {
	ID         string    `json:"id"`
	PublicKey  string    `json:"public_key"`
	Endpoint   string    `json:"endpoint"`
	AllowedIPs []string  `json:"allowed_ips"`
	LastSeen   time.Time `json:"last_seen"`
}

type State struct {
	NodeID     string    `json:"node_id"`
	Namespace  string    `json:"namespace"`
	PrivateKey string    `json:"private_key"`
	Peers      []Peer    `json:"peers"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastRotate time.Time `json:"last_rotate"`
}

type Options struct {
	Enabled        bool
	CoordinatorURL string
	AuthToken      string
	Namespace      string
	PrivateCIDR    string
	RelayNodes     []string
	StateFile      string
	PollInterval   time.Duration
	KeyRotation    time.Duration
	NodeID         string
}

type Manager struct {
	log           *zap.Logger
	client        *api.Client
	opts          Options
	stateLock     sync.RWMutex
	state         State
	lastKeyRotate time.Time
	lastPeerSync  time.Time
}

func New(log *zap.Logger, client *api.Client, opts Options) *Manager {
	return &Manager{log: log, client: client, opts: opts}
}

func (m *Manager) EnsureState() error {
	if !m.opts.Enabled {
		return nil
	}
	if strings.TrimSpace(m.opts.StateFile) == "" {
		return errors.New("mesh state file required when mesh is enabled")
	}
	if err := os.MkdirAll(filepath.Dir(m.opts.StateFile), 0o750); err != nil {
		return fmt.Errorf("create mesh state dir: %w", err)
	}
	if err := m.loadState(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.stateLock.Lock()
			m.state = State{
				NodeID:    m.opts.NodeID,
				Namespace: m.opts.Namespace,
				UpdatedAt: time.Now().UTC(),
			}
			err = m.saveStateLocked()
			m.stateLock.Unlock()
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	m.stateLock.RLock()
	m.lastKeyRotate = m.state.LastRotate
	if !m.state.UpdatedAt.IsZero() {
		m.lastPeerSync = m.state.UpdatedAt
	}
	m.stateLock.RUnlock()
	return nil
}

func (m *Manager) Start(ctx context.Context) {
	if !m.opts.Enabled {
		m.log.Info("mesh manager disabled")
		return
	}
	if m.opts.PollInterval <= 0 {
		m.log.Warn("mesh poll interval invalid, using default 5m", zap.Duration("interval", m.opts.PollInterval))
		m.opts.PollInterval = 5 * time.Minute
	}

	go m.run(ctx)
}

func (m *Manager) SyncOnce(ctx context.Context) error {
	if !m.opts.Enabled {
		return nil
	}

	m.log.Info("mesh sync", zap.String("namespace", m.opts.Namespace))
	if m.client == nil {
		m.stateLock.Lock()
		m.state.UpdatedAt = time.Now().UTC()
		m.lastPeerSync = m.state.UpdatedAt
		err := m.saveStateLocked()
		m.stateLock.Unlock()
		return err
	}

	query := url.Values{}
	if m.opts.Namespace != "" {
		query.Set("namespace", m.opts.Namespace)
	}
	if m.opts.NodeID != "" {
		query.Set("node_id", m.opts.NodeID)
	}
	path := "/api/v1/mesh/peers"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	resp, err := m.client.Do(ctx, "GET", path, nil)
	if err != nil {
		return fmt.Errorf("mesh peer sync request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mesh peer sync failed: status %d", resp.StatusCode)
	}

	var payload struct {
		Peers      []Peer    `json:"peers"`
		PrivateKey string    `json:"private_key"`
		UpdatedAt  time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode mesh peers: %w", err)
	}

	m.stateLock.Lock()
	m.state.Peers = payload.Peers
	if payload.PrivateKey != "" {
		m.state.PrivateKey = payload.PrivateKey
		m.lastKeyRotate = time.Now().UTC()
		m.state.LastRotate = m.lastKeyRotate
	}
	if payload.UpdatedAt.IsZero() {
		payload.UpdatedAt = time.Now().UTC()
	}
	m.state.UpdatedAt = payload.UpdatedAt
	m.lastPeerSync = payload.UpdatedAt
	err = m.saveStateLocked()
	m.stateLock.Unlock()
	return err
}

func (m *Manager) run(ctx context.Context) {
	ticker := time.NewTicker(m.opts.PollInterval)
	defer ticker.Stop()

	m.log.Info("mesh manager started", zap.Duration("interval", m.opts.PollInterval))

	if err := m.SyncOnce(ctx); err != nil {
		m.log.Warn("initial mesh sync failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			m.log.Info("mesh manager stopping")
			return
		case <-ticker.C:
			if err := m.SyncOnce(ctx); err != nil {
				m.log.Warn("mesh sync failed", zap.Error(err))
			}
			m.rotateIfNeeded(ctx)
		}
	}
}

func (m *Manager) rotateIfNeeded(ctx context.Context) {
	if m.opts.KeyRotation <= 0 {
		return
	}

	m.stateLock.Lock()
	nextRotation := m.lastKeyRotate.Add(m.opts.KeyRotation)
	m.stateLock.Unlock()

	if time.Now().Before(nextRotation) {
		return
	}

	if m.client == nil {
		m.stateLock.Lock()
		m.lastKeyRotate = time.Now().UTC()
		m.state.LastRotate = m.lastKeyRotate
		err := m.saveStateLocked()
		m.stateLock.Unlock()
		if err != nil {
			m.log.Warn("persist mesh state", zap.Error(err))
		}
		return
	}

	payload := map[string]any{
		"node_id":   m.opts.NodeID,
		"namespace": m.opts.Namespace,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		m.log.Warn("marshal mesh rotation payload", zap.Error(err))
		return
	}

	resp, err := m.client.Do(ctx, "POST", "/api/v1/mesh/rotate", body)
	if err != nil {
		m.log.Warn("mesh rotation request failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		m.log.Warn("mesh rotation rejected", zap.Int("status", resp.StatusCode))
		return
	}

	var rotateResp struct {
		PrivateKey string    `json:"private_key"`
		RotatedAt  time.Time `json:"rotated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rotateResp); err != nil {
		m.log.Warn("decode rotation response", zap.Error(err))
		return
	}

	if rotateResp.RotatedAt.IsZero() {
		rotateResp.RotatedAt = time.Now().UTC()
	}

	m.stateLock.Lock()
	if rotateResp.PrivateKey != "" {
		m.state.PrivateKey = rotateResp.PrivateKey
	}
	m.lastKeyRotate = rotateResp.RotatedAt
	m.state.LastRotate = rotateResp.RotatedAt
	if err := m.saveStateLocked(); err != nil {
		m.log.Warn("persist mesh state", zap.Error(err))
	}
	m.stateLock.Unlock()
}

func (m *Manager) loadState() error {
	data, err := os.ReadFile(m.opts.StateFile)
	if err != nil {
		return err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("unmarshal mesh state: %w", err)
	}
	m.stateLock.Lock()
	m.state = st
	m.stateLock.Unlock()
	return nil
}

func (m *Manager) saveState() error {
	m.stateLock.Lock()
	defer m.stateLock.Unlock()
	return m.saveStateLocked()
}

func (m *Manager) saveStateLocked() error {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mesh state: %w", err)
	}
	tmp := m.opts.StateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write mesh state tmp: %w", err)
	}
	return os.Rename(tmp, m.opts.StateFile)
}
