# Control One — Node Agent (Living Document)

**Status:** living doc — updated whenever the agent's surface area changes
**Last updated:** 2026-05-09
**Owners:** Control One platform team
**Source of truth:** the code wins; this doc gets corrected, never the other way around
**How to update:** when you add a collector, change the heartbeat envelope, add a Doris ingest path, or land a new action handler, append a row in the relevant catalogue table below and bump `Last updated`. Update the ASCII diagrams when a *stage* is added or removed.

---

## 0. Why this exists

The c1 agent is the primary orchestration tool for everything Control One does on a host: telemetry capture, event emission, action execution. Before this doc, that surface was scattered across `cmd/nodeagent/`, `internal/`, `controlplane/internal/server/`, and `controlplane/internal/doris/migrations/`. New work kept duplicating discovery — including, recently, an investigation event-capture sprint that proposed "new" collectors when fsnotify and a time-series Doris schema already existed.

This doc is the **map**: every collector, every emitted event/metric name, every Doris table that receives agent data, every detector that fires, every action the agent can execute. Read this *before* proposing new agent capabilities.

---

## 1. The flow (high-level)

The agent is one stage in a six-stage pipeline. The **first three stages exist today**; the last three are partial or missing and are the subject of Sprint 6 in [`pr51-closure-timeline.md`](./pr51-closure-timeline.md).

```
  ┌─────────────────────────────────────────────────────────────────────────┐
  │                         END-TO-END INVESTIGATION FLOW                   │
  └─────────────────────────────────────────────────────────────────────────┘

   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
   │  STAGE 1     │    │  STAGE 2     │    │  STAGE 3     │
   │  COLLECTION  │───▶│  TIME-GRAPH  │───▶│  DETECTION   │  ✅ EXISTS
   │  (agent)     │    │  (Doris)     │    │ (controlplane│
   │              │    │              │    │  detectors)  │
   └──────────────┘    └──────────────┘    └──────┬───────┘
                                                  │
                                                  ▼
   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
   │  STAGE 6     │    │  STAGE 5     │    │  STAGE 4     │
   │  ACTION /    │◀───│  RCA         │◀───│  BACK-WALK + │  ⚠️ PARTIAL/MISSING
   │  DE-ESCALATE │    │  SYNTHESIS   │    │  CROSS-REF   │
   │              │    │              │    │  (20–30 min  │
   │              │    │              │    │   window)    │
   └──────────────┘    └──────────────┘    └──────────────┘
```

**Stages today:**
| Stage | State | Where it lives |
|---|---|---|
| 1. Collection | ✅ Rich | `cmd/nodeagent/`, `internal/` (8 collector packages) |
| 2. Time-graph storage | ✅ Time-series schema with retention | `controlplane/internal/doris/migrations/0001_events_pipeline.up.sql` (5 tables + 1 MV) |
| 3. Detection | ✅ 8 detectors firing | `controlplane/internal/server/events_anomaly.go:22-348` |
| 4. Back-walk + cross-ref | ⚠️ Partial — time-window query exists, **no rolling-window MVs**, **no cross-ref engine** | `controlplane/internal/server/investigate.go:288-389` |
| 5. RCA synthesis | ❌ Missing — detectors fire independently, no synthesizer | (gap) |
| 6. Action / de-escalate | ⚠️ Partial — only `firewall.rule_add/delete` and remediation scripts | `cmd/nodeagent/firewall_exec.go:96-160`, `internal/remediation/engine.go:49-100` |

Sprint 6 in the closure timeline is **architectural refactor**, not new features: extend the collectors that already exist, add Doris MVs over data the schema already carries, expose a synthesizer over the events that already fire, and broaden the action surface beyond firewall rules.

---

## 2. STAGE 1 — Collection (current state)

The agent runs 8 collectors in parallel, dispatched from `cmd/nodeagent/main.go:204-232`. Each emits typed events into a shared `eventstream.Batcher` that ships to the controlplane.

### Current architecture

