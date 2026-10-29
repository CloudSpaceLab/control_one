// Package packageinventory defines the authenticated agent's complete native
// package-manager snapshot contract. Completeness is scoped, never host-wide.
package packageinventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

type Item struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	Arch        string `json:"arch,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
}

type Snapshot struct {
	Schema     int       `json:"schema"`
	Scope      string    `json:"scope"`
	Complete   bool      `json:"complete"`
	ObservedAt time.Time `json:"observed_at"`
	SHA256     string    `json:"sha256"`
	Count      int       `json:"count"`
}

func Digest(items []Item) string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		// Installation timestamps are optional annotations, not package identity.
		row, _ := json.Marshal([]string{item.Name, item.Version, item.Source, item.Arch})
		rows = append(rows, string(row))
	}
	sort.Strings(rows)
	raw, _ := json.Marshal(rows)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s Snapshot) Validate(items []Item, now time.Time) error {
	if s.Schema != 1 || !s.Complete || (s.Scope != "dpkg" && s.Scope != "rpm") || s.Count != len(items) || s.ObservedAt.IsZero() || s.ObservedAt.After(now) || now.Sub(s.ObservedAt) > 24*time.Hour || s.SHA256 != Digest(items) {
		return errors.New("invalid, incomplete or stale native package snapshot")
	}
	seen := map[string]bool{}
	for _, p := range items {
		if p.Source != s.Scope || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Version) == "" || strings.TrimSpace(p.Arch) == "" {
			return errors.New("snapshot contains invalid package identity or mixed scope")
		}
		key := p.Name + "\x00" + p.Arch
		// RPM may co-install versions. Dpkg cannot co-install two versions of
		// the same binary package/architecture; neither permits duplicate rows.
		if s.Scope == "rpm" {
			key += "\x00" + p.Version
		}
		if seen[key] {
			return errors.New("snapshot contains duplicate package identity")
		}
		seen[key] = true
	}
	return nil
}
