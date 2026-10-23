package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Each job is reserved durably before starting. Interrupted commands are never
// automatically retried: their package-manager outcome requires reconciliation.
type patchJournalEntry struct {
	Action       string           `json:"action"`
	Result       *completedAction `json:"result,omitempty"`
	Acknowledged bool             `json:"acknowledged,omitempty"`
}

type patchJournal struct {
	mu      sync.Mutex
	path    string
	entries map[string]patchJournalEntry
}

var agentPatchJournal *patchJournal
var patchAgentNodeID string
var patchExecutionMu sync.Mutex

const maxPatchJournalEntries = 4096

func openPatchJournal(path string) (*patchJournal, error) {
	j := &patchJournal{path: path, entries: map[string]patchJournalEntry{}}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal(data, &j.entries); err != nil {
			return nil, fmt.Errorf("invalid patch execution journal: %w", err)
		}
		if j.entries == nil {
			return nil, errors.New("patch journal must be an object")
		}
	}
	for id, entry := range j.entries {
		if entry.Result == nil {
			entry.Result = &completedAction{Action: entry.Action, JobID: id, Status: "failed", Error: "agent restarted during reserved patch execution; outcome unknown, manual reconciliation required"}
			j.entries[id] = entry
		}
	}
	if err = j.persist(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *patchJournal) persist() error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(j.entries)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(j.path), ".patch-journal-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, j.path)
}

func (j *patchJournal) reserve(id, action string) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, exists := j.entries[id]; exists {
		return false, nil
	}
	if len(j.entries) >= maxPatchJournalEntries {
		return false, errors.New("patch journal capacity reached; operator reconciliation required")
	}
	j.entries[id] = patchJournalEntry{Action: action}
	if err := j.persist(); err != nil {
		delete(j.entries, id)
		return false, err
	}
	return true, nil
}

func (j *patchJournal) complete(result completedAction) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok := j.entries[result.JobID]
	if !ok || entry.Action != result.Action {
		return errors.New("patch completion has no matching reservation")
	}
	entry.Result = &result
	j.entries[result.JobID] = entry
	return j.persist()
}

func (j *patchJournal) pending() []completedAction {
	j.mu.Lock()
	defer j.mu.Unlock()
	ids := make([]string, 0, len(j.entries))
	for id := range j.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []completedAction
	for _, id := range ids {
		entry := j.entries[id]
		if entry.Result != nil && !entry.Acknowledged {
			out = append(out, *entry.Result)
			if len(out) >= 256 {
				break
			}
		}
	}
	return out
}

func (j *patchJournal) acknowledge(ids []string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, id := range ids {
		entry, ok := j.entries[id]
		if ok && entry.Result != nil {
			entry.Acknowledged = true
			j.entries[id] = entry
		}
	}
	return j.persist()
}

func isPatchDeploymentAction(action string) bool {
	return action == patchJobDirect || action == patchJobProxy || action == patchJobAirgapped
}
