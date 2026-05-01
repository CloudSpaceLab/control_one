package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// FirewallRule is one platform-specific rule in a node's firewall state.
type FirewallRule struct {
	Action    string `json:"action,omitempty"`    // allow | deny | reject
	Direction string `json:"direction,omitempty"` // in | out
	Protocol  string `json:"protocol,omitempty"`  // tcp | udp | any
	Port      string `json:"port,omitempty"`      // "22" | "80,443" | "1000:2000"
	Source    string `json:"source,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Raw       string `json:"raw,omitempty"`
}

// FirewallZone is a firewalld zone snapshot.
type FirewallZone struct {
	Name       string   `json:"name"`
	Default    bool     `json:"default,omitempty"`
	Interfaces []string `json:"interfaces,omitempty"`
	Sources    []string `json:"sources,omitempty"`
	Services   []string `json:"services,omitempty"`
}

// NodeFirewallState is the per-node firewall snapshot — one row per node.
type NodeFirewallState struct {
	NodeID       uuid.UUID
	FirewallType string // ufw | firewalld | iptables | nftables | windows_defender_firewall | none
	Enabled      bool
	Rules        []FirewallRule
	Zones        []FirewallZone
	Raw          map[string]any
	ObservedAt   time.Time
}

// UpsertNodeFirewallState records the latest firewall snapshot for a node.
// Always sent in full by the agent (no delta), so this is a simple upsert.
func (s *Store) UpsertNodeFirewallState(ctx context.Context, st NodeFirewallState) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if st.NodeID == uuid.Nil {
		return errors.New("node id is required")
	}
	if st.FirewallType == "" {
		st.FirewallType = "none"
	}
	if st.ObservedAt.IsZero() {
		st.ObservedAt = time.Now().UTC()
	}

	rules, err := json.Marshal(orEmpty(st.Rules))
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	zones, err := json.Marshal(orEmptyZones(st.Zones))
	if err != nil {
		return fmt.Errorf("marshal zones: %w", err)
	}
	raw := []byte("{}")
	if len(st.Raw) > 0 {
		raw, err = json.Marshal(st.Raw)
		if err != nil {
			return fmt.Errorf("marshal raw: %w", err)
		}
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO node_firewall_state (node_id, firewall_type, enabled, rules, zones, raw, observed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (node_id) DO UPDATE SET
			firewall_type = EXCLUDED.firewall_type,
			enabled       = EXCLUDED.enabled,
			rules         = EXCLUDED.rules,
			zones         = EXCLUDED.zones,
			raw           = EXCLUDED.raw,
			observed_at   = EXCLUDED.observed_at
	`, st.NodeID, st.FirewallType, st.Enabled, rules, zones, raw, st.ObservedAt)
	if err != nil {
		return fmt.Errorf("upsert node firewall state: %w", err)
	}
	return nil
}

// GetNodeFirewallState returns the latest snapshot for a node, or nil if none
// has been reported yet.
func (s *Store) GetNodeFirewallState(ctx context.Context, nodeID uuid.UUID) (*NodeFirewallState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var (
		st         NodeFirewallState
		rulesJSON  []byte
		zonesJSON  []byte
		rawJSON    []byte
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT node_id, firewall_type, enabled, rules, zones, raw, observed_at
		FROM node_firewall_state
		WHERE node_id = $1
	`, nodeID).Scan(&st.NodeID, &st.FirewallType, &st.Enabled, &rulesJSON, &zonesJSON, &rawJSON, &st.ObservedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get node firewall state: %w", err)
	}
	if len(rulesJSON) > 0 {
		if err := json.Unmarshal(rulesJSON, &st.Rules); err != nil {
			return nil, fmt.Errorf("unmarshal rules: %w", err)
		}
	}
	if len(zonesJSON) > 0 {
		if err := json.Unmarshal(zonesJSON, &st.Zones); err != nil {
			return nil, fmt.Errorf("unmarshal zones: %w", err)
		}
	}
	if len(rawJSON) > 0 {
		if err := json.Unmarshal(rawJSON, &st.Raw); err != nil {
			return nil, fmt.Errorf("unmarshal raw: %w", err)
		}
	}
	return &st, nil
}

func orEmpty(rules []FirewallRule) []FirewallRule {
	if rules == nil {
		return []FirewallRule{}
	}
	return rules
}

func orEmptyZones(zones []FirewallZone) []FirewallZone {
	if zones == nil {
		return []FirewallZone{}
	}
	return zones
}
