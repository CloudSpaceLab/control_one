# Universal Agent-Managed Targets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Control One treat every enrolled node as a universal agent-managed target whose identity is stable and whose IP addresses are changing observations.

**Architecture:** This first shippable slice keeps `nodes` as the implementation object and adds target semantics through typed helpers, labels, heartbeat enrichment, node API serialization, and onboarding UI changes. Static IP remains supported as `public_ip`, but API/UI consumers also receive `management_mode`, `target_type`, `reachability_mode`, `classification`, and `network_observations`.

**Tech Stack:** Go control plane, Postgres-backed storage via existing `storage.Store`, React/TypeScript UI, Vitest, Go tests.

---

## File Structure

- Modify `controlplane/internal/storage/store.go`: add typed target metadata structs and label helper methods on `storage.Node`.
- Modify `controlplane/internal/storage/nodes_test.go`: cover metadata defaults, classification overrides, and network observation extraction from labels/public IP.
- Modify `controlplane/internal/server/heartbeat.go`: accept optional target metadata fields on heartbeat and persist normalized target labels.
- Modify `controlplane/internal/server/heartbeat_test.go`: verify heartbeat persists network observations and does not break legacy heartbeats.
- Modify `controlplane/internal/server/server.go`: enrich `nodeResponse` with target metadata fields.
- Modify `controlplane/internal/server/server_test.go`: verify list/get node responses include target metadata defaults and label-derived values.
- Modify `controlplane/internal/server/enrollment.go`: stamp `management_mode`, intended target/onboarding labels, and first public-IP observation during enrollment.
- Modify `cmd/nodeagent/join.go`: add install context to enrollment request.
- Modify `ui/src/lib/api.ts`: add target metadata TypeScript types to `NodeSummary`.
- Modify `ui/src/pages/Onboard.tsx`: change server-only onboarding copy to machine/target language and add scenario-oriented entry text while reusing existing flows.
- Modify `ui/src/pages/Nodes.tsx`: render target type and reachability mode on node cards and label public IP as an observation.
- Modify `ui/src/pages/Nodes.test.tsx`: cover target metadata rendering.
- Modify `ui/src/pages/FleetEnroll.tsx`: change bulk enrollment copy from host/server-only phrasing to machine/target phrasing.
- Modify `ui/src/hooks/useOnboardingState.ts`: change checklist copy from server-only to machine/target language.

## Target Metadata Contract

Use these canonical label keys for the first slice:

- `target.management_mode`: always `agent_managed`.
- `target.type`: target class such as `server`, `personal_pc`, `workstation`, `laptop`, `vm`, `cloud_instance`, or `unknown`.
- `target.type_source`: `heuristic`, `operator_override`, `enrollment_hint`, or `default`.
- `target.classification_confidence`: integer 0-100.
- `target.classification_evidence`: string array.
- `target.reachability_mode`: `outbound_only`, `direct_private`, `direct_public`, `overlay`, `offline_periodic`, or `unknown`.
- `target.install_context`: `local_interactive`, `remote_push`, `fleet_enroll`, `offline_bundle`, `repair`, or `unknown`.
- `target.network_observations`: array of observation objects.

Network observation object:

```json
{
  "kind": "public_ip",
  "value": "203.0.113.10",
  "source": "enrollment",
  "first_seen_at": "2026-07-20T00:00:00Z",
  "last_seen_at": "2026-07-20T00:00:00Z",
  "confidence": 90
}
```

---

### Task 1: Storage Target Metadata Helpers

**Files:**
- Modify: `controlplane/internal/storage/store.go`
- Test: `controlplane/internal/storage/nodes_test.go`

- [ ] **Step 1: Write failing storage tests**

Add these tests to `controlplane/internal/storage/nodes_test.go`:

