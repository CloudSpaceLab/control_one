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

type networkOnboardingStore interface {
	CreateNetworkCredential(context.Context, storage.CreateProviderCredentialParams, storage.TargetAccess) (*storage.ProviderCredential, error)
	GetNetworkCredential(context.Context, uuid.UUID, uuid.UUID, storage.TargetAccess) (*storage.ProviderCredential, error)
	BeginNetworkConnectionTest(context.Context, storage.NetworkConnectionTest, storage.TargetAccess) (*storage.NetworkConnectionTest, error)
	FinishNetworkConnectionTest(context.Context, uuid.UUID, networkdevice.Result) (*storage.NetworkConnectionTest, error)
	SaveNetworkOnboarding(context.Context, storage.NetworkSaveParams, storage.TargetAccess) (*storage.Target, error)
}

type networkCredentialRequest struct {
	TenantID uuid.UUID                `json:"tenant_id"`
	Protocol string                   `json:"protocol"`
	Name     string                   `json:"name"`
	Config   networkdevice.Credential `json:"config"`
}
type networkTestRequest struct {
	TenantID     uuid.UUID `json:"tenant_id"`
	CredentialID uuid.UUID `json:"credential_id"`
	Address      string    `json:"address"`
	Port         int       `json:"port"`
}

// A bounded pool prevents long probes from exhausting server resources.
var networkProbeSlots = make(chan struct{}, 8)

