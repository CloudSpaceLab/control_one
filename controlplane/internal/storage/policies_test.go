package storage

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAssignmentScope(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	clusterID := uuid.New()

	scopeType, scopeID, legacyNodeID, selector, err := normalizeAssignmentScope("", uuid.Nil, uuid.Nil, nil)
	require.NoError(t, err)
	require.Equal(t, AssignmentScopeTenant, scopeType)
	require.Equal(t, uuid.Nil, scopeID)
	require.Equal(t, uuid.Nil, legacyNodeID)
	require.Empty(t, selector)

	scopeType, scopeID, legacyNodeID, selector, err = normalizeAssignmentScope("", uuid.Nil, nodeID, nil)
	require.NoError(t, err)
	require.Equal(t, AssignmentScopeNode, scopeType)
	require.Equal(t, nodeID, scopeID)
	require.Equal(t, nodeID, legacyNodeID)
	require.Empty(t, selector)

	scopeType, scopeID, legacyNodeID, selector, err = normalizeAssignmentScope(AssignmentScopeLabelSelector, uuid.Nil, uuid.Nil, map[string]any{
		"agent.primary_purpose": " db_node ",
		"empty":                 "",
	})
	require.NoError(t, err)
	require.Equal(t, AssignmentScopeLabelSelector, scopeType)
	require.Equal(t, uuid.Nil, scopeID)
	require.Equal(t, uuid.Nil, legacyNodeID)
	require.Equal(t, map[string]any{"agent.primary_purpose": "db_node"}, selector)

	scopeType, scopeID, legacyNodeID, selector, err = normalizeAssignmentScope(AssignmentScopeCluster, clusterID, uuid.Nil, nil)
	require.NoError(t, err)
	require.Equal(t, AssignmentScopeCluster, scopeType)
	require.Equal(t, clusterID, scopeID)
	require.Equal(t, uuid.Nil, legacyNodeID)
	require.Empty(t, selector)

	_, _, _, _, err = normalizeAssignmentScope(AssignmentScopeTenant, uuid.Nil, nodeID, nil)
	require.Error(t, err)

	_, _, _, _, err = normalizeAssignmentScope(AssignmentScopeLabelSelector, uuid.Nil, uuid.Nil, nil)
	require.Error(t, err)
}
