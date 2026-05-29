package privateaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSnapshotFromNetBirdPayloadNormalizesRoutePolicy(t *testing.T) {
	payload := map[string]json.RawMessage{
		"peers":  json.RawMessage(`[{"id":"peer-1","name":"app-01","ip":"100.80.0.10","status":"connected","last_seen":"2026-05-29T10:00:00Z"}]`),
		"groups": json.RawMessage(`[{"id":"grp-admins","name":"Admins","peers":["peer-1"]}]`),
		"routes": json.RawMessage(`[{"id":"route-app","network":"10.40.1.0/24","peer":"peer-1","enabled":true,"access_control_groups":["grp-admins"]}]`),
	}
	snapshot, summary, err := SnapshotFromProviderPayload(ProviderNetBird, "bank-prod", payload, time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("normalize payload: %v", err)
	}
	if snapshot.Provider != ProviderNetBird || snapshot.AccountID != "bank-prod" || len(snapshot.Peers) != 1 || len(snapshot.Routes) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if len(snapshot.Policies) != 1 || snapshot.Policies[0].Resources[0].RouteIDs[0] != "route-app" {
		t.Fatalf("policies = %#v", snapshot.Policies)
	}
	if summary.Peers != 1 || summary.Routes != 1 || summary.Policies != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestSnapshotFromOpenZitiPayloadNormalizesServicePolicy(t *testing.T) {
	payload := map[string]json.RawMessage{
		"identities":       json.RawMessage(`[{"id":"id-admin","name":"admin","roleAttributes":["admins"],"enabled":true}]`),
		"services":         json.RawMessage(`[{"id":"svc-ssh","name":"SSH","host":"app-01.bank.local","protocol":"tcp","ports":[22],"enabled":true}]`),
		"service_policies": json.RawMessage(`[{"id":"pol-ssh","name":"SSH admins","identityRoles":["#admins"],"serviceRoles":["@svc-ssh"],"enabled":true}]`),
	}
	snapshot, _, err := SnapshotFromProviderPayload(ProviderOpenZiti, "", payload, time.Time{})
	if err != nil {
		t.Fatalf("normalize payload: %v", err)
	}
	if snapshot.AccountID != "default" || len(snapshot.Services) != 1 || snapshot.Services[0].Ports[0] != 22 {
		t.Fatalf("services = %#v account=%s", snapshot.Services, snapshot.AccountID)
	}
	if len(snapshot.Policies) != 1 || snapshot.Policies[0].Sources[0].Tag != "admins" || snapshot.Policies[0].Resources[0].ServiceIDs[0] != "svc-ssh" {
		t.Fatalf("policies = %#v", snapshot.Policies)
	}
}

func TestFetchSnapshotUsesProviderAuthorizationAndEndpointOverrides(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if r.URL.Path == "/custom/peers" {
			_, _ = w.Write([]byte(`[{"id":"peer-1","name":"router","online":true}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	snapshot, _, err := FetchSnapshot(context.Background(), HTTPImportConfig{
		Provider:  ProviderHeadscale,
		AccountID: "hs",
		BaseURL:   srv.URL,
		Token:     "secret-token",
		Endpoints: map[string]string{
			"nodes": "/custom/peers",
		},
		Client: srv.Client(),
	})
	if err != nil {
		t.Fatalf("fetch snapshot: %v", err)
	}
	if !strings.EqualFold(authHeader, "Bearer secret-token") {
		t.Fatalf("authorization = %q", authHeader)
	}
	if len(snapshot.Peers) != 1 || snapshot.Peers[0].ID != "peer-1" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
