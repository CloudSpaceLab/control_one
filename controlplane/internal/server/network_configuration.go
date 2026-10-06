package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type networkConfigurationHistoryStore interface {
	GetNetworkConfigurationHistory(context.Context, uuid.UUID, storage.TargetAccess) (*storage.NetworkConfigurationHistory, error)
}

type networkConfigurationSnapshotResponse struct {
	storage.NetworkConfigurationSnapshot
	Added    []string                             `json:"added"`
	Removed  []string                             `json:"removed"`
	Findings []networkdevice.ConfigurationFinding `json:"findings"`
}

func (s *Server) handleNetworkConfiguration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := uuid.Parse(strings.TrimPrefix(r.URL.Path, "/api/v1/network-configuration/"))
	if err != nil || id == uuid.Nil {
		http.NotFound(w, r)
		return
	}
	access, ok := s.targetAccess(w, r, "targets.read")
	if !ok {
		return
	}
	store, ok := s.store.(networkConfigurationHistoryStore)
	if !ok {
		http.Error(w, "network configuration history unavailable", http.StatusServiceUnavailable)
		return
	}
	history, err := store.GetNetworkConfigurationHistory(r.Context(), id, access)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil || history == nil {
		http.Error(w, "unable to read network configuration history", http.StatusInternalServerError)
		return
	}
	response := make([]networkConfigurationSnapshotResponse, 0, len(history.Snapshots))
	previous := map[string]storage.NetworkConfigurationSnapshot{}
	for _, snapshot := range history.Snapshots {
		item := networkConfigurationSnapshotResponse{NetworkConfigurationSnapshot: snapshot, Added: []string{}, Removed: []string{}, Findings: networkdevice.EvaluateConfigurationPostureForSnapshot(snapshot.ID.String(), snapshot.Format, snapshot.Content)}
		if old, ok := previous[snapshot.SourceType]; ok {
			item.Added, item.Removed = networkdevice.ConfigurationDiff(old.Content, snapshot.Content)
			finding := networkdevice.ConfigurationFinding{ID: "configuration_drift", Title: "Configuration drift from previous revision", SnapshotID: snapshot.ID.String(), EvidenceRef: "network_configuration_snapshots:" + snapshot.ID.String(), RelatedEvidenceRef: "network_configuration_snapshots:" + old.ID.String()}
			if len(item.Added)+len(item.Removed) == 0 {
				finding.Status = "not_observed"
			} else {
				finding.Status = "finding"
				finding.Severity = "medium"
			}
			item.Findings = append(item.Findings, finding)
		} else {
			item.Findings = append(item.Findings, networkdevice.ConfigurationFinding{ID: "configuration_drift", Status: "unsupported", Title: "Configuration drift requires a previous snapshot baseline", SnapshotID: snapshot.ID.String()})
		}
		previous[snapshot.SourceType] = snapshot
		response = append(response, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"target_id": history.TargetID, "state": history.State, "snapshots": response})
}
