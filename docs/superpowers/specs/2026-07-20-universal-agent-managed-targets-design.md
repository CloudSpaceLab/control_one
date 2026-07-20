# Universal Agent-Managed Targets Design

Date: 2026-07-20

## Status

Approved direction: Control One will support only agent-managed machines for now, while making that support robust across personal PCs, workstations, laptops, servers, VMs, cloud instances, and restricted-network machines. Static IP addresses must not be required for onboarding, identity, management, or ongoing health.

## Problem

Control One already has strong agent foundations: enrollment tokens, machine ID dedupe, heartbeat capability labels, fleet enrollment over SSH, offline bundles, repair flows, service inventory, compliance scans, telemetry, and private-access imports. The remaining product gap is that onboarding and target language still imply a server with a reachable host or stable IP.

That model breaks down for:

- Personal PCs and laptops that roam between networks.
- Home or branch machines behind NAT.
- Cloud instances with changing public IPs.
- Servers reachable only through private networks, overlays, or outbound-only egress.
- Reimaged machines whose hostname changes but hardware or OS identity remains stable.
- Offline or restricted environments where a bundle must be carried to the target.

Control One needs a single target model where the agent identity is durable and network addresses are observations.

## Goals

- Treat every managed machine as an agent-managed target, not as an IP endpoint.
- Support personal PC endpoints, workstations, laptops, servers, VMs, and cloud instances through one model.
- Preserve static IP support as one reachability observation, not a requirement.
- Make onboarding easy regardless of whether the machine is local, remote, bulk-enrolled, air-gapped, or repaired.
- Keep the existing node agent and enrollment system as the management plane.
- Dispatch management jobs through agent polling and heartbeat, not inbound connection to target IP.
- Use capabilities and target classification to tailor UI actions and defaults.

## Non-Goals

- Agentless management of SaaS apps, databases, network devices, cloud APIs, or subnets.
- Full MDM replacement.
- Building a VPN or overlay network inside Control One.
- Replacing existing private-access provider imports.
- Requiring all target machines to have a public DNS name, public IP, or open inbound admin port.

## Core Design

Control One will formalize a universal agent-managed target layer over the existing node model.

The durable identity is the Control One target and enrolled node. IP addresses, hostnames, DNS names, overlay addresses, and remote admin reachability are changing facts about that target.

Existing `nodes` remain the primary implementation object initially. The first implementation can represent target fields as node columns plus labels. If the model grows beyond label ergonomics, a later migration can split durable target identity and current node enrollment into separate tables.

## Target Identity

Each enrolled machine should have stable identity fields:

- `target_id`: durable target identity. Initially this can be the current `node_id`.
- `node_id`: current enrolled agent identity.
- `tenant_id`: tenant boundary.
- `management_mode`: always `agent_managed` for this design.
- `target_type`: `personal_pc`, `workstation`, `laptop`, `server`, `vm`, `cloud_instance`, `domain_controller`, `kiosk`, or `unknown`.
- `lifecycle_state`: `invited`, `enrollment_pending`, `active`, `stale`, `roaming`, `repair_needed`, `retired`, or `enrollment_failed`.
- `identity_evidence`: machine ID, agent certificate fingerprint, hostname history, OS install ID where available, hardware/VM/cloud identifiers where available.
- `first_enrolled_at`, `last_seen_at`, `first_scan_at`, and `last_classified_at`.

Machine ID and agent certificate identity should be preferred for dedupe. Hostname should remain a fallback for legacy agents and a display hint, not a durable key.

## Reachability Model

Each target should have a computed `reachability_mode`:

- `outbound_only`: agent can reach Control One, but Control One should not assume inbound access.
- `direct_private`: target has a private address reachable from the operator/control-plane network.
- `direct_public`: target has a public address that may be reachable directly.
- `overlay`: target has an imported overlay/private-access address or peer identity.
- `offline_periodic`: target reports through bundle/manual sync patterns or is expected to be disconnected for long periods.
- `unknown`: insufficient evidence.

Reachability is derived from heartbeat observations, enrollment context, probe results, and optional private-access imports. It should not gate core agent jobs. Core jobs are queued on the control plane and fetched by the agent.

## Network Observations

The agent and control plane should track changing network facts separately from identity:

- Server-observed public IP on enrollment and heartbeat.
- Agent-reported local interface addresses.
- DNS suffixes and hostnames.
- Default route and NAT hints where available.
- Overlay/private-access address and peer ID where imported.
- ASN, country, and provider enrichment for public IPs.
- First seen, last seen, and confidence for each observation.

The UI should show "last seen from" and "current observed address" rather than implying that IP is the target's identity.

## Agent Enrollment Contract

The existing `/api/v1/enroll` contract should be extended compatibly. New fields must be optional.

Enrollment request additions:

- `install_context`: `local_interactive`, `remote_push`, `fleet_enroll`, `offline_bundle`, `repair`, or `unknown`.
- `target_hint`: optional user-selected target type.
- `network_observations`: local IPs, interface summaries, DNS suffixes, gateway hints.
- `device_evidence`: battery present, domain joined, virtualization hint, cloud metadata hint.
- `agent_capabilities`: initial capability list when available.

Heartbeat additions:

- Updated network observations.
- Target classification evidence.
- Capability inventory.
- Install/runtime context.
- Roaming hint when public network changes materially.

Compatibility rule: older agents continue to enroll and heartbeat successfully. The control plane classifies them as `unknown` target type with best-effort reachability.

## Target Classification

Control One should classify target type from evidence, not from onboarding path alone.

Likely server evidence:

- Server OS edition.
- Long uptime and no battery.
- Listening service inventory.
- Webserver/database/cache/message-queue service evidence.
- Cloud instance metadata.
- Domain controller role.

Likely personal endpoint evidence:

- Battery present.
- Desktop/workstation OS edition.
- Interactive user/session evidence if available.
- Roaming network changes.
- No server role evidence.

Classification should produce:

- `target_type`.
- `classification_confidence`.
- `classification_evidence`.
- `operator_override` support.

Operators can override target type when the heuristic is wrong. The override should be audited and visible.

## Capability-Based UX

The UI should show actions based on agent capabilities and target type.

Common actions:

- Compliance scan.
- Package/software inventory.
- Patch posture.
- Process telemetry.
- Agent repair/re-enrollment.
- Policy assignment.

Server-oriented actions:

- Service and listener inventory.
- Webserver inventory and remediation.
- Database/log source recommendations.
- Host firewall enforcement.
- Maintenance-window-aware patching.

Personal endpoint defaults:

- Lighter telemetry by default.
- Compliance and patch posture emphasized.
- No webserver/database remediation prompts unless the agent reports those capabilities.
- Roaming status visible.

This avoids splitting Control One into separate products while keeping the experience context-aware.

## Onboarding UX

The top-level onboarding page should move from "Add servers" to "Add machines" or "Add targets".

The first choice should be scenario-based:

- `Install on this machine`: copy/download a local installer command for the current OS where possible.
- `Install on another machine`: remote push with SSH or WinRM, using existing connection probe and fleet-enroll machinery.
- `Bulk enroll`: paste hosts, upload CSV, or use a generated script/token for many machines.
- `Offline or restricted network`: generate a signed offline bundle.
- `Repair existing agent`: issue a one-shot token and reinstall while preserving identity.

The UI should explain outcomes, not network assumptions:

- "The machine will appear after its first heartbeat."
- "No inbound access is required after the agent is installed."
- "IP addresses may change; Control One tracks the machine by agent identity."

RDP should remain a reachability probe only. Full Windows onboarding should use local installer, WinRM push, bulk script, or offline bundle.

## Onboarding Success Gate

A target is fully onboarded when:

- Enrollment succeeds.
- The node receives cert/token material.
- First heartbeat arrives.
- First inventory/compliance scan completes or reports a clear failure.
- Target type and reachability mode are computed.
- Capabilities are visible.

Existing `enrollment_pending` and `enrollment_failed` behavior should remain. The UI should make failures actionable: token expired, cannot reach control plane, service not running, first scan failed, duplicate identity, unsupported platform, or policy assignment failed.

## API Surface

Initial API changes can be additive:

- Extend node summary responses with `target_type`, `reachability_mode`, `management_mode`, `classification`, and `network_observations`.
- Add a target-focused list/filter facade if needed: `/api/v1/targets`.
- Extend onboarding protocols with scenario metadata.
- Extend enrollment token labels to capture onboarding scenario and intended target type.
- Extend heartbeat persistence to keep current and historical network observations.