```go
func TestNodeTargetMetadataDefaultsToAgentManagedUnknown(t *testing.T) {
	node := Node{Labels: map[string]any{}}
	meta := node.TargetMetadata()

	if meta.ManagementMode != "agent_managed" {
		t.Fatalf("management mode = %q", meta.ManagementMode)
	}
	if meta.TargetType != "unknown" {
		t.Fatalf("target type = %q", meta.TargetType)
	}
	if meta.ReachabilityMode != "unknown" {
		t.Fatalf("reachability = %q", meta.ReachabilityMode)
	}
	if meta.Classification.Source != "default" {
		t.Fatalf("source = %q", meta.Classification.Source)
	}
}

func TestNodeTargetMetadataReadsLabelsAndPublicIPObservation(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	node := Node{
		PublicIP: sql.NullString{String: "203.0.113.10", Valid: true},
		Labels: map[string]any{
			"target.type":                      "server",
			"target.type_source":               "heuristic",
			"target.classification_confidence": float64(85),
			"target.classification_evidence":   []any{"server OS edition", "webserver listener"},
			"target.reachability_mode":         "direct_public",
			"target.install_context":           "fleet_enroll",
			"target.network_observations": []any{
				map[string]any{
					"kind":          "public_ip",
					"value":         "198.51.100.5",
					"source":        "heartbeat",
					"first_seen_at":  now.Format(time.RFC3339),
					"last_seen_at":   now.Format(time.RFC3339),
					"confidence":    float64(95),
				},
			},
		},
	}

	meta := node.TargetMetadata()
	if meta.TargetType != "server" || meta.Classification.Confidence != 85 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if len(meta.NetworkObservations) != 2 {
		t.Fatalf("expected label observation plus public_ip observation, got %+v", meta.NetworkObservations)
	}
	if meta.NetworkObservations[1].Value != "203.0.113.10" || meta.NetworkObservations[1].Source != "node.public_ip" {
		t.Fatalf("public_ip observation missing: %+v", meta.NetworkObservations)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./controlplane/internal/storage -run TestNodeTargetMetadata -count=1`

Expected: FAIL because `Node.TargetMetadata` and related types are undefined.

- [ ] **Step 3: Add target metadata types and helpers**

Add near the `Node` type in `controlplane/internal/storage/store.go`:

```go
type TargetClassification struct {
	Source     string   `json:"source"`
	Confidence int      `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

