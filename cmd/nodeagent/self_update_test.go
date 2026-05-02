package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRolloutBucketDeterministic(t *testing.T) {
	// Same id → same bucket every time. Spot-check a handful of UUIDs and
	// confirm idempotence; the math is crc32 mod 100 so this is a sanity
	// check, not an exhaustive distribution test.
	cases := []string{
		"00000000-0000-0000-0000-000000000001",
		"deadbeef-cafe-babe-face-1234567890ab",
		"f47ac10b-58cc-4372-a567-0e02b2c3d479",
	}
	for _, id := range cases {
		first := rolloutBucket(id)
		if first < 0 || first >= 100 {
			t.Fatalf("bucket out of [0,100) for %q: %d", id, first)
		}
		for i := 0; i < 5; i++ {
			if got := rolloutBucket(id); got != first {
				t.Fatalf("bucket nondeterministic for %q: %d != %d", id, got, first)
			}
		}
	}
}

func TestRolloutBucketCaseInsensitive(t *testing.T) {
	// UUIDs from different sources may differ in case; the bucket must be
	// stable so an operator who edits the row by hand can't accidentally
	// shift a node into a different wave by casing alone.
	a := rolloutBucket("DEADBEEF-CAFE-BABE-FACE-1234567890AB")
	b := rolloutBucket("deadbeef-cafe-babe-face-1234567890ab")
	if a != b {
		t.Fatalf("bucket varies with case: %d vs %d", a, b)
	}
}

func TestRolloutBucketEmptyFailsClosed(t *testing.T) {
	// Empty id → bucket 100, which is >= every rollout_pct in [0, 100],
	// so shouldUpdate always rejects. Belt-and-braces against a misconfigured
	// agent landing in a wave it shouldn't.
	if got := rolloutBucket(""); got != 100 {
		t.Fatalf("expected fail-closed bucket=100, got %d", got)
	}
}

func TestShouldUpdateRejections(t *testing.T) {
	// Bucket 10 + a valid manifest is the green-path baseline; the table
	// below mutates one field at a time and asserts the matching reject
	// reason fires.
	type tc struct {
		name       string
		manifest   updateManifest
		current    int
		bucket     int
		wantEmpty  bool
		wantSubstr string
	}
	cases := []tc{
		{
			name:      "green path",
			manifest:  updateManifest{ReleaseSeq: 5, RolloutPct: 25},
			current:   3,
			bucket:    10,
			wantEmpty: true,
		},
		{
			name:       "paused brakes everything",
			manifest:   updateManifest{ReleaseSeq: 5, RolloutPct: 100, Paused: true},
			current:    0,
			bucket:     0,
			wantSubstr: "paused",
		},
		{
			name:       "no rollout configured",
			manifest:   updateManifest{ReleaseSeq: 0, RolloutPct: 100},
			current:    0,
			bucket:     0,
			wantSubstr: "release_seq=0",
		},
		{
			name:       "downgrade refused",
			manifest:   updateManifest{ReleaseSeq: 3, RolloutPct: 100},
			current:    5,
			bucket:     0,
			wantSubstr: "downgrade",
		},
		{
			name:       "equal seq is downgrade",
			manifest:   updateManifest{ReleaseSeq: 5, RolloutPct: 100},
			current:    5,
			bucket:     0,
			wantSubstr: "downgrade",
		},
		{
			name:       "outside wave",
			manifest:   updateManifest{ReleaseSeq: 5, RolloutPct: 25},
			current:    3,
			bucket:     50,
			wantSubstr: "outside current rollout wave",
		},
		{
			name:       "boundary: bucket equal to pct rejected",
			manifest:   updateManifest{ReleaseSeq: 5, RolloutPct: 25},
			current:    3,
			bucket:     25,
			wantSubstr: "outside current rollout wave",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldUpdate(c.manifest, c.current, c.bucket)
			if c.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty (proceed), got reject %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected reject containing %q, got proceed", c.wantSubstr)
			}
			if !contains(got, c.wantSubstr) {
				t.Fatalf("reject reason %q does not contain %q", got, c.wantSubstr)
			}
		})
	}
}

func TestSaveAndLoadAgentState(t *testing.T) {
	dir := t.TempDir()

	// Write a state.json by hand with a non-rollout key, then have the agent
	// overlay current_release_seq via the helpers and round-trip. The
	// pre-existing key must survive — other consumers (rotate_cert.go etc.)
	// rely on us not clobbering their keys.
	initial := map[string]any{
		"node_id": "abc-123",
	}
	body, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("marshal initial: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), body, 0o600); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	loaded := loadAgentState(dir)
	if loaded["node_id"] != "abc-123" {
		t.Fatalf("did not preserve node_id; got %#v", loaded)
	}

	loaded["current_release_seq"] = 7
	if err := saveAgentState(dir, loaded); err != nil {
		t.Fatalf("save: %v", err)
	}

	again := loadAgentState(dir)
	if again["node_id"] != "abc-123" {
		t.Fatalf("node_id lost after save; got %#v", again)
	}
	if got := currentReleaseSeqFromState(again); got != 7 {
		t.Fatalf("current_release_seq round-trip: got %d want 7", got)
	}
}

func TestCurrentReleaseSeqFromState(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want int
	}{
		{"absent", map[string]any{}, 0},
		{"json-number-via-decode", mustDecodeJSON(`{"current_release_seq": 12}`), 12},
		{"int", map[string]any{"current_release_seq": 5}, 5},
		{"int64", map[string]any{"current_release_seq": int64(9)}, 9},
		{"string ignored", map[string]any{"current_release_seq": "42"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := currentReleaseSeqFromState(c.in); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func mustDecodeJSON(s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}
