# Bank HA/DR Runbook

This runbook is the production baseline for on-prem bank deployments.

## Availability Targets

- Control plane: at least 2 replicas behind an internal load balancer.
- Postgres: 3-node HA with synchronous standby for RPO 0 on control metadata.
- Doris: 3 FE / 3 BE minimum for production analytic storage, with
  `doris.replication_num: 3` before applying Doris migrations.
- Object/offline content store: replicated storage or versioned object bucket.
- Edge collectors: at least 2 per network zone for syslog/API receiver HA.

## Backup Schedule

- Postgres: continuous WAL archiving plus daily logical export.
- Doris: daily snapshot of analytic tables and FE metadata backup.
- Offline content: immutable signed bundle archive with checksums.
- Control-plane config/secrets: encrypted export from the secret backend.

## Restore Drill

1. Restore Postgres into an isolated namespace and verify tenant, node, job,
   action-plan, and audit-log tables.
2. Restore Doris snapshots and run read-only event/search smoke queries.
3. Restore offline content root and verify bundle signatures.
4. Start one control-plane replica against the restored stores.
5. Run agent heartbeat, event ingest, SIEM source-health, patch-plan dry-run,
   and private-access exposure reconciliation smoke tests.
6. Record RPO/RTO, failed checks, operator, timestamp, and evidence links.

## Executable Drill

Use `scripts/bank_ha_dr_drill.sh` for change-window evidence. It writes
NDJSON evidence under `ARTIFACT_DIR` and is non-destructive unless
`ALLOW_RESTORE=true` is explicitly set with an isolated `RESTORE_POSTGRES_URL`.

Examples:

```bash
POSTGRES_URL="$PROD_POSTGRES_URL" \
OFFLINE_CONTENT_ROOT=/var/lib/control-one/offline-content \
CONTROL_ONE_CONFIG=/etc/control-one/controlplane.yaml \
ARTIFACT_DIR=/evidence/drills/2026-05-29 \
scripts/bank_ha_dr_drill.sh backup

POSTGRES_DUMP=/evidence/drills/2026-05-29/postgres-20260529T000000Z.dump \
RESTORE_POSTGRES_URL="$RESTORE_POSTGRES_URL" \
ALLOW_RESTORE=true \
scripts/bank_ha_dr_drill.sh restore-smoke

CONTROL_ONE_URL=https://control-one-dr.example.bank \
DORIS_FE_HTTP_URL=http://doris-fe-dr.example.bank:8030 \
TENANT_ID="$TENANT_ID" \
CONTROL_ONE_TOKEN="$CONTROL_ONE_TOKEN" \
scripts/bank_ha_dr_drill.sh failover-smoke
```

## Production Gates

- `doris.replication_num` is `3` for production clusters.
- No production Doris table is created from single-replica defaults.
- Backup artifacts are encrypted and tenant scoped.
- Restore drill evidence is less than 90 days old before go-live.
- Airgapped bundle public keys and rollback bundle inventory are documented.
- `bank_ha_dr_drill.sh backup`, `restore-smoke`, and `failover-smoke` evidence
  exists for the target environment or the bank has signed a documented waiver.
