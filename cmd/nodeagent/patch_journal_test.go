package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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
