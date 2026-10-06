package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type networkInventoryStore interface {
	GetNetworkInventory(context.Context, uuid.UUID, storage.TargetAccess) (*storage.NetworkInventory, error)
	GetNetworkInventoryConnection(context.Context, uuid.UUID, storage.TargetAccess) (*storage.NetworkInventoryConnection, error)
	BeginNetworkInventory(context.Context, uuid.UUID, storage.TargetAccess) (*storage.NetworkInventory, error)
	FinishNetworkInventory(context.Context, uuid.UUID, uuid.UUID, networkdevice.Inventory) (*storage.NetworkInventory, error)
}

func (s *Server) handleNetworkInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	id, err := uuid.Parse(strings.TrimPrefix(r.URL.Path, "/api/v1/network-inventory/"))
	if err != nil || id == uuid.Nil {
		http.NotFound(w, r)
		return
	}
	permission := "targets.read"
	if r.Method == http.MethodPost {
		permission = "targets.connect"
	}
	access, ok := s.targetAccess(w, r, permission)
	if !ok {
		return
	}
	store, ok := s.store.(networkInventoryStore)
	if !ok {
		http.Error(w, "network inventory unavailable", 503)
		return
	}
	if r.Method == http.MethodGet {
		result, err := store.GetNetworkInventory(r.Context(), id, access)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "unable to read inventory", 500)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	// Credentials, ports and commands cannot be supplied through refresh.
	var payload struct{}
	if decodeStrictJSONDocument(r, &payload, 1024) != nil {
		http.Error(w, "refresh expects an empty JSON object", 400)
		return
	}
	if s.sealer == nil {
		http.Error(w, "credential encryption is not configured", 503)
		return
	}
	connection, err := store.GetNetworkInventoryConnection(r.Context(), id, access)
	if err != nil || connection == nil {
		http.Error(w, "verified connection not found or access denied", 403)
		return
	}
	raw, err := s.sealer.Open(connection.Credential.ConfigEncrypted, connection.Credential.Nonce)
	if err != nil {
		http.Error(w, "unable to open credential", 503)
		return
	}
	var credential networkdevice.Credential
	err = json.Unmarshal(raw, &credential)
	clear(raw)
	if err != nil || credential.Validate(connection.Protocol) != nil {
		http.Error(w, "saved credential is invalid", 400)
		return
	}
	select {
	case networkProbeSlots <- struct{}{}:
		defer func() { <-networkProbeSlots }()
	default:
		http.Error(w, "connection capacity reached; try again shortly", 429)
		return
	}
	receipt, err := store.BeginNetworkInventory(r.Context(), id, access)
	if errors.Is(err, storage.ErrInventoryBusy) {
		http.Error(w, "inventory refresh already in progress", 409)
		return
	}
	if err != nil {
		http.Error(w, "unable to begin inventory refresh", 403)
		return
	}
	principal, _ := s.authorize(w, r)
	s.recordAudit(r.Context(), principal, connection.TenantID, "network_inventory.started", "target", id.String(), map[string]any{"refresh_id": receipt.RefreshID.String(), "protocol": connection.Protocol})
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	inventory := networkdevice.Inventory{State: "unreachable"}
	ip, err := networkdevice.Resolve(ctx, connection.Address, s.cfg.NetworkOnboarding.AllowedCIDRs)
	if err != nil {
		inventory.State = err.Error()
	} else {
		collect := s.networkInventory
		if collect == nil {
			collect = networkdevice.Refresh
		}
		inventory = collect(ctx, ip, connection.Port, connection.Protocol, credential)
	}
	persistCtx, persistCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer persistCancel()
	result, err := store.FinishNetworkInventory(persistCtx, id, receipt.RefreshID, inventory)
	if err != nil {
		http.Error(w, "unable to persist inventory refresh", 500)
		return
	}
	s.recordAudit(persistCtx, principal, connection.TenantID, "network_inventory.completed", "target", id.String(), map[string]any{"refresh_id": receipt.RefreshID.String(), "state": inventory.State})
	writeJSON(w, 200, result)
}
