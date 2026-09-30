# Phase 2 production readiness checklist

Phase 2 covers reliable node enrollment, telemetry collection, normalization,
correlation, and operator-visible alert delivery. The checklist is deliberately
based on observable evidence rather than a successful installer exit code.

## Completed

- [x] Windows enrollment writes Security, Windows Hello, and Biometrics event-log sources.
- [x] Windows service install preserves existing SCM configuration during repair.
- [x] Repair stops the running agent before replacing a locked executable.
- [x] Event IDs 4625, 7001, and 1005 normalize to authentication failure signals.
- [x] Normalized events retain the original source event ID and message.
- [x] Correlation alerts retain contributing event evidence and node scope.
- [x] Alert presentation identifies authentication activity as **Credential** and names the affected node.
- [x] Alert email delivery and the live fingerprint-to-alert workflow were verified locally.
- [x] Agent heartbeats report collector state and the node detail page displays telemetry readiness.
- [x] A node-scoped update request bypasses the staged rollout wave safely while
  respecting the operator pause and binary checksum verification.
- [x] Failed update jobs expose their recorded failure reason in node maintenance.
- [x] The live Cloudspace Windows node was repaired/re-enrolled and verified with a
  running service, current heartbeat, ready telemetry, and running `procmon` and
  `services` collectors.
- [x] Fleet health totals now reconcile against the current node list and treat an
  `active` node with a stale heartbeat as **warning**, instead of presenting it as
  healthy.
- [x] Installer URL derivation is covered for an explicit HTTPS production address,
  an HTTPS reverse proxy, and the local development request scheme.

## In progress

- [ ] Repeat the Windows enrollment readiness check on a clean machine: service,
  endpoint, channel access, audit policy, and first telemetry sample. The existing
  Cloudspace node has passed this check; a clean-host run is still required before
  production sign-off.
- [ ] Verify the production installer uses the deployed HTTPS control-plane URL rather
  than the local `127.0.0.1` development endpoint.
- [ ] Validate Linux journald authentication collection and macOS Unified Logging
  authentication collection on representative hosts.
- [ ] Migrate production correlation templates to the portable
  `authentication.failure` event type while retaining platform-specific aliases.
- [ ] Verify notification deduplication and suppression with the production mail
  provider, not only local Mailpit.

## Exit criteria

Phase 2 is complete when a clean Windows, Linux, and macOS enrollment each shows a
running service, reachable control plane, readable configured source, and recent
telemetry in the node detail page; a representative authentication failure produces
one normalized event and one deduplicated alert; and the alert contains the host,
source evidence, and notification outcome.