```
                              ┌───────────────────────────────┐
                              │       c1 node agent           │
                              │  cmd/nodeagent/main.go        │
                              └─────────────┬─────────────────┘
                                            │
                ┌───────────┬────────────┬──┴──┬──────────┬──────────┬──────────┐
                ▼           ▼            ▼     ▼          ▼          ▼          ▼
          ┌─────────┐ ┌─────────┐ ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐
          │ procmon │ │ netflow │ │file-    │  │ dbquery │  │firewall │  │telemetry│
          │         │ │         │ │access   │  │         │  │         │  │/logs    │
          ├─────────┤ ├─────────┤ ├─────────┤  ├─────────┤  ├─────────┤  ├─────────┤
          │ 30s     │ │ eBPF    │ │ eBPF    │  │ MySQL   │  │ ufw /   │  │fsnotify │
          │ snap    │ │ tcplife │ │ LSM /   │  │ PG /    │  │firewalld│  │+ poll   │
          │ top-K   │ │ /proc/  │ │auditd / │  │ MSSQL / │  │/nft/    │  │ tail    │
          │ proc    │ │net/tcp  │ │ ETW /   │  │ Oracle  │  │iptables │  │ log     │
          │         │ │ Win/Mac │  │ fs_usage│  │         │  │         │  │ lines   │
          └────┬────┘ └────┬────┘ └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘
               │           │           │            │            │            │
               │           │           │            │            │            │
               ▼           ▼           ▼            ▼            ▼            ▼
            proc.exec  conn.open   file.open    db.query   firewall_   log lines
            proc.exit  conn.close  file.write              state in    (rawlog)
            proc.usage conn.summary file.unlink            heartbeat
                                   file.rename
                                   file.write.summary
                                          │
                                          ▼
                                ┌────────────────────┐                ┌──────────────┐
                                │ eventstream.Batcher│   heartbeat ──▶│ inventory +  │
                                │ (in-process)       │                │ firewall +   │
                                └─────────┬──────────┘                │ completed    │
                                          │                           │ actions      │
                                          ▼                           └──────┬───────┘
                                /api/v1/events/ingest                        │
                                          │                                  ▼
                                          │                       /api/v1/agent/heartbeat
                                          ▼                                  │
                                  ┌──────────────────────────────────────────┘
                                  ▼
                           controlplane (Stage 2 → Doris)
```

### Collector catalogue

| Collector | Cadence | Emits | OS support | File |
|---|---|---|---|---|
| **procmon** | 30 s snapshot (configurable) | `proc.exec` (first-seen), `proc.exit`, `proc.usage` (top-20 by CPU+mem). Carries xxhash64 of exe + cmdline + UID | All | `internal/procmon/collector.go:1-15`, `:168` |
| **netflow** | per-conn lifecycle (eBPF real-time / poll fallback) | `conn.open`, `conn.close`, `conn.summary` (per-(PID, port, minute)). Carries threat-intel match, bytes delta | Linux ≥5.4 (eBPF) → /proc/net/tcp; Windows Get-NetTCPConnection; macOS lsof+nettop | `internal/netflow/collector.go:1-14, 162-237` |
| **fileaccess** | per-access + 5 s aggregation bucket | `file.open / read / write / unlink / rename` + `file.write.summary`. Smart-filtered to `/etc, /var/lib, /var/log, /opt, /home, /root, /tmp/sensitive` | Linux ≥5.7 (eBPF LSM) → auditd; Windows ETW; macOS fs_usage | `internal/fileaccess/collector.go:1-14` |
| **dbquery** | per-query | `db.query` (engine, db, user, query_hash, rows_affected, exec_time_ms) | All | `internal/dbquery/collector.go` |
| **inventory** | heartbeat-gated (24 h or hash change) | OS package list, kernel, OS version | All | `cmd/nodeagent/inventory.go:30`, `heartbeat.go:240-291` |
| **firewall** | every heartbeat | Firewall rule snapshot | All | `cmd/nodeagent/firewall.go` |
| **securityfacts** | optional | Security posture data | All | `internal/securityfacts/collector.go` |
| **telemetry/logs** | fsnotify + timer poll | `RawLog` events with timestamp + message; **fsnotify already in use** | All | `internal/telemetry/logs/collector_file.go:13, 34, 40-44, 110-118` |