func (s *Server) handleNetworkOnboarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/network-onboarding/")
	if path != "credentials" && path != "tests" && path != "save" {
		http.NotFound(w, r)
		return
	}
	permission := "targets.connect"
	if path == "save" {
		permission = "targets.write"
	}
	access, ok := s.targetAccess(w, r, permission)
	if !ok {
		return
	}
	store, ok := s.store.(networkOnboardingStore)
	if !ok {
		http.Error(w, "network onboarding unavailable", 503)
		return
	}
	if path == "save" {
		var p storage.NetworkSaveParams
		if decodeStrictJSONDocument(r, &p, 16*1024) != nil || p.TestID == uuid.Nil {
			http.Error(w, "invalid save request", 400)
			return
		}
		// Validate user-controlled identity before using a persisted receipt.
		identity := storage.NetworkTargetParams{TenantID: uuid.New(), Type: p.Type, DisplayName: p.DisplayName, Site: p.Site, Group: p.Group, ManagementAddresses: []string{"validation.example"}}
		if err := identity.Validate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		target, err := store.SaveNetworkOnboarding(r.Context(), p, access)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "test receipt not found or access denied", 403)
			return
		}
		if err != nil {
			http.Error(w, "save requires your successful connection test from the last 15 minutes and a supported source selection", 409)
			return
		}
		principal, _ := s.authorize(w, r)
		s.recordAudit(r.Context(), principal, target.TenantID, "network_onboarding.saved", "target", target.ID.String(), map[string]any{"test_id": p.TestID.String(), "type": target.Type, "classification_source": target.Classification.Source})
		writeJSON(w, http.StatusCreated, target)
		return
	}
	if s.sealer == nil {
		http.Error(w, "credential encryption is not configured", 503)
		return
	}
	if path == "credentials" {
		var p networkCredentialRequest
		if decodeStrictJSONDocument(r, &p, 24*1024) != nil || p.TenantID == uuid.Nil {
			http.Error(w, "invalid credential request", 400)
			return
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" || len(p.Name) > 100 || strings.ContainsAny(p.Name, "\r\n\x00") {
			http.Error(w, "credential name is required and must be single-line", 400)
			return
		}
		if err := p.Config.Validate(p.Protocol); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		raw, err := json.Marshal(p.Config)
		if err != nil {
			http.Error(w, "invalid credential", 400)
			return
		}
		sealed, nonce, err := s.sealer.Seal(raw)
		clear(raw)
		if err != nil {
			http.Error(w, "unable to encrypt credential", 500)
			return
		}
		cred, err := store.CreateNetworkCredential(r.Context(), storage.CreateProviderCredentialParams{TenantID: p.TenantID, Provider: "network_" + p.Protocol, Name: p.Name, ConfigEncrypted: sealed, Nonce: nonce}, access)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "tenant access denied", 403)
			return
		}
		if err != nil {
			http.Error(w, "unable to save credential; choose a unique name", 409)
			return
		}
		principal, _ := s.authorize(w, r)
		s.recordAudit(r.Context(), principal, p.TenantID, "network_credential.created", "provider_credential", cred.ID.String(), map[string]any{"protocol": p.Protocol})
		// Never serialize the storage credential or the configuration.
		writeJSON(w, 201, map[string]any{"id": cred.ID, "tenant_id": cred.TenantID, "protocol": p.Protocol, "name": cred.Name})
		return
	}
	var p networkTestRequest
	if decodeStrictJSONDocument(r, &p, 4096) != nil || p.TenantID == uuid.Nil || p.CredentialID == uuid.Nil || p.Port < 0 || p.Port > 65535 {
		http.Error(w, "invalid connection test request", 400)
		return
	}
	address := storage.NetworkTargetParams{TenantID: p.TenantID, Type: "network_appliance", DisplayName: "probe", ManagementAddresses: []string{p.Address}}
	if address.Validate() != nil {
		http.Error(w, "address must be an IP address or DNS name", 400)
		return
	}
	p.Address = address.ManagementAddresses[0]
	cred, err := store.GetNetworkCredential(r.Context(), p.CredentialID, p.TenantID, access)
	if err != nil || cred == nil {
		http.Error(w, "credential not found or access denied", 403)
		return
	}
	protocol := strings.TrimPrefix(cred.Provider, "network_")
	raw, err := s.sealer.Open(cred.ConfigEncrypted, cred.Nonce)
	if err != nil {
		http.Error(w, "unable to open credential", 503)
		return
	}
	var credential networkdevice.Credential
	err = json.Unmarshal(raw, &credential)
	clear(raw)
	if err != nil || credential.Validate(protocol) != nil {
		http.Error(w, "saved credential is invalid", 400)
		return
	}
	if p.Port == 0 {
		p.Port = 161
		if protocol == "ssh" {
			p.Port = 22
		}
	}
	select {
	case networkProbeSlots <- struct{}{}:
		defer func() { <-networkProbeSlots }()
	default:
		http.Error(w, "connection test capacity reached; try again shortly", 429)
		return
	}
	receipt, err := store.BeginNetworkConnectionTest(r.Context(), storage.NetworkConnectionTest{TenantID: p.TenantID, CredentialID: p.CredentialID, Protocol: protocol, Address: p.Address, Port: p.Port}, access)
	if err != nil {
		http.Error(w, "unable to begin connection test", 403)
		return
	}
	principal, _ := s.authorize(w, r)
	s.recordAudit(r.Context(), principal, p.TenantID, "network_connection.started", "network_connection_test", receipt.ID.String(), map[string]any{"protocol": protocol})
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result := networkdevice.Outcome("unreachable")
	ip, resolveErr := networkdevice.Resolve(ctx, p.Address, s.cfg.NetworkOnboarding.AllowedCIDRs)
	if resolveErr != nil {
		result = networkdevice.Outcome(resolveErr.Error())
	} else {
		probe := s.networkProbe
		if probe == nil {
			probe = networkdevice.Probe
		}
		result = probe(ctx, ip, p.Port, protocol, credential)
	}
	// Persist failure receipts even when the browser cancels its request.
	persistCtx, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer persistCancel()
	receipt, err = store.FinishNetworkConnectionTest(persistCtx, receipt.ID, result)
	if err != nil {
		http.Error(w, "unable to persist connection receipt", 500)
		return
	}
	s.recordAudit(persistCtx, principal, p.TenantID, "network_connection.completed", "network_connection_test", receipt.ID.String(), map[string]any{"protocol": protocol, "state": result.State})
	writeJSON(w, 200, receipt)
}
