package amlservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClientRejectsInsecureBaseURLByDefault(t *testing.T) {
	_, err := NewClient(Config{BaseURL: "http://aml.internal:8090", APIKey: "secret"})
	if err == nil {
		t.Fatal("expected insecure base URL to be rejected")
	}
}

func TestClientScreenCallsAMLServiceWithAPIKeyAndMappedPayload(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotReq ScreeningRequest

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-API-Key")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"req-1","risk_level":"high","overall_risk":0.81}`))
	}))
	t.Cleanup(upstream.Close)

	client, err := NewClient(Config{
		BaseURL:       upstream.URL,
		APIKey:        "secret",
		AllowInsecure: true,
		Timeout:       time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	res, err := client.Screen(t.Context(), ScreeningRequest{
		Name:                "Jane Doe",
		Country:             "NG",
		BirthDate:           "1990-01-01",
		IDNumber:            "12345678901",
		EntityType:          "person",
		IncludeSanctions:    true,
		IncludePEP:          true,
		IncludeAdverseMedia: true,
		IncludeRegistry:     true,
	})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}

	if gotPath != "/api/v1/screen" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotAuth != "secret" {
		t.Fatalf("X-API-Key: got %q", gotAuth)
	}
	if gotReq.Name != "Jane Doe" || gotReq.BirthDate != "1990-01-01" || gotReq.IDNumber != "12345678901" {
		t.Fatalf("unexpected payload: %+v", gotReq)
	}
	if !gotReq.IncludeSanctions || !gotReq.IncludePEP || !gotReq.IncludeAdverseMedia || !gotReq.IncludeRegistry {
		t.Fatalf("include flags not propagated: %+v", gotReq)
	}
	if res.RequestID != "req-1" || res.RiskLevel != "high" || res.OverallRisk != 0.81 {
		t.Fatalf("unexpected response: %+v", res)
	}
}

func TestClientScreenOmitsEmptyBirthDate(t *testing.T) {
	var raw map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"req-2","risk_level":"low","overall_risk":0}`))
	}))
	t.Cleanup(upstream.Close)

	client, err := NewClient(Config{BaseURL: upstream.URL, AllowInsecure: true})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Screen(t.Context(), ScreeningRequest{Name: "Jane Doe", IncludeSanctions: true}); err != nil {
		t.Fatalf("Screen: %v", err)
	}
	if _, ok := raw["birth_date"]; ok {
		t.Fatalf("birth_date should be omitted when empty: %+v", raw)
	}
}