### What's NOT collected today

- **CPU / memory / disk metrics** at the host level — only per-process via procmon. There's no `cpu_usage_pct` / `memory_used_pct` / `disk_usage_pct` host emitter.
- **SMART / PSI / OOM** signals — `node_predictive` reads names that no collector emits (this is the calibration bug, S4 `c1-calibration-metric-contract`).
- **File-system *size* or *growth-rate*** — fsnotify watches log files for *content* tailing only; no growth-rate metric exists. (Sprint 6 `c1-fs-watcher` extends this collector to emit `fs.size.bytes` + `fs.growth_rate.bytes_per_sec`.)
- **ICMP latency / packet loss** — no probe collector.

### Heartbeat envelope (size-bounded)

`cmd/nodeagent/heartbeat.go:25-45`:

```go
type heartbeatPayload struct {
    AgentVersion     string             `json:"agent_version"`
    OSPackages       []PackageInfo      `json:"os_packages,omitempty"`
    PackageHash      string             `json:"package_hash,omitempty"`
    PackageCount     int                `json:"package_count,omitempty"`
    KernelVersion    string             `json:"kernel_version,omitempty"`
    OSVersion        string             `json:"os_version,omitempty"`
    FirewallState    *FirewallState     `json:"firewall_state,omitempty"`
    CompletedActions []completedAction  `json:"completed_actions,omitempty"`
}
```

- Typical size ≤ 50 KiB. Full inventory resend (24 h or server-requested) can hit 500 KiB–2 MiB.
- **Metrics do not ride the heartbeat.** They go through a separate `/api/v1/events/ingest` endpoint via `eventstream.Batcher` (`cmd/nodeagent/main.go:210-213`).
- Bugs doc §6 #18 flags `MaxBytesReader = 64 KiB` on telemetry ingest — fine today; SMART per-disk + per-NIC will blow this when `c1-fs-watcher` and `c1-calibration-metric-contract` land.

---

## 3. STAGE 2 — Time-graph storage in Doris (current state)

All agent data lands in **time-partitioned Doris tables with retention**. Schema seeded by `controlplane/internal/doris/migrations/0001_events_pipeline.up.sql`.

### Current tables

| Table | Time column | Partition | Retention | Source events |
|---|---|---|---|---|
| `events` | `ts DATETIME(3)` | daily by `event_date` | 90 d | All event types — primary log |
| `process_connections` | `started_at DATETIME(3)` | daily by `event_date` | 60 d | `conn.open` + `conn.close` enriched pairs (direction, bytes_in/out, packets_in/out, threat_match, threat_score) |
| `process_lineage` | `observed_at DATETIME(3)` | daily by `event_date` | 30 d | `proc.exec` snapshots (ppid, exe_path, exe_hash, cmdline, uid, gid) |
| `file_accesses` | `ts DATETIME(3)` | daily by `event_date` | 90 d | `file.*` events (path, op, bytes, op_count, started_at, ended_at) |
| `db_queries` | `ts DATETIME(3)` | daily by `event_date` | 90 d | `db.query` events (engine, db, user, query_hash, rows_affected, exec_time_ms) |
| `events_per_hour_mv` | `hour_ts` | rolling MV | — | aggregate of `events` by (tenant_id, node_id, event_type, hour_ts) → COUNT, MAX(severity), SUM(bytes_in/out) |

**Key insight:** the time-graph the user wants **already exists** for connections, processes, file accesses, and DB queries. What's missing is:

