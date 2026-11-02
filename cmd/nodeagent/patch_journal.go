package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

// Each job is reserved durably before starting. Interrupted commands are never
// automatically retried: their package-manager outcome requires reconciliation.
type patchJournalEntry struct {
	Action       string           `json:"action"`
	Result       *completedAction `json:"result,omitempty"`
	Acknowledged bool             `json:"acknowledged,omitempty"`
}

type patchJournal struct {
	mu           sync.Mutex
	path         string
	entries      map[string]patchJournalEntry
	dirty        bool               // Results cannot be reported until the latest snapshot persists.
	persistFault func(string) error // Per-instance fault seam; nil in production.
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
	// Validate the whole file before recovery rewrites any entry. Parseable
	// JSON is insufficient evidence of a consistent execution reservation.
	for id, entry := range j.entries {
		if err := validatePatchJournalEntry(id, entry); err != nil {
			return nil, fmt.Errorf("invalid patch journal entry %q: %w", id, err)
		}
	}
	for id, entry := range j.entries {
		if entry.Result == nil {
			entry.Result = &completedAction{Action: entry.Action, JobID: id, Status: "failed", Error: "agent restarted during reserved patch execution; outcome unknown, manual reconciliation required"}
			if entry.Action == "patch.prepare_offline" || entry.Action == patchsupport.PrepareOnlineJob {
				entry.Result.Error = "preparation interrupted; no installation authorized; inspect staged content and submit a fresh preparation request"
			}
			if patchsupport.IsProxyLifecycle(entry.Action) {
				entry.Result.Error = "agent restarted during reserved proxy action; service/configuration outcome unknown; inspect target and submit a fresh authorized request, never replay"
			}
			j.entries[id] = entry
		}
	}
	if err = j.persist(); err != nil {
		return nil, err
	}
	return j, nil
}

func validatePatchJournalEntry(id string, entry patchJournalEntry) error {
	if strings.TrimSpace(id) == "" || !isDurablePatchAction(entry.Action) {
		return errors.New("patch reservation identity required")
	}
	if entry.Result == nil {
		if entry.Acknowledged {
			return errors.New("acknowledgment requires a terminal result")
		}
		return nil
	}
	if entry.Result.JobID != id || entry.Result.Action != entry.Action {
		return errors.New("patch result does not match reservation")
	}
	if entry.Result.Status != "succeeded" && entry.Result.Status != "failed" {
		return errors.New("patch result must be terminal")
	}
	return nil
}

func (j *patchJournal) persistStage(stage string) error {
	if j.persistFault != nil {
		return j.persistFault(stage)
	}
	return nil
}

func (j *patchJournal) persist() error {
	j.dirty = true
	if err := j.persistStage("mkdir"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(j.entries)
	if err != nil {
		return err
	}
	if err = j.persistStage("create"); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(j.path), ".patch-journal-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = j.persistStage("chmod"); err == nil {
		err = f.Chmod(0600)
	}
	if err == nil {
		err = j.persistStage("write")
	}
	if err == nil {
		var n int
		n, err = f.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = j.persistStage("sync")
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if closeErr == nil {
		closeErr = j.persistStage("close")
	}
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = j.persistStage("rename"); err != nil {
		return err
	}
	if err = os.Rename(name, j.path); err != nil {
		return err
	}
	if err = j.persistStage("dirsync"); err != nil {
		return err
	}
	if err = syncPatchJournalDirectory(filepath.Dir(j.path)); err != nil {
		return err
	}
	j.dirty = false
	return nil
}
func (j *patchJournal) reserve(id, action string) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := validatePatchJournalEntry(id, patchJournalEntry{Action: action}); err != nil {
		return false, err
	}
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
	if err := validatePatchJournalEntry(result.JobID, entry); err != nil {
		return err
	}
	j.entries[result.JobID] = entry
	return j.persist()
}

func (j *patchJournal) pending() []completedAction {
	j.mu.Lock()
	defer j.mu.Unlock()
	// Retry a failed completion/ack snapshot before exposing any result.
	if j.dirty {
		if err := j.persist(); err != nil {
			return nil
		}
	}
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
	if j.dirty {
		if err := j.persist(); err != nil {
			return err
		}
	}
	previous := make(map[string]patchJournalEntry)
	for _, id := range ids {
		entry, ok := j.entries[id]
		if ok && entry.Result != nil && !entry.Acknowledged {
			previous[id] = entry
			entry.Acknowledged = true
			j.entries[id] = entry
		}
	}
	if err := j.persist(); err != nil {
		for id, entry := range previous {
			j.entries[id] = entry
		}
		return err
	}
	return nil
}

func isPatchDeploymentAction(action string) bool {
	return action == patchJobDirect || action == patchJobProxy || action == patchJobAirgapped
}

func isDurablePatchAction(action string) bool {
	return isPatchDeploymentAction(action) || action == "patch.prepare_offline" || action == patchsupport.PrepareOnlineJob || patchsupport.IsProxyLifecycle(action)
}