The UI can initially consume enriched node responses to reduce migration risk.

## Data Migration

Migration should be low-risk:

- Existing nodes become `management_mode=agent_managed`.
- Existing nodes get `target_type=unknown` until classified.
- Existing public IP values become network observations.
- Existing enrollment labels remain intact.
- Static-IP workflows continue to work as direct reachability observations.

No existing enrolled agent should need to re-enroll for the initial rollout.

## Security

- Enrollment tokens remain time-bound and scoped.
- Repair tokens should be one-shot and audited.
- Remote-push credentials remain in memory only and must not be persisted.
- Operator target-type overrides are audited.
- Network observations should not expand authorization scope.
- Agent job dispatch remains tenant-scoped and capability-gated.
- Public IP enrichment is informational and must not become identity evidence.

## Error Handling

Onboarding and target pages should distinguish:

- Enrollment token invalid, expired, revoked, or exhausted.
- Control plane unreachable from target.
- Remote push could not reach SSH/WinRM.
- Remote credentials failed.
- Agent installed but service did not start.
- Agent heartbeat missing.
- First scan failed.
- Duplicate machine identity detected.
- Target appears to be roaming.
- Target is active but only outbound-reachable.

Each state should have a recommended next action.

## Testing Plan

Backend tests:

- Dynamic public IP changes update observations without creating duplicate nodes.
- Hostname changes with same machine ID update display fields without duplicating identity.
- Legacy agents without new fields still enroll and heartbeat.
- Enrollment context labels are preserved.
- Classification detects server and personal endpoint evidence.
- Operator override wins over heuristic classification.
- Offline bundle enrollment produces the same target model.
- Repair enrollment preserves target identity.

Frontend tests:

- Onboarding copy and routes no longer assume server-only targets.
- Scenario picker shows local, remote, bulk, offline, and repair paths.
- Personal endpoint target hides server-only actions by default.
- Server target shows server capabilities when reported.
- Dynamic/roaming address state is displayed as observation, not identity.
- Enrollment failure states render actionable guidance.

Manual validation:

- Linux server via SSH fleet enroll.
- Windows server via WinRM or local installer.
- Personal Windows workstation local install.
- macOS or Linux laptop local install.
- Offline bundle install.
- Re-enrollment after hostname change.
- Heartbeat after public IP change.

## Phased Delivery

### Phase 1: Language and Model Alignment

- Rename onboarding UI language from server-only to machine/target where appropriate.
- Add target-oriented fields to node responses using existing labels where possible.
- Add `management_mode=agent_managed`, `target_type`, and `reachability_mode` defaults.
- Preserve existing `/onboard`, `/fleet-enroll`, and `/offline-bundle` entry points.

### Phase 2: Network Observations

- Persist heartbeat-derived public/private address observations.
- Compute `reachability_mode`.
- Show current and historical network observations in node/target views.
- Ensure dynamic IP and hostname changes do not create duplicate nodes.

### Phase 3: Universal Onboarding Wizard

- Add scenario-first onboarding.
- Reuse existing SSH/WinRM probe, fleet enroll, offline bundle, and repair flows.
- Add local install path with platform-specific command/download guidance.
- Improve enrollment gate progress and failure messages.

### Phase 4: Target Classification and Capability-Based Actions

- Add heuristic classification from heartbeat, inventory, and service evidence.
- Add operator override.
- Gate UI actions from `agent.capabilities`, target type, and reachability mode.
- Tune personal endpoint defaults separately from server defaults.

### Phase 5: Hardening and Regression Coverage

- Add backend and frontend tests from this spec.
- Add migration tests for existing nodes.
- Validate all onboarding paths in dev and staging.
- Document operational runbooks for dynamic-IP, repair, and offline cases.

## Acceptance Criteria

- A personal PC with changing public IP can enroll, heartbeat, and remain the same target.
- A server with a static IP still works without behavior regression.
- A server behind NAT can be fully managed after outbound agent enrollment.
- Remote push is just one onboarding path, not a requirement for target identity.
- UI copy no longer implies Control One only manages servers.
- Target actions are capability-aware.
- Existing enrolled nodes continue to work after migration.
- Tests cover identity stability, dynamic reachability, target classification, onboarding failures, and legacy compatibility.
