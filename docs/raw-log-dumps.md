# Raw log dumps

Raw log dumps are short-lived diagnostic artifacts for a single tenant and node.
They are separate from normal telemetry delivery and never consume the agent
delivery spool.

## Sources

- `control_plane`: bounded rows already stored by the control plane.
- `node_agent`: a non-destructive snapshot of the agent durable log spool.
  Nodes that do not advertise `log_dump.v1` cannot accept this source.

An empty source is different from an unavailable source. Successful empty
captures report `source_available=true` and zero rows. Missing/unreadable
sources report `source_available=false` with a reason.

## Bounds

- Maximum request window: 24 hours.
- Retention: 1, 3, 7, 14, or 30 days; default 7.
- Agent upload chunk: 4 MiB maximum.
- Final artifact: 32 MiB maximum.
- Preview: 256 KiB / 200 lines maximum.
- Output: NDJSON.

Raw payload bytes are written only to the artifact store. Jobs and audit rows
contain lifecycle/scope metadata, not dump content.

## Storage and cleanup

The control plane stores artifacts under `CONTROL_ONE_LOG_DUMPS_DIR`, default
`/var/lib/control-one/log-dumps`. Production Compose bind-mounts the restricted host directory
`/opt/control-one/deploy/log-dumps` there. The deploy workflow creates it as
uid/gid `65532:65532` with mode `750`, so artifacts survive
image/container replacement and remain writable by the distroless nonroot
control-plane process.

The control plane runs a bounded cleanup pass on startup and hourly. Expiry is
also enforced on every read, so content is inaccessible once `expires_at` is
reached even if physical deletion has not run yet. Cleanup removes final
artifacts, upload chunks and metadata; failed filesystem/database cleanup is
retried with bounded backoff.

Operators should size and back up this volume according to local policy. Do not
copy it into general application logs or object stores without equivalent
tenant isolation and retention controls.

## Access

Request creation requires operator/admin tenant access. List/detail/preview/
download require an authenticated tenant role. Agent claim/upload endpoints are
mTLS-scoped to the enrolled node and additionally bind dump, job, tenant, node,
claim token and claim generation.

Downloads are authenticated API requests. Browser clients must fetch with the
normal Authorization header and save the returned blob; do not expose the
artifact directory through nginx or create public file URLs.

## Operational notes

- Agent capture is non-destructive: export never acknowledges or deletes normal
  telemetry backlog.
- Claim leases allow restart/takeover. A newer claim invalidates the old token.
- Chunk retries are idempotent only when ordinal, generation, size and SHA-256
  match.
- Final assembly revalidates every chunk and the final SHA-256 before the dump
  becomes `captured`.
- Captures still in progress after the server timeout are failed with the paired
  job terminalized in the same lifecycle.
