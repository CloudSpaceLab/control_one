# Sprint 6 Event-Capture Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the missing event-capture layer that lets chat-first investigations explain fast health degradation with time-windowed traffic, resource, file-growth, and log evidence.

**Architecture:** Keep Sprint 5's tool-use loop as the query surface. Sprint 6 adds new durable evidence sources below it: agent collectors, Postgres/Doris rollups, investigation tools, and a root-cause synthesis table. All remediation remains gated through existing approval paths.

**Tech Stack:** Go nodeagent collectors, Go controlplane APIs/workers, Postgres metadata, Doris high-volume events, existing `/ai/ask` investigation tool registry, existing audit log and approval primitives.

---

## Phase 1: Flow-Rate and Bandwidth Rollups

**Goal:** Answer "what changed on this port/process in the last N minutes?"

**Tasks:**
1. Add nodeagent flow samples for connection count, connection rate, bytes in/out, process, local port, remote ASN/country where available.
2. Write raw high-cardinality samples to Doris.
3. Write compact per-minute rollups keyed by tenant, node, process, port, direction.
4. Add `GET /api/v1/investigate/nodes/{id}/flow-delta?since=&until=` and a matching AI tool.
5. Add tests for cps deltas such as 15 cps to 30 cps and 2 TB transfer spikes.

**Acceptance:** `/ai/ask` can cite a flow-delta tool result that states previous/current connection rate and bytes transferred for a node, port, and process.

## Phase 2: File Growth Watcher

**Goal:** Detect logs or files growing fast enough to degrade disk health.

**Tasks:**
1. Add nodeagent config for watched paths, defaulting to common app/log directories with size and privacy exclusions.
2. Emit file size snapshots with inode, path hash, redacted basename, size, and mtime.
3. Store raw snapshots in Doris and compact growth rollups in Postgres.
4. Add `file_growth_delta` investigation tool with top growing files over a window.
5. Add tests for 30 MB to 13 GB growth in eight minutes.

**Acceptance:** Chat can answer which files grew fastest and cite starting size, ending size, and rate.

## Phase 3: Resource Delta Tool

**Goal:** Compare CPU, memory, disk, and load between two times.

**Tasks:**
1. Normalize existing telemetry metrics into a time-window delta query.
2. Add `resource_delta` investigation tool.
3. Include before, after, absolute delta, percentage delta, and sample count.
4. Degrade cleanly when a metric is missing.

**Acceptance:** Chat can cite CPU 20% to 99%, memory 60% to 99%, and disk growth over the same incident window.

## Phase 4: Log-Tail Tool

**Goal:** Let investigations read relevant app/db/system log lines without dumping raw logs into the base prompt.

**Tasks:**
1. Add bounded Doris log-tail query by tenant, node, source, time window, and search term.
2. Enforce max line count, max bytes, and redaction before returning data.
3. Add `log_tail` AI tool with citation IDs per returned excerpt.
4. Audit every log-tail tool call.

**Acceptance:** Chat can cite a small set of log lines explaining a spike while preserving tenant scope and redaction.

## Phase 5: Root-Cause Synthesizer

**Goal:** Collapse related evidence into one incident verdict row.

**Tasks:**
1. Add a `root_cause_findings` table with tenant, node, window, summary, confidence, evidence IDs, and created_at.
2. Build a worker that joins flow deltas, resource deltas, file growth, alerts, and log tails into candidate findings.
3. Expose `root_cause_findings` as an investigation tool.
4. Keep the synthesizer deterministic first; LLM wording stays in `/ai/ask`.

**Acceptance:** A known incident fixture produces one finding that points to the same evidence a human would inspect.

## Phase 6: Safe Auto-De-Escalation

**Goal:** Propose and gate remediation for runaway logs, connections, or processes.

**Tasks:**
1. Add proposal-only actions for log truncation/rotation, rogue connection block, and rogue process kill.
2. Reuse Sprint 5 operator-mode proposal and existing approval gates.
3. Require explicit confirmation, role, tenant, node, and policy gate before any mutation.
4. Add dry-run output for every proposed action.
5. Audit proposals, approvals, denials, and execution results.

**Acceptance:** Chat may propose a safe action with evidence, but cannot execute without the existing gate and explicit confirmation.

## End-to-End Exit Test

Seed one incident window where:

1. Port 80 doubles from 15 to 30 cps.
2. Bandwidth reaches 2 TB in the spike window.
3. CPU moves from 20% to 99%.
4. Memory moves from 60% to 99%.
5. Three log files grow from 30 MB to 13 GB.
6. App/db log lines identify the cause.

Then ask `/ai/ask` what happened. The answer must cite flow delta, resource delta, file-growth, log-tail, and root-cause tool results, and it must only propose remediation through the operator-mode gate.
