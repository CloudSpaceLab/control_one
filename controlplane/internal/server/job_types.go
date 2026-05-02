package server

import (
	"encoding/json"
	"sync"
)

type jobDefinition struct {
	RequiresTenant bool
	Validate       func(json.RawMessage) (any, error)
}

var (
	jobDefinitionsMu sync.RWMutex
	jobDefinitions   = map[string]jobDefinition{}
)

func registerJobDefinition(jobType string, def jobDefinition) {
	jobDefinitionsMu.Lock()
	defer jobDefinitionsMu.Unlock()
	jobDefinitions[jobType] = def
}

func jobDefinitionForType(jobType string) (jobDefinition, bool) {
	jobDefinitionsMu.RLock()
	defer jobDefinitionsMu.RUnlock()
	def, ok := jobDefinitions[jobType]
	return def, ok
}

func init() {
	registerJobDefinition(JobTypeProvisionApply, jobDefinition{
		RequiresTenant: true,
		Validate: func(payload json.RawMessage) (any, error) {
			return decodeProvisionPayload(payload)
		},
	})
	registerJobDefinition(JobTypeComplianceScan, jobDefinition{
		RequiresTenant: true,
		Validate: func(payload json.RawMessage) (any, error) {
			return decodeCompliancePayload(payload)
		},
	})
	// Agent update jobs don't need tenant (derived from node) and have no payload validation
	registerJobDefinition(JobTypeAgentUpdate, jobDefinition{
		RequiresTenant: false,
		Validate:       nil,
	})
	// Firewall jobs (PR 3) — control-plane validates payload shape; the actual
	// dispatch + lifecycle is heartbeat-driven, not worker-loop-driven.
	registerJobDefinition(JobTypeFirewallRuleAdd, jobDefinition{
		RequiresTenant: true,
		Validate: func(payload json.RawMessage) (any, error) {
			return decodeFirewallPayload(payload)
		},
	})
	registerJobDefinition(JobTypeFirewallRuleDelete, jobDefinition{
		RequiresTenant: true,
		Validate: func(payload json.RawMessage) (any, error) {
			return decodeFirewallPayload(payload)
		},
	})
	// Patch management (PR 4) — heartbeat-driven lifecycle, same as firewall.*
	registerJobDefinition(JobTypePatchDeployDirect, jobDefinition{
		RequiresTenant: true,
		Validate: func(payload json.RawMessage) (any, error) {
			return decodePatchPayload(payload)
		},
	})
}