1. **Rolling-window MVs at finer granularity than hourly.** `events_per_hour_mv` is the only rollup; there's nothing per-minute or per-port. (Sprint 6 `c1-flowrate-aggregator` adds 1m/5m/1h MVs over `process_connections`; `c1-bandwidth-rollups` adds bytes-in/out per-(node, port, window).)
2. **Metric-delta semantics.** Tables store *events*, not *metric values*. Asking "what was CPU at T0 vs T1" needs a `telemetry_metrics` table that doesn't yet exist for host-level metrics. (Sprint 6 `c1-resource-delta-tool` exposes the query; the metric emitters land in S4 `c1-calibration-metric-contract`.)
3. **A separate file/log-size stream.** Today `file_accesses` records file-write *events*, not file *size* or *growth rate* over time. (Sprint 6 `c1-fs-watcher` adds per-file size sampling at fsnotify-driven cadence.)

---

## 4. STAGE 3 — Detection (current state, 8 detectors)

Detection is server-side, in `controlplane/internal/server/events_anomaly.go:22-348`. **Detectors fire independently** — there is no synthesizer that says "these 3 anomalies are facets of one root cause."

| # | Detector | Triggers on | What it emits | File:line |
|---|---|---|---|---|
| F.1 | First-seen destination | `conn.open` | `anomaly.new_destination` (high if threat_feed/score > 0) | `:56-96` |
| F.2 | Long connection | `conn.close` | `anomaly.long_connection` (3× p95 of (tenant, dst_ip, dst_port), min 50 samples) | `:99-134` |
| F.4a | High bytes out | `conn.close` | `anomaly.high_bytes_out` (5× p95 of (tenant, process_name, dst_port), min 30 samples) | `:138-162` |
| F.4b | Fast bulk transfer | `conn.close` | `anomaly.fast_bulk_transfer` (≥100 MiB in <5 s) | `:164-182` |
| F.4c | Packet scan | `conn.close` | `anomaly.packet_scan` (>10k packets, <60 B/packet) | `:184-208` |
| F.3a | New executable | `proc.exec` | `anomaly.new_executable` (high if /tmp, /dev/shm, /var/tmp) | `:212-248` |
| F.3b | Executable dropped | `file.write.summary` | `anomaly.executable_dropped` (write to executable prefixes ≥1 KiB) | `:250-297` |
| F.5 | DB query anomaly | `db.query` | `anomaly.new_db_query`, `anomaly.db_query_high_rows` (5× historical max) | `:300-348` |

**What's NOT detected today:**
- File-system growth-rate spikes (no `anomaly.fs_runaway_writer`)
- Per-port traffic-rate doubling (no `anomaly.port_flow_spike`)
- Resource-pressure cascades (no `anomaly.resource_saturation`)
- Cross-anomaly synthesis (no `anomaly.root_cause` row)

---

## 5. STAGE 4 — Back-walk + cross-reference (PARTIAL, the refactor target)

### What exists today

`controlplane/internal/server/investigate.go`:

- `GET /api/v1/investigate/search?q=&types=` — entity search, no time window (`:79-150+`)
- `GET /api/v1/entities/{type}/{id}/lifecycle?since=&until=` — **time-window query** (`:288-334`). Accepts RFC3339, returns events in window with pagination.
- `GET /api/v1/entities/{type}/{id}/related` — co-occurring entities, **hardcoded 24 h window** (`:349-389`)

The 20–30 min "back-walk" the user describes is **already implementable** via `lifecycle?since=now-30m&until=now`. What's missing is the *automation* — the synthesizer that, on every anomaly emit, walks back and stitches signals.

### What's missing (the cross-reference engine)

The synthesizer needs:
1. **Per-port flow-rate window query** — give me `cps` and `bytes/s` for each port between T0 and T1
2. **Per-file growth-rate window query** — give me `growth_rate` for each watched file between T0 and T1
3. **Resource snapshot at T0 vs T1** — give me `(cpu, mem, disk_pct, load)` for the node at both timestamps and the delta
4. **Recent log tail by file path** — give me the last N MB of `/var/log/app.log` since T0, redacted
5. **Cross-entity dedup** — multiple anomalies caused by one event (e.g. F.4a + F.4b + F.1 all fire on the same exfiltration) should fold into one investigation row

(1)–(4) are **direct extensions** of the time-window query that already exists — new MVs and a new tool surface, not a new architecture. (5) is genuinely new — needs a correlator over the anomaly stream.

---

