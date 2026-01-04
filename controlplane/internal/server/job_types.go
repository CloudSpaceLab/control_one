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
}
