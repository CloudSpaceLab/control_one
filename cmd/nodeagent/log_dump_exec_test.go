package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFilterAgentLogDumpSnapshotPreservesRawWithoutEntityFilter(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)
	raw := []byte("{\n  \"entries\": [{\"timestamp\":\"2026-10-01T08:30:00Z\",\"message\":\"one\"}],\n  \"count\": 1\n}\n")

	got, rows, err := filterAgentLogDumpSnapshot(raw, nil, since, until)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	require.Equal(t, raw, got)
}

func TestFilterAgentLogDumpSnapshotAppliesWindowAndEntity(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)
	raw := []byte(strings.Join([]string{
		`{"entries":[{"timestamp":"2026-10-01T08:05:00Z","labels":{"service":"api"},"message":"keep"},{"timestamp":"2026-10-01T08:10:00Z","labels":{"service":"db"},"message":"wrong entity"}],"count":2}`,
		`{"entries":[{"timestamp":"2026-10-01T10:00:00Z","labels":{"service":"api"},"message":"outside"}],"count":1}`,
		"",
	}, "\n"))

	got, rows, err := filterAgentLogDumpSnapshot(raw, map[string]any{"service": "api"}, since, until)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	require.Contains(t, string(got), "\"message\":\"keep\"")
	require.NotContains(t, string(got), "wrong entity")
	require.NotContains(t, string(got), "outside")
}

func TestAgentLogDumpEntryInWindowRejectsMissingAndMalformedTimestamp(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)
	require.False(t, agentLogDumpEntryInWindow(map[string]any{}, since, until))
	require.False(t, agentLogDumpEntryInWindow(map[string]any{"timestamp": "bad"}, since, until))
	require.True(t, agentLogDumpEntryInWindow(map[string]any{"timestamp": since.Format(time.RFC3339Nano)}, since, until))
	require.True(t, agentLogDumpEntryInWindow(map[string]any{"timestamp": until.Format(time.RFC3339Nano)}, since, until))
}
