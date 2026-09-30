# Correlation Notifications and Node Response Design

## Goal

Expose operational intelligence already collected by Control One, allow correlation rules to deliver targeted notifications, and provide approval-gated node response actions with agent-reported receipts.

## Evidence and gaps

`NodeDetail.tsx` already reads node liveness, listener inventory, services, packages, web servers, health, connections, and audit records. `node_services` contains the PID, binary path, command line, listening address, and port for listening services, but it is not a full running-process snapshot. The existing firewall pipeline is agent-mediated and receipt-backed, but an internal high-risk firewall action plan is created directly in `queued` state, so it is not an approval gate.

The correlation engine opens alerts only. Global alert email settings and generic webhooks exist, but a correlation rule cannot choose recipients, webhook delivery, or notification severity independently.

## Considered approaches

1. Add buttons to the existing node detail view and dispatch commands immediately. This is fast, but bypasses separation of duties and makes destructive work unsafe.
2. Add an independent remediation and notification subsystem. This duplicates action-plan approvals, heartbeat dispatch, audit logging, receipts, and webhook delivery.
3. Recommended: add small, explicit extensions to the existing contracts. Correlation rules own notification policy; node-response requests create high-risk action plans in `needs_approval`; a distinct authorized user approves; the control plane then creates an agent job; the agent reports a receipt on its next heartbeat.

## Correlation notification policy

Extend a correlation rule with an optional `notification_policy` object:

```json
{
  "in_app": true,
  "email": {"enabled": true, "recipients": ["soc@example.com"]},
  "webhook_ids": ["uuid"],
  "minimum_severity": "high"
}
```

Validation requires at least one channel, limits recipients and webhook IDs, validates tenant ownership, and deduplicates values. The engine keeps alert creation as its primary outcome; after a successful alert insert, it writes an in-app notification for matched tenant users, sends rule-specific email through the existing SMTP infrastructure, and delivers `correlation.alert.fired` to selected enabled tenant webhooks. Delivery failures never suppress alert creation and are auditable.

The rule editor shows a notification policy section with delivery channels, recipients, selected webhooks, and a minimum severity. Existing rules decode with an empty policy and retain the global alert-email behavior until an administrator explicitly configures a rule policy.

## Node intelligence

Add agent capability `process_inventory.v1` and a heartbeat `process_inventory` snapshot. Each row contains PID, parent PID, executable, command line, user, start time, process state, and listening socket references. The control plane replaces the node-scoped snapshot atomically and records its observation time. A missing capability or stale snapshot is shown as unavailable rather than inferred.

The node detail view consolidates agent state, listener inventory, and process inventory into an Operations tab. Each listener links to its process where a PID is available. The view exposes freshness, agent version, capabilities, last heartbeat, process count, and open/listening ports. It does not treat listener inventory as a complete process list.

## Node response and approval boundary

Two response kinds are in scope:

- `process.terminate`: PID plus immutable process identity evidence (PID, start time, executable hash/path, command line) to prevent PID reuse.
- `firewall.port_block`: protocol, local port, direction, optional source CIDR, TTL, and an inverse-rule rollback.

Every request creates an `action_plan` with state `needs_approval`, risk `high`, `min_approvers: 1`, `separation_of_duties: true`, and approver roles limited to operator/admin. The requester cannot approve their own plan. Approval creates exactly one idempotent agent job; denial cancels without creating a job. The heartbeat only exposes jobs for approved plans. Completed actions create receipts and update the plan to succeeded or failed. Offline, stale, unsupported, or inventory-mismatched nodes are rejected before request creation.

Existing IP firewall blocks are migrated to this same gate before their agent job is created. This closes the current direct-queue bypass for high-risk firewall changes.

## Security, audit, and recovery

All node-response requests, approvals, dispatches, agent receipts, failures, and rollbacks are audit events. Request scope retains the node ID and target evidence. Responses surface plan state, approver identity, job ID, receipt, and rollback reference. Process termination is never retried automatically. Firewall blocks use a bounded TTL by default and retain an explicit inverse action.

## Delivery order

1. Correlation notification policy persistence, API, engine delivery, and editor.
2. Process inventory heartbeat/storage/API and node Operations presentation.
3. Approval-gated node-response actions and migration of firewall dispatch to the gate.

## Verification

Tests cover notification policy validation, selected webhook and recipient delivery, alert persistence despite delivery failures, process snapshot tenant isolation/freshness, same-user approval rejection, approval-before-dispatch, PID-reuse rejection, port-block rollback data, agent receipt state transitions, and node-detail action states.
