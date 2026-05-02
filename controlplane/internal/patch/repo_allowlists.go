// Package patch holds curated repository allow-lists used by the Wave C
// patch-management completion. The list itself is hand-maintained in
// repo_allowlists.json — embed it at compile time so the controlplane never
// synthesises hostnames at runtime.
package patch

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed repo_allowlists.json
var rawRepoAllowlists []byte

// RepoAllowlists is the parsed structure: os -> version -> []hostname.
type RepoAllowlists map[string]map[string][]string

// LoadRepoAllowlists returns the parsed allow-list once and caches the result
// at first call. Errors are returned to the caller — there is no silent
// fallback because that would defeat the whole point of a curated list.
func LoadRepoAllowlists() (RepoAllowlists, error) {
	out := RepoAllowlists{}
	if err := json.Unmarshal(rawRepoAllowlists, &out); err != nil {
		return nil, fmt.Errorf("decode repo allowlists: %w", err)
	}
	return out, nil
}

// HostsFor returns the curated hostnames for the given OS / version pair.
// Lookup is case-insensitive on the os key and exact on the version key —
// callers should pass values like ("ubuntu","24.04") or ("windows","server-2022").
// Returns an empty slice when no entry matches; the caller is expected to
// surface the empty case as a configuration error rather than treating it as
// "allow everything".
func (r RepoAllowlists) HostsFor(os, version string) []string {
	if r == nil {
		return nil
	}
	osKey := strings.ToLower(strings.TrimSpace(os))
	verKey := strings.TrimSpace(version)
	if osKey == "" || verKey == "" {
		return nil
	}
	versions, ok := r[osKey]
	if !ok {
		return nil
	}
	hosts, ok := versions[verKey]
	if !ok {
		return nil
	}
	out := make([]string, len(hosts))
	copy(out, hosts)
	return out
}