## 6. STAGE 5 — RCA synthesis (MISSING)

There is no synthesizer today. Anomalies surface raw to the operator UI; the LLM in `ai_ask.go` can call `lifecycle` and `related` but only does so single-shot, with no `tool_use` loop and no automated invocation on anomaly emit.

The Sprint 5 chain (`c1-mcp-wrapper` → `c1-tooluse-loop` → `c1-operator-mode`) gets the LLM into the loop. Sprint 6's `c1-root-cause-synth` is the worker that *automatically* triggers the loop on every severity-≥-high anomaly emit, fans out to the 5 capture surfaces above, and writes a synthesized verdict.

This is the only **net-new** architectural component. All five capture surfaces are extensions of existing data; the synthesizer itself is the new orchestration layer.

---

## 7. STAGE 6 — Action / de-escalation (PARTIAL)

### What the agent can do today

| Action | Surface | Safety |
|---|---|---|
| `firewall.rule_add` / `firewall.rule_delete` | `cmd/nodeagent/firewall_exec.go:96-160`. Backends: ufw → firewalld → nftables → iptables (Linux); netsh (Windows) | Job detail fetched from controlplane before exec; completion queued for next heartbeat |
| Remediation script execution (bash / PowerShell / Ansible) | `internal/remediation/engine.go:49-100`. Configurable timeout (default 5 min). Env passthrough. | Script signed + checksum verified (Sprint 3 PR #19); approval gate exists but mis-default-blocked (S4 `c1-patch-approval-gate`) |

### What the agent CANNOT do today

- **Truncate a log file** with archive-to-S3
- **Kill a process by PID** (with PID-allowlist, no SIGKILL on PPID 1)
- **Drop a TCP connection / kill an in-flight session** (the existing autoblock fans out *firewall rules* — that's prevention, not termination of an open conn)
- **Auto-run any of the above** without operator-issued job

Sprint 6 `c1-auto-deescalate` adds these three actions as new agent capabilities, gated by `tenant.auto_deescalate=true` (default OFF), with safety gates that reuse the Sprint-2 blast-radius circuit breaker pattern.

---

## 8. Target architecture (the refactor)

The dotted boxes are net-new; everything solid is extension or already exists.

```
                              ┌───────────────────────────────┐
                              │       c1 node agent           │
                              └─────────────┬─────────────────┘
                                            │
   ┌──────────┬──────────┬──────────┬───────┴──┬──────────┬──────────┬──────────┐
   ▼          ▼          ▼          ▼          ▼          ▼          ▼          ▼
 ┌────┐    ┌────┐    ┌────┐    ┌────┐    ┌────┐    ┌────┐    ┌────┐    ┌────┐
 │proc│    │net │    │file│    │db  │    │fw  │    │tlmy│    │NEW:│    │NEW:│
 │mon │    │flow│    │acc │    │qry │    │    │    │/log│    │ fs-│    │host│
 │    │    │    │    │    │    │    │    │    │    │    │    │watch    │mtrx│
 │    │    │    │    │    │    │    │    │    │    │    │    │(size,   │(cpu│
 │    │    │    │    │    │    │    │    │    │    │    │    │ growth) │ mem│
 │    │    │    │    │    │    │    │    │    │    │    │    │         │ etc│
 └─┬──┘    └─┬──┘    └─┬──┘    └─┬──┘    └─┬──┘    └─┬──┘    └─┬──┘    └─┬──┘
   │         │         │         │         │         │         │         │
   ▼         ▼         ▼         ▼         ▼         ▼         ▼         ▼
   ════════════════ eventstream.Batcher (existing) ══════════════════════════
                                            │
                                            ▼
                                /api/v1/events/ingest (existing)
                                            │
                                            ▼
   ┌─────────────────────── DORIS TIME-GRAPH (Stage 2) ──────────────────────┐
   │                                                                          │
   │  EXISTING tables:                                                        │
   │   • events / process_connections / process_lineage / file_accesses /     │
   │     db_queries (all time-partitioned, retention 30–90 d)                 │
   │   • events_per_hour_mv                                                   │
   │                                                                          │
   │  NEW MVs (Sprint 6 — extension, not new architecture):                   │
   │   • flow_rate_per_port_1m / _5m / _1h ◀── c1-flowrate-aggregator         │
   │   • bandwidth_per_port_1m / _5m / _1h ◀── c1-bandwidth-rollups           │
   │   • file_growth_rate_1m                ◀── c1-fs-watcher (Doris side)    │
   │                                                                          │
   │  NEW table:                                                              │
   │   • investigation_events                ◀── c1-root-cause-synth          │
   │     (one row per synthesized RCA verdict)                                │
   └────────────────────┬─────────────────────────────────────────────────────┘
                        │
                        ▼
       ┌───── STAGE 3 — DETECTION (existing 8 detectors) ──────┐
       │ events_anomaly.go: F.1, F.2, F.3a, F.3b, F.4a, F.4b,  │
       │                    F.4c, F.5                          │
       │ NEW: F.6 fs_runaway_writer (over file_growth_rate_1m) │
       │ NEW: F.7 port_flow_spike  (over flow_rate_per_port_*) │
       └────────────────────┬──────────────────────────────────┘
                            │  anomaly emit (severity ≥ high)
                            ▼
       ┌─── STAGE 4 — BACK-WALK + CROSS-REF (refactor) ─────────┐
       │                                                         │
       │  c1-operator-mode worker (S5)                           │
       │      ▼                                                  │
       │  c1-root-cause-synth (S6)                               │
       │      ├── window = [now − 20m, now]                      │
       │      └── fan-out via MCP tools (existing tool_use):     │
       │                                                          │
       │  ┌──────────────┬──────────────┬────────┬──────────┐    │
       │  ▼              ▼              ▼        ▼          ▼    │
       │ c1_flow_rate   c1_metric_     c1_log_  c1_fs_     c1_   │
       │ _query (NEW)   delta (NEW)    tail     growth     entity│
       │                                (NEW)   (NEW)      _life │
       │                                                  cycle  │
       │                                                  (exists)│
       │                                                         │
       │  All five tools ride the existing                       │
       │  `c1_*` MCP surface from S5 (extension, not new layer). │
       │                                                         │
       │  Dedup: anomalies with overlapping (node, time-window)  │
       │  fold into one investigation_events row.                │
       └────────────────────┬────────────────────────────────────┘
                            │
                            ▼
       ┌─── STAGE 5 — RCA SYNTHESIS (new orchestration) ────────┐
       │                                                         │
       │   Google Gemini 2.5 Pro long-context pass              │
       │   (Anthropic Opus 4.7 fallback)                        │
       │                                                         │
       │   inputs:                                               │
       │    • the original anomaly emit                          │
       │    • 5 capture-surface JSON blobs                       │
       │    • redacted log tails (per-tool RBAC)                 │
       │                                                         │
       │   outputs (single row):                                 │
       │    • timeline                                           │
       │    • verdict (string)                                   │
       │    • confidence (float)                                 │
       │    • recommended_action (typed)                         │
       └────────────────────┬────────────────────────────────────┘
                            │
                            ▼
                ┌───── tenant.auto_deescalate ? ─────┐
                │                                    │
        false   │                                    │  true
        ┌───────┘                                    └──────────┐
        ▼                                                        ▼
   ┌──────────────┐                          ┌─────────────────────────────┐
   │ ALERT-ONLY:  │                          │  STAGE 6 — ACTION (refactor) │
   │ verdict on   │                          │                              │
   │ /ai/ask +    │                          │  EXISTING:                   │
   │ node UI      │                          │   • firewall.rule_add/delete │
   │ webhook fires│                          │   • script execute           │
   │ no action    │                          │                              │
   └──────────────┘                          │  NEW (c1-auto-deescalate):   │
                                             │   • smart log truncation     │
                                             │     (archive→S3, truncate,   │
                                             │      re-open file handles)   │
                                             │   • rogue-process kill       │
                                             │     (SIGTERM→SIGKILL, PID-   │
                                             │      allowlist, never PPID 1)│
                                             │   • rogue-conn drop          │
                                             │     (extends autoblock to    │
                                             │      kill in-flight session) │
                                             │                              │
                                             │  SAFETY GATES (reuses S2):   │
                                             │   • 1-host canary required   │
                                             │   • blast-radius CB          │
                                             │   • verdict confidence ≥0.85 │
                                             │   • action ∉ deny-list       │
                                             └──────────────┬───────────────┘
                                                            │
                                                            ▼
                                          ┌─────────────────────────────┐
                                          │ POST-ACTION VERIFICATION    │
                                          │   • disk_pct re-check       │
                                          │   • cps re-baseline         │
                                          │   • CPU/MEM re-baseline     │
                                          │   • append to               │
                                          │     investigation_events    │
                                          │   • fire webhook outbox     │
                                          └─────────────────────────────┘
```

---

## 9. Refactor scope (cross-references Sprint 6)

Comparing what each Sprint 6 worktree actually does against existing plumbing:

| Sprint 6 worktree | True scope | Existing plumbing it extends |
|---|---|---|
| `c1-fs-watcher` | **Extension.** Add a new emitter inside the existing `internal/telemetry/logs/collector_file.go` that polls file size at fsnotify-driven cadence and emits `fs.size.bytes` + `fs.growth_rate.bytes_per_sec` events. | fsnotify watcher already running. Add a Doris MV `file_growth_rate_1m`. |
| `c1-flowrate-aggregator` | **Extension.** New Doris MV over the existing `process_connections` table. No agent change. | Schema exists; just add the rollup. |
| `c1-bandwidth-rollups` | **Extension.** New Doris MV over `process_connections`. Tighter granularity than `events_per_hour_mv`. | Same as above. |
| `c1-resource-delta-tool` | **Extension.** New MCP tool that wraps a window query. Once `c1-calibration-metric-contract` (S4) lands the missing host metrics, the tool is a thin lookup. | Time-window query path in `investigate.go` already exists. |
| `c1-log-tail-tool` | **Extension** with redaction layer. Logs are already collected and ingested. The tool exposes a query endpoint with per-tool RBAC + PII regex denylist. | `telemetry/logs/collector_file.go` ingests; new query handler reads. |
| `c1-root-cause-synth` | **Net-new orchestration.** No correlator/synthesizer exists. New table `investigation_events`, new worker subscribing to anomaly emits, new fan-out via MCP tools. | Reuses S5's `c1-operator-mode` trigger and tool_use loop. |
| `c1-auto-deescalate` | **Net-new agent capability.** Three new actions (truncate, kill-proc, kill-conn) — none of which exist on the agent today. Safety-gate framework reuses Sprint 2 patterns. | Reuses `internal/remediation/engine.go` for execution shape and `internal/autoblock/` for fan-out plumbing. |

**Conclusion:** 5 of 7 Sprint 6 worktrees are *extensions* of existing collectors, schema, or query surface. Only 2 (`c1-root-cause-synth`, `c1-auto-deescalate`) are genuinely new architectural components. This validates "architectural refactor, not new features."

---

## 10. How to keep this doc current

- **When you add a collector**, append a row to §2's catalogue and update the ASCII fan-out in §2.
- **When you change the heartbeat envelope**, update §2's struct snippet.
- **When you add a Doris ingest path**, append a row to §3's table list.
- **When you add an anomaly detector**, append a row to §4's table.
- **When you broaden the action surface**, update §7.
- **When the target architecture in §8 lands**, move the dotted boxes to solid and bump the stages-status table in §1.

This doc is one file. PR review for any agent-touching change should require this doc to be updated in the same PR.

---

## See also

- [`pr51-closure-timeline.md`](./pr51-closure-timeline.md) — the Sprint 4–8 delivery plan
- [`gaps-vs-probo-holmesgpt.md`](./gaps-vs-probo-holmesgpt.md) — strategic gap analysis (the 3-pillar lens)
- [`incomplete-features-and-bugs.md`](./incomplete-features-and-bugs.md) — bug audit citing the 21+ findings