type NetworkObservation struct {
	Kind        string `json:"kind"`
	Value       string `json:"value"`
	Source      string `json:"source"`
	FirstSeenAt string `json:"first_seen_at,omitempty"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
	Confidence  int    `json:"confidence"`
}

type TargetMetadata struct {
	ManagementMode      string                 `json:"management_mode"`
	TargetType          string                 `json:"target_type"`
	ReachabilityMode    string                 `json:"reachability_mode"`
	InstallContext      string                 `json:"install_context,omitempty"`
	Classification      TargetClassification   `json:"classification"`
	NetworkObservations []NetworkObservation   `json:"network_observations"`
}

func (n Node) TargetMetadata() TargetMetadata {
	labels := n.Labels
	if labels == nil {
		labels = map[string]any{}
	}
	meta := TargetMetadata{
		ManagementMode:   labelString(labels, "target.management_mode", "agent_managed"),
		TargetType:       labelString(labels, "target.type", "unknown"),
		ReachabilityMode: labelString(labels, "target.reachability_mode", "unknown"),
		InstallContext:   labelString(labels, "target.install_context", ""),
		Classification: TargetClassification{
			Source:     labelString(labels, "target.type_source", "default"),
			Confidence: labelInt(labels, "target.classification_confidence", 0),
			Evidence:   labelStringSlice(labels, "target.classification_evidence"),
		},
		NetworkObservations: labelNetworkObservations(labels),
	}
	if n.PublicIP.Valid && strings.TrimSpace(n.PublicIP.String) != "" {
		meta.NetworkObservations = append(meta.NetworkObservations, NetworkObservation{
			Kind:       "public_ip",
			Value:      strings.TrimSpace(n.PublicIP.String),
			Source:     "node.public_ip",
			Confidence: 60,
		})
	}
	return meta
}
```

Add helper functions below it:

```go
func labelString(labels map[string]any, key, fallback string) string {
	if raw, ok := labels[key]; ok {
		if value, ok := raw.(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}

func labelInt(labels map[string]any, key string, fallback int) int {
	raw, ok := labels[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i)
		}
	}
	return fallback
}

func labelStringSlice(labels map[string]any, key string) []string {
	raw, ok := labels[key]
	if !ok {
		return []string{}
	}
	out := []string{}
	switch values := raw.(type) {
	case []string:
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				out = append(out, strings.TrimSpace(value))
			}
		}
	case []any:
		for _, rawValue := range values {
			if value, ok := rawValue.(string); ok && strings.TrimSpace(value) != "" {
				out = append(out, strings.TrimSpace(value))
			}
		}
	}
	return out
}

func labelNetworkObservations(labels map[string]any) []NetworkObservation {
	raw, ok := labels["target.network_observations"]
	if !ok {
		return []NetworkObservation{}
	}
	items, ok := raw.([]any)
	if !ok {
		return []NetworkObservation{}
	}
	out := make([]NetworkObservation, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		obs := NetworkObservation{
			Kind:        labelString(m, "kind", ""),
			Value:       labelString(m, "value", ""),
			Source:      labelString(m, "source", ""),
			FirstSeenAt: labelString(m, "first_seen_at", ""),
			LastSeenAt:  labelString(m, "last_seen_at", ""),
			Confidence:  labelInt(m, "confidence", 0),
		}
		if obs.Kind != "" && obs.Value != "" {
			out = append(out, obs)
		}
	}
	return out
}
```

- [ ] **Step 4: Run storage tests**

Run: `go test ./controlplane/internal/storage -run TestNodeTargetMetadata -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/internal/storage/store.go controlplane/internal/storage/nodes_test.go
git commit -m "feat: add node target metadata helpers"
```

---

### Task 2: Enrollment Labels for Agent-Managed Targets

**Files:**
- Modify: `controlplane/internal/server/enrollment.go`
- Test: `controlplane/internal/server/enrollment_test.go`
- Modify: `cmd/nodeagent/join.go`

- [ ] **Step 1: Write failing enrollment test**

Add to `controlplane/internal/server/enrollment_test.go`:

```go
func TestEnrollStampsAgentManagedTargetLabels(t *testing.T) {
	srv, rawToken, tenantID := setupEnrollmentServer(t)

	body := `{
		"token":"` + rawToken + `",
		"hostname":"laptop-01",
		"os":"windows",
		"arch":"amd64",
		"public_ip":"203.0.113.99",
		"machine_id":"machine-123",
		"install_context":"local_interactive",
		"target_hint":"laptop"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleEnroll(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp enrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	nodeID, _ := uuid.Parse(resp.NodeID)
	node, err := srv.store.GetNode(context.Background(), nodeID)
	if err != nil || node == nil {
		t.Fatalf("node: %v", err)
	}
	if node.TenantID != tenantID {
		t.Fatalf("tenant mismatch")
	}
	if node.Labels["target.management_mode"] != "agent_managed" {
		t.Fatalf("labels=%+v", node.Labels)
	}
	if node.Labels["target.type"] != "laptop" {
		t.Fatalf("target type labels=%+v", node.Labels)
	}
	if node.Labels["target.install_context"] != "local_interactive" {
		t.Fatalf("install context labels=%+v", node.Labels)
	}
	if node.Labels["target.reachability_mode"] != "direct_public" {
		t.Fatalf("reachability labels=%+v", node.Labels)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controlplane/internal/server -run TestEnrollStampsAgentManagedTargetLabels -count=1`

Expected: FAIL because `install_context` and `target_hint` are not decoded or stamped.

- [ ] **Step 3: Extend enrollment request and labels**

In `controlplane/internal/server/enrollment.go`, extend `enrollRequest`:

```go
InstallContext string `json:"install_context,omitempty"`
TargetHint     string `json:"target_hint,omitempty"`
```

Change the new-node labels call:

```go
Labels: enrollmentNodeLabels(nil, token, req),
```

Change the re-enrollment labels call:

```go
mergedLabels := enrollmentNodeLabels(existing.Labels, token, req)
```

Replace the helper signature and body:

```go
func enrollmentNodeLabels(existing map[string]any, token *storage.EnrollmentToken, req enrollRequest) map[string]any {
	labels := map[string]any{}
	for key, value := range existing {
		labels[key] = value
	}
	if token != nil {
		for key, value := range token.Labels {
			key = strings.TrimSpace(key)
			if key != "" {
				labels[key] = strings.TrimSpace(value)
			}
		}
		labels["enrollment.token_id"] = token.ID.String()
		labels["enrollment.token_name"] = token.Name
	}
	labels["target.management_mode"] = "agent_managed"
	installContext := normalizeInstallContext(req.InstallContext)
	if installContext != "" {
		labels["target.install_context"] = installContext
	}
	if targetType := normalizeTargetType(req.TargetHint); targetType != "" {
		labels["target.type"] = targetType
		labels["target.type_source"] = "enrollment_hint"
		labels["target.classification_confidence"] = 60
	} else if _, ok := labels["target.type"]; !ok {
		labels["target.type"] = "unknown"
		labels["target.type_source"] = "default"
	}
	if strings.TrimSpace(req.PublicIP) != "" {
		labels["target.reachability_mode"] = "direct_public"
	}
	return labels
}
```

Add helpers:

```go
func normalizeInstallContext(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "local_interactive", "remote_push", "fleet_enroll", "offline_bundle", "repair":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeTargetType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "personal_pc", "workstation", "laptop", "server", "vm", "cloud_instance", "domain_controller", "kiosk":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}
```

- [ ] **Step 4: Update node agent join enrollment payload**

In `cmd/nodeagent/join.go`, add fields to `enrollRequest`:

```go
InstallContext string `json:"install_context,omitempty"`
TargetHint     string `json:"target_hint,omitempty"`
```

Set a default in `runJoin`:

```go
InstallContext: installContextForJoin(installService),
```

Add helper:

```go
func installContextForJoin(installService bool) string {
	if installService {
		return "local_interactive"
	}
	return "local_interactive"
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./controlplane/internal/server -run TestEnrollStampsAgentManagedTargetLabels -count=1
go test ./cmd/nodeagent -run TestBootstrap -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/server/enrollment.go controlplane/internal/server/enrollment_test.go cmd/nodeagent/join.go
git commit -m "feat: stamp target metadata during enrollment"
```

---

### Task 3: Heartbeat Target Metadata and Network Observations

**Files:**
- Modify: `controlplane/internal/server/heartbeat.go`
- Test: `controlplane/internal/server/heartbeat_test.go`

- [ ] **Step 1: Write failing heartbeat test**

Add to `controlplane/internal/server/heartbeat_test.go`:

```go
func TestHeartbeatPersistsTargetMetadataObservations(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "endpoint-1",
			State:     storage.NodeStateActive,
			CreatedAt: now,
			UpdatedAt: now,
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)
	body := `{
		"agent_version":"1.2.3",
		"target_type":"workstation",
		"reachability_mode":"outbound_only",
		"install_context":"local_interactive",
		"network_observations":[
			{"kind":"private_ip","value":"10.0.0.25","source":"agent_interface","confidence":80}
		],
		"target_classification_evidence":["desktop OS edition","battery present"],
		"target_classification_confidence":75
	}`

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	req.Body = io.NopCloser(strings.NewReader(body))
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	node, err := srv.store.GetNode(context.Background(), nodeID)
	if err != nil || node == nil {
		t.Fatalf("node: %v", err)
	}
	if node.Labels["target.type"] != "workstation" {
		t.Fatalf("labels=%+v", node.Labels)
	}
	if node.Labels["target.reachability_mode"] != "outbound_only" {
		t.Fatalf("labels=%+v", node.Labels)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controlplane/internal/server -run TestHeartbeatPersistsTargetMetadataObservations -count=1`

Expected: FAIL because heartbeat ignores target metadata fields.

- [ ] **Step 3: Extend heartbeat request**

In `heartbeatRequest`, add:

```go
TargetType                       string                         `json:"target_type,omitempty"`
ReachabilityMode                  string                         `json:"reachability_mode,omitempty"`
InstallContext                    string                         `json:"install_context,omitempty"`
NetworkObservations               []heartbeatNetworkObservation  `json:"network_observations,omitempty"`
TargetClassificationEvidence      []string                       `json:"target_classification_evidence,omitempty"`
TargetClassificationConfidence    int                            `json:"target_classification_confidence,omitempty"`
```

Add type:

```go
type heartbeatNetworkObservation struct {
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Source     string `json:"source"`
	Confidence int    `json:"confidence,omitempty"`
}
```

- [ ] **Step 4: Persist metadata after capabilities/server purposes**

In `handleNodeHeartbeat`, after server-purpose update, add:

```go
if updated, terr := s.updateNodeTargetMetadataFromHeartbeat(r.Context(), node, body); terr != nil {
	s.logger.Warn("update target metadata", zap.Error(terr))
} else if updated != nil {
	node = updated
}
```

Add function:

```go
func (s *Server) updateNodeTargetMetadataFromHeartbeat(ctx context.Context, node *storage.Node, body heartbeatRequest) (*storage.Node, error) {
	if s == nil || s.store == nil || node == nil {
		return node, nil
	}
	labels := map[string]any{}
	for k, v := range node.Labels {
		labels[k] = v
	}
	labels["target.management_mode"] = "agent_managed"
	if targetType := normalizeTargetType(body.TargetType); targetType != "" {
		labels["target.type"] = targetType
		labels["target.type_source"] = "heuristic"
	}
	if mode := normalizeReachabilityMode(body.ReachabilityMode); mode != "" {
		labels["target.reachability_mode"] = mode
	}
	if context := normalizeInstallContext(body.InstallContext); context != "" {
		labels["target.install_context"] = context
	}
	if body.TargetClassificationConfidence > 0 {
		labels["target.classification_confidence"] = clampInt(body.TargetClassificationConfidence, 0, 100)
	}
	if len(body.TargetClassificationEvidence) > 0 {
		labels["target.classification_evidence"] = sanitizeStringSlice(body.TargetClassificationEvidence, 32)
	}
	if len(body.NetworkObservations) > 0 {
		labels["target.network_observations"] = normalizeHeartbeatNetworkObservations(body.NetworkObservations, time.Now().UTC())
	}
	if err := s.store.UpdateNodeLabels(ctx, node.ID, labels); err != nil {
		return node, err
	}
	updated := *node
	updated.Labels = labels
	return &updated, nil
}
```

Add helpers:

```go
func normalizeReachabilityMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "outbound_only", "direct_private", "direct_public", "overlay", "offline_periodic", "unknown":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeHeartbeatNetworkObservations(items []heartbeatNetworkObservation, observedAt time.Time) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	stamp := observedAt.UTC().Format(time.RFC3339)
	for _, item := range items {
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		value := strings.TrimSpace(item.Value)
		if kind == "" || value == "" {
			continue
		}
		out = append(out, map[string]any{
			"kind":         kind,
			"value":        value,
			"source":       strings.TrimSpace(item.Source),
			"last_seen_at": stamp,
			"confidence":   clampInt(item.Confidence, 0, 100),
		})
	}
	return out
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
```

- [ ] **Step 5: Run heartbeat tests**

Run:

```bash
go test ./controlplane/internal/server -run 'TestHeartbeat(PersistsTargetMetadataObservations|AllowsUnknownFields|UpdatesAgentCapabilities)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/server/heartbeat.go controlplane/internal/server/heartbeat_test.go
git commit -m "feat: persist target metadata from heartbeat"
```

---

### Task 4: Enrich Node API Responses

**Files:**
- Modify: `controlplane/internal/server/server.go`
- Test: `controlplane/internal/server/server_test.go`

- [ ] **Step 1: Write failing API response test**

Add to `controlplane/internal/server/server_test.go`:

```go
func TestNodeResponseIncludesTargetMetadata(t *testing.T) {
	node := storage.Node{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Hostname: "endpoint-1",
		State:    storage.NodeStateActive,
		Labels: map[string]any{
			"target.type":                      "workstation",
			"target.type_source":               "heuristic",
			"target.reachability_mode":         "outbound_only",
			"target.classification_confidence": 70,
			"target.classification_evidence":   []any{"desktop OS edition"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	resp := nodeResponseFromModel(node)
	if resp.ManagementMode != "agent_managed" {
		t.Fatalf("management mode = %q", resp.ManagementMode)
	}
	if resp.TargetType != "workstation" {
		t.Fatalf("target type = %q", resp.TargetType)
	}
	if resp.ReachabilityMode != "outbound_only" {
		t.Fatalf("reachability = %q", resp.ReachabilityMode)
	}
	if resp.Classification.Confidence != 70 {
		t.Fatalf("classification = %+v", resp.Classification)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controlplane/internal/server -run TestNodeResponseIncludesTargetMetadata -count=1`

Expected: FAIL because response fields do not exist.

- [ ] **Step 3: Extend node response**

In `nodeResponse`, add:

```go
ManagementMode      string                          `json:"management_mode"`
TargetType          string                          `json:"target_type"`
ReachabilityMode    string                          `json:"reachability_mode"`
InstallContext      string                          `json:"install_context,omitempty"`
Classification      storage.TargetClassification    `json:"classification"`
NetworkObservations []storage.NetworkObservation    `json:"network_observations"`
```

In `nodeResponseFromModel`, after initializing `resp`, add:

```go
target := n.TargetMetadata()
resp.ManagementMode = target.ManagementMode
resp.TargetType = target.TargetType
resp.ReachabilityMode = target.ReachabilityMode
resp.InstallContext = target.InstallContext
resp.Classification = target.Classification
resp.NetworkObservations = target.NetworkObservations
```

- [ ] **Step 4: Run server tests**

Run:

```bash
go test ./controlplane/internal/server -run 'TestNodeResponseIncludesTargetMetadata|TestNode' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controlplane/internal/server/server.go controlplane/internal/server/server_test.go
git commit -m "feat: expose target metadata on node APIs"
```

---

### Task 5: Frontend API Types and Node Cards

**Files:**
- Modify: `ui/src/lib/api.ts`
- Modify: `ui/src/pages/Nodes.tsx`
- Test: `ui/src/pages/Nodes.test.tsx`

- [ ] **Step 1: Write failing UI test**

In `ui/src/pages/Nodes.test.tsx`, extend the existing `node` fixture with:

```ts
target_type: 'workstation',
management_mode: 'agent_managed',
reachability_mode: 'outbound_only',
classification: { source: 'heuristic', confidence: 70, evidence: ['desktop OS edition'] },
network_observations: [
  { kind: 'public_ip', value: '203.0.113.10', source: 'node.public_ip', confidence: 60 },
],
```

Add an assertion to the main render test:

```ts
expect(await screen.findByText(/workstation/i)).toBeInTheDocument();
expect(screen.getByText(/outbound only/i)).toBeInTheDocument();
expect(screen.getByText(/observed ip/i)).toBeInTheDocument();
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npm --prefix ui test -- Nodes.test.tsx`

Expected: FAIL because types/rendering do not include target metadata.

- [ ] **Step 3: Add TypeScript types**

In `ui/src/lib/api.ts`, add:

```ts
export type TargetType =
  | "personal_pc"
  | "workstation"
  | "laptop"
  | "server"
  | "vm"
  | "cloud_instance"
  | "domain_controller"
  | "kiosk"
  | "unknown"
  | (string & {});

export type ReachabilityMode =
  | "outbound_only"
  | "direct_private"
  | "direct_public"
  | "overlay"
  | "offline_periodic"
  | "unknown"
  | (string & {});

export interface TargetClassification {
  source: string;
  confidence: number;
  evidence: string[];
}

export interface NetworkObservation {
  kind: string;
  value: string;
  source: string;
  first_seen_at?: string;
  last_seen_at?: string;
  confidence: number;
}
```

Extend `NodeSummary`:

```ts
management_mode?: "agent_managed" | string;
target_type?: TargetType;
reachability_mode?: ReachabilityMode;
install_context?: string;
classification?: TargetClassification;
network_observations?: NetworkObservation[];
```

- [ ] **Step 4: Render metadata on cards**

In `ui/src/pages/Nodes.tsx`, add helpers near existing node label helpers:

```ts
function humanizeTargetValue(value?: string): string {
  if (!value) return 'unknown';
  return value.replace(/_/g, ' ');
}

function primaryObservedIP(node: NodeSummary): string | undefined {
  const observations = node.network_observations ?? [];
  return observations.find((item) => item.kind === 'public_ip')?.value ?? node.public_ip;
}
```

In `NodeCard`, replace the direct `node.public_ip` display block:

```tsx
{primaryObservedIP(node) && (
  <div className="flex flex-col gap-0.5">
    <span className="font-mono text-[0.55rem] uppercase tracking-wider text-text-muted">Observed IP</span>
    <code className="font-mono text-[0.65rem] text-text-muted">{primaryObservedIP(node)}</code>
  </div>
)}
```

Add target chips beside OS/arch:

```tsx
<span className="rounded bg-surface-2 px-1.5 py-0.5 text-[0.6rem] font-medium uppercase tracking-wider text-text-secondary">
  {humanizeTargetValue(node.target_type)}
</span>
<span className="rounded bg-surface-2 px-1.5 py-0.5 text-[0.6rem] font-medium uppercase tracking-wider text-text-muted">
  {humanizeTargetValue(node.reachability_mode)}
</span>
```

- [ ] **Step 5: Run UI tests**

Run:

```bash
npm --prefix ui test -- Nodes.test.tsx
npm --prefix ui run lint -- --max-warnings=0
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ui/src/lib/api.ts ui/src/pages/Nodes.tsx ui/src/pages/Nodes.test.tsx
git commit -m "feat: show target metadata in nodes UI"
```

---

### Task 6: Onboarding Language and Scenario Copy

**Files:**
- Modify: `ui/src/pages/Onboard.tsx`
- Modify: `ui/src/pages/FleetEnroll.tsx`
- Modify: `ui/src/hooks/useOnboardingState.ts`
- Test: create or modify `ui/src/pages/Onboard.test.tsx`

- [ ] **Step 1: Write failing onboarding copy test**

Create `ui/src/pages/Onboard.test.tsx` if it does not exist:

```tsx
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { Onboard } from './Onboard';

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => ({
    enrichIp: vi.fn(),
    testServerConnection: vi.fn(),
    createEnrollmentToken: vi.fn(),
    startFleetEnroll: vi.fn(),
    createTenant: vi.fn(),
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1', tenants: [{ id: 'tenant-1', name: 'Production' }], refresh: vi.fn() }),
}));

vi.mock('../components/settings/OnboardAIPanel', () => ({
  OnboardAIPanel: () => null,
}));

describe('Onboard', () => {
  it('uses universal machine onboarding language', () => {
    render(
      <MemoryRouter>
        <Onboard />
      </MemoryRouter>,
    );

    expect(screen.getByText(/Add machines to Control One/i)).toBeInTheDocument();
    expect(screen.getByText(/No inbound access is required after the agent is installed/i)).toBeInTheDocument();
    expect(screen.queryByText(/Add servers to Control One/i)).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npm --prefix ui test -- Onboard.test.tsx`

Expected: FAIL because current copy says "Add servers to Control One" and lacks the no-inbound sentence.

- [ ] **Step 3: Update `Onboard.tsx` copy**

Change `SectionHeader`:

```tsx
title="Add machines to Control One"
description="Enroll a personal endpoint, workstation, server, VM, or restricted-network machine. Control One tracks the agent identity even when IP addresses change."
```

Change action button text:

```tsx
<Server className="h-4 w-4" /> Bulk machines
```

Change tabs:

```tsx
<Terminal className="h-4 w-4" /> Remote install
<Boxes className="h-4 w-4" /> Hypervisor / cloud
```

Insert a compact informational `Panel` below `OnboardAIPanel`:

```tsx
<Panel padding="sm" toneAccent="brand">
  <div className="grid gap-2 text-sm text-text-secondary md:grid-cols-3">
    <span>The machine appears after its first heartbeat.</span>
    <span>No inbound access is required after the agent is installed.</span>
    <span>IP addresses are tracked as observations, not identity.</span>
  </div>
</Panel>
```

Change protocol panel title from `"Pick how to reach the host"` to `"Pick how to install remotely"`.

Change target panel title from `"Where is the server?"` to `"Where is the machine right now?"`.

Change enrollment button text from `"Enrol server"` to `"Enrol machine"`.

- [ ] **Step 4: Update `FleetEnroll.tsx` and onboarding checklist copy**

In `ui/src/pages/FleetEnroll.tsx`, change:

```tsx
title="Bulk enroll machines"
description="Onboard many agent-managed machines at once. Live progress per target."
```

In `ui/src/hooks/useOnboardingState.ts`, change the server step:

```ts
title: 'Onboard a machine',
description: 'Install the Control One agent on a PC, workstation, server, VM, or restricted-network machine.',
cta: hasNode ? 'Manage machines' : 'Add machine',
```

- [ ] **Step 5: Run UI tests**

Run:

```bash
npm --prefix ui test -- Onboard.test.tsx
npm --prefix ui test -- FleetEnroll.test.tsx Nodes.test.tsx
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/Onboard.tsx ui/src/pages/FleetEnroll.tsx ui/src/hooks/useOnboardingState.ts ui/src/pages/Onboard.test.tsx
git commit -m "fix: make onboarding language target-neutral"
```

---

### Task 7: Compatibility and Regression Sweep

**Files:**
- No source changes expected unless tests reveal regressions.

- [ ] **Step 1: Run backend focused tests**

Run:

```bash
go test ./controlplane/internal/storage -count=1
go test ./controlplane/internal/server -run 'Test(Enroll|Heartbeat|NodeResponse|ListNodes|GetNode)' -count=1
go test ./cmd/nodeagent -run 'Test(Bootstrap|Service|Join)' -count=1
```

Expected: PASS or no matching tests for narrow patterns. Investigate any compile failures because new structs touch shared packages.

- [ ] **Step 2: Run frontend focused tests**

Run:

```bash
npm --prefix ui test -- Onboard.test.tsx Nodes.test.tsx FleetEnroll.test.tsx
npm --prefix ui run lint -- --max-warnings=0
```

Expected: PASS.

- [ ] **Step 3: Run formatting**

Run:

```bash
gofmt -w controlplane/internal/storage/store.go controlplane/internal/storage/nodes_test.go controlplane/internal/server/enrollment.go controlplane/internal/server/enrollment_test.go controlplane/internal/server/heartbeat.go controlplane/internal/server/heartbeat_test.go controlplane/internal/server/server.go controlplane/internal/server/server_test.go cmd/nodeagent/join.go
```

Expected: command exits 0.

- [ ] **Step 4: Review git diff for scope**

Run: `git diff --stat HEAD`

Expected: diff contains only target metadata, heartbeat/enrollment, API serialization, and onboarding UI files from this plan.

- [ ] **Step 5: Commit any regression fixes**

If formatting or small regression fixes changed files:

```bash
git add controlplane/internal ui/src cmd/nodeagent
git commit -m "test: verify universal target compatibility"
```

If no files changed, do not create an empty commit.

---

## Final Verification

- [ ] Run full backend compile-focused check:

```bash
go test ./controlplane/internal/storage ./controlplane/internal/server ./cmd/nodeagent -count=1
```

- [ ] Run frontend checks:

```bash
npm --prefix ui test -- Onboard.test.tsx Nodes.test.tsx FleetEnroll.test.tsx
npm --prefix ui run lint -- --max-warnings=0
```

- [ ] Confirm `git status --short` contains only unrelated pre-existing untracked files or is clean.

- [ ] Update `docs/bank-sales-go-live-issue-log.md` only if the project convention requires a live validation note for this change. If updated, keep it factual and include the exact tests run.

---

## Spec Coverage Review

This plan covers the first shippable slice of the approved design:

- Stable identity vs IP observation: Tasks 1, 3, 4, and 5.
- Agent-managed-only scope: Tasks 1, 2, 4, and 6.
- Dynamic reachability metadata: Tasks 1, 3, 4, and 5.
- Easier onboarding language across machine types: Task 6.
- Backward compatibility for older agents: Task 3 and Task 7.
- Capability-aware UI groundwork: Task 4 exposes metadata; later work can gate every action by capability.

Deferred follow-up work from the design:

- A dedicated `/api/v1/targets` facade.
- Operator target-type override workflow.
- Heuristic target classification from richer inventory.
- Full scenario-first onboarding UI with a local installer path.
- Historical network observation table if labels become too small for long histories.
