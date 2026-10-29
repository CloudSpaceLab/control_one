package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPatchJournalRejectsInconsistentEvidence(t *testing.T) {
	for name, data := range map[string]string{
		"empty-job":      `{"":{"action":"patch.deploy_direct"}}`,
		"unknown-action": `{"job":{"action":"patch.inventory_scan"}}`,
		"ack-no-result":  `{"job":{"action":"patch.deploy_direct","acknowledged":true}}`,
		"wrong-job":      `{"job":{"action":"patch.deploy_direct","result":{"action":"patch.deploy_direct","job_id":"other","status":"succeeded"}}}`,
		"wrong-action":   `{"job":{"action":"patch.deploy_direct","result":{"action":"patch.deploy_proxy","job_id":"job","status":"succeeded"}}}`,
		"nonterminal":    `{"job":{"action":"patch.deploy_direct","result":{"action":"patch.deploy_direct","job_id":"job","status":"running"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "patch.json")
			original := []byte(data)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := openPatchJournal(path); err == nil {
				t.Fatal("inconsistent execution evidence accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatal("rejected evidence must remain unchanged for reconciliation")
			}
		})
	}
}

func TestPatchJournalAcknowledgmentWriteFailureRetainsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patch.json")
	j, err := openPatchJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := j.reserve("job", patchJobDirect); err != nil || !ok {
		t.Fatalf("reserve: %v", err)
	}
	if err := j.complete(completedAction{Action: patchJobDirect, JobID: "job", Status: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	// Force atomic replacement to fail without changing permissions or live files.
	blocked := filepath.Join(filepath.Dir(path), "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	j.path = blocked
	if err := j.acknowledge([]string{"job", "job"}); err == nil {
		t.Fatal("expected acknowledgment persistence failure")
	}
	if j.entries["job"].Acknowledged || j.entries["job"].Result == nil {
		t.Fatal("failed acknowledgment write discarded the retained unacknowledged result")
	}
	if len(j.pending()) != 0 {
		t.Fatal("a snapshot that cannot persist must not be reported")
	}
	j.path = path
	if len(j.pending()) != 1 {
		t.Fatal("persisted retry did not restore the pending report")
	}
	reopened, err := openPatchJournal(path)
	if err != nil || len(reopened.pending()) != 1 {
		t.Fatalf("durable evidence was not retained: %v", err)
	}
	if err := j.acknowledge([]string{"job"}); err != nil {
		t.Fatal(err)
	}
	reopened, err = openPatchJournal(path)
	if err != nil || len(reopened.pending()) != 0 {
		t.Fatalf("acknowledgment retry did not persist: %v", err)
	}
	if ok, err := reopened.reserve("job", patchJobDirect); err != nil || ok {
		t.Fatal("acknowledged work must remain reserved against re-execution")
	}
}

// Qualification reads a supplied snapshot but only opens/writes a temporary
// clone. It must never normalize or recover the enrolled agent's live journal.
func TestPatchJournalUpgradeCompatibility(t *testing.T) {
	snapshot := os.Getenv("PATCH_JOURNAL_COMPATIBILITY_FILE")
	if snapshot == "" {
		t.Skip("set PATCH_JOURNAL_COMPATIBILITY_FILE to a pre-upgrade journal snapshot")
	}
	original, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "patch.json")
	if err := os.WriteFile(clone, original, 0600); err != nil {
		t.Fatal(err)
	}
	j, err := openPatchJournal(clone)
	if err != nil {
		t.Fatalf("pre-upgrade journal incompatible: %v", err)
	}
	after, err := os.ReadFile(snapshot)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("qualification must not alter the supplied snapshot")
	}
	t.Logf("compatible journal: %d entries, %d pending results", len(j.entries), len(j.pending()))
}

func TestPatchJournalDuplicateAndLostAcknowledgment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patch.json")
	j, err := openPatchJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	starts := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := j.reserve("same-job", patchJobDirect)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				starts++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if starts != 1 {
		t.Fatalf("duplicate execution reservations=%d", starts)
	}
	if err = j.complete(completedAction{Action: patchJobDirect, JobID: "same-job", Status: "succeeded", Metadata: map[string]any{"packages_upgraded": 3}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if len(j.pending()) != 1 {
			t.Fatal("result lost before explicit acknowledgment")
		}
	}
	j, err = openPatchJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.pending()) != 1 {
		t.Fatal("result did not survive restart")
	}
	if err = j.acknowledge([]string{"unrelated-job"}); err != nil {
		t.Fatal(err)
	}
	if len(j.pending()) != 1 {
		t.Fatal("unrelated acknowledgment discarded result")
	}
	if err = j.acknowledge([]string{"same-job"}); err != nil {
		t.Fatal(err)
	}
	j, err = openPatchJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.pending()) != 0 {
		t.Fatal("acknowledged result still pending")
	}
	if ok, err := j.reserve("same-job", patchJobDirect); err != nil || ok {
		t.Fatal("acknowledged job re-executed")
	}
}

func TestPatchJournalInterruptedExecutionRequiresReconciliation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patch.json")
	j, err := openPatchJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := j.reserve("interrupted", patchJobDirect); err != nil || !ok {
		t.Fatal("reserve failed")
	}
	j, err = openPatchJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	results := j.pending()
	if len(results) != 1 || results[0].Status != "failed" || results[0].Error == "" {
		t.Fatalf("unsafe interrupted result: %#v", results)
	}
	if ok, _ := j.reserve("interrupted", patchJobDirect); ok {
		t.Fatal("interrupted command automatically retried")
	}
}

func TestPatchJournalRefusesCorruptionAndWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patch.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openPatchJournal(path); err == nil {
		t.Fatal("corrupted execution evidence accepted")
	}
	if _, err := openPatchJournal(filepath.Join(path, "impossible.json")); err == nil {
		t.Fatal("unwritable execution journal accepted")
	}
}

func TestPatchJournalCapacityFailsClosed(t *testing.T) {
	j, err := openPatchJournal(filepath.Join(t.TempDir(), "patch.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPatchJournalEntries; i++ {
		j.entries[fmt.Sprint(i)] = patchJournalEntry{Action: patchJobDirect, Acknowledged: true}
	}
	if reserved, err := j.reserve("new-job", patchJobDirect); reserved || err == nil {
		t.Fatal("capacity overflow must not execute or discard history")
	}
}

func TestPatchDispatchRequiresMatchingNodeAndMode(t *testing.T) {
	detail := patchActionDetail{NodeID: "node-a", DeploymentID: "deployment", NodePatchStateID: "state", Mode: "direct"}
	if err := validatePatchDispatchIdentity(patchJobDirect, detail, "node-a"); err != nil {
		t.Fatal(err)
	}
	if err := validatePatchDispatchIdentity(patchJobDirect, detail, "node-b"); err == nil {
		t.Fatal("wrong target accepted")
	}
	if err := validatePatchDispatchIdentity(patchJobProxy, detail, "node-a"); err == nil {
		t.Fatal("mode substitution accepted")
	}
}
