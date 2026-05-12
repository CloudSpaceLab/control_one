# Chat-First Investigation Implementation Plan

**Goal:** Turn `/ai/ask` from a single-shot knowledge-graph prompt into a tool-using investigation assistant that can answer operator questions with current node, alert, firewall, process, package, and connection evidence.

**Architecture:** Keep the existing REST investigation endpoints as the source of truth. Add a thin internal tool registry around those handlers, expose tool schemas to the LLM router, and run a bounded tool-use loop from `controlplane/internal/server/ai_ask.go`. The first milestone should use existing data; Sprint 6 event-capture work comes after the tool loop is proven.

**Tech Stack:** Go controlplane, existing storage interfaces, current `/api/v1/investigate/*` handlers, provider router abstraction for Anthropic/OpenAI/Google, React UI only where citations or streaming need visible support.

---

## Phase 1: LLM Router

**Scope:** Add a provider-neutral server-side interface for chat completions and tool calls.

**Implementation tasks:**
1. Create `controlplane/internal/llm` with request, message, tool, tool call, and response structs.
2. Add provider adapters behind buildable interfaces; keep env-driven provider selection isolated from request handlers.
3. Add fake provider tests before real provider wiring.
4. Keep existing `/ai/ask` behavior as the fallback path when no tool-capable provider is configured.

**Acceptance:**
```bash
go test ./controlplane/internal/llm ./controlplane/internal/server -run 'AIAsk|LLMRouter' -count=1
```

## Phase 2: Internal Investigation Tool Registry

**Scope:** Wrap existing investigation capabilities as typed tools without inventing a separate service protocol yet.

**Implementation tasks:**
1. Create tool definitions for node documentation, alerts, firewall state, node services, packages, connections, and recommendations.
2. Add a registry that validates tenant and node scope before every tool call.
3. Reuse storage-layer calls directly where possible; do not route through HTTP internally.
4. Return compact JSON payloads with stable citation IDs.

**Acceptance:**
```bash
go test ./controlplane/internal/server -run 'InvestigationTool|NodeDocumentation' -count=1
```

## Phase 3: Tool-Use Loop in `/ai/ask`

**Scope:** Let the model choose tools, execute them, then synthesize a final answer with citations.

**Implementation tasks:**
1. Replace single-shot prompt execution in `ai_ask.go` with a bounded loop.
2. Cap loop depth at 12 tool calls and wall time at 60 seconds.
3. Preserve the compressed knowledge graph as the initial context, not the only context.
4. Log tool names, durations, result sizes, and failures without logging secrets.
5. On tool failure, let the model continue with an explicit unavailable-evidence citation.

**Acceptance:**
```bash
curl -sS -X POST "$CONTROLONE_URL/api/v1/ai/ask" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"question":"For node NODE_ID, explain the likely cause of health degradation using services, firewall state, alerts, and packages."}'
```

The response must show at least two tool calls in server logs and include citations tied to tool result IDs.

## Phase 4: Citations and Streaming

**Scope:** Make investigation answers auditable and usable during longer tool chains.

**Implementation tasks:**
1. Add a response shape containing `answer`, `citations`, `tool_trace`, and `confidence`.
2. Keep the old response fields compatible for existing UI calls.
3. Add optional SSE streaming for token and tool-status events.
4. Update UI rendering only after the API contract is tested.

**Acceptance:**
```bash
go test ./controlplane/internal/server -run 'AIAskCitation|AIAskStream' -count=1
npm run test --prefix ui -- AI
```

## Phase 5: Tool RBAC and Audit

**Scope:** Prevent chat from bypassing existing operator/admin boundaries.

**Implementation tasks:**
1. Assign every tool a minimum role.
2. Enforce tenant scoping and node ownership before execution.
3. Add audit events for tool calls that read sensitive evidence.
4. Deny mutation tools in the first milestone; operator-mode actions get a separate gate.

**Acceptance:**
```bash
go test ./controlplane/internal/server -run 'AIAskRBAC|InvestigationToolRBAC|Audit' -count=1
```

## Phase 6: Operator Mode

**Scope:** Add safe, gated remediation only after read-only tool-use is stable.

**Implementation tasks:**
1. Define operator-mode intent classes: explain, recommend, propose action, execute action.
2. Require explicit user confirmation and existing approval gates before any mutation.
3. Start with patch approval and firewall actions already represented in the product.
4. Record action proposals and outcomes as audit events.

**Acceptance:** A chat-initiated action must be impossible without role, tenant, confirmation, and existing gate checks.

## Parallel Epic: CVE/KEV Enrichment

**Scope:** Keep vulnerability intelligence parallel to chat tooling; do not block the tool-use loop on it.

**Implementation tasks:**
1. Preserve package inventory from node detail and patch flows.
2. Add CVE, KEV, OSV, and Trivy detail enrichment as a separate storage path.
3. Expose vulnerability lookup as a later investigation tool once data quality is proven.

**Acceptance:** CVE/KEV data can enrich chat answers, but `/ai/ask` remains useful without it.

## Architectural Exit Test

A single `/ai/ask` request about one unhealthy node must:

1. Preserve the exact node match from the compressed knowledge graph.
2. Execute at least two investigation tools.
3. Cite tool results in the final answer.
4. Respect tenant and role boundaries.
5. Degrade cleanly when one tool returns no data.
