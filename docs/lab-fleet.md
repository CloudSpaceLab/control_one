# Control One Lab Fleet: Roaming / Non-Static Endpoint Agents

This lab starts a small fleet of node-agent containers that behave like endpoint clients rather than servers.

Properties:

- Agents initiate outbound traffic to the control plane.
- No container publishes inbound ports.
- No static Docker IP addresses are assigned.
- Recreating a container gives it a fresh Docker bridge IP and fresh local state.
- Mesh is disabled because the current control plane does not implement `/api/v1/mesh/peers` or `/api/v1/mesh/rotate` yet.
- The lab image bakes in throwaway self-signed cert files only to satisfy the current agent client initialization path; the dev lab URL is plain HTTP.

## Prerequisite

Start the dev control plane on the host. The lab config expects:

```text
http://host.docker.internal:8444
```

For the local dev config, the bootstrap token is:

```text
dev-bootstrap-token
```

The UI/API dev admin token is:

```text
dev-admin-token
```

The current node agent registration payload does not carry `tenant_id`, so the dev control plane must run with `registration.default_tenant_id` set to an existing tenant. In the local dev lab, create/select a tenant, then start the control plane with a temporary config that sets that field.

`controlplane/config/controlplane.dev.yaml` also defines `dev-bootstrap-token` as a dev-only static token so lab agents can exercise heartbeat/orchestration over HTTP when client TLS is disabled. Production heartbeat behavior still requires mTLS agent identity.

## Start the lab fleet

From the repo root:

```bash
docker compose -f docker-compose.lab-fleet.yml up -d --build
```

Check containers:

```bash
docker compose -f docker-compose.lab-fleet.yml ps
```

Tail one endpoint:

```bash
docker compose -f docker-compose.lab-fleet.yml logs -f roaming-laptop-01
```

Verify enrollment:

```bash
curl -fsS -H 'Authorization: Bearer dev-admin-token' \
  http://localhost:8444/api/v1/nodes | python3 -m json.tool
```

## Simulate dynamic IP / roaming churn

Recreate one endpoint. Because the lab does not use persistent volumes or static IPAM, the container gets a new filesystem state and Docker-assigned IP.

```bash
docker compose -f docker-compose.lab-fleet.yml rm -sf roaming-laptop-01
docker compose -f docker-compose.lab-fleet.yml up -d roaming-laptop-01
```

Inspect its current Docker IP:

```bash
docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' \
  controlone-lab-fleet-roaming-laptop-01-1
```

## Stop the lab fleet

```bash
docker compose -f docker-compose.lab-fleet.yml down
```

To remove built images too:

```bash
docker compose -f docker-compose.lab-fleet.yml down --rmi local
```

## Notes

These containers are intentionally not realistic Linux servers. They are light, outbound-only node-agent clients used to exercise the control-plane enrollment, liveness, inventory, telemetry, and pending-action paths under non-static Docker IPs.

Current limitation: the control plane still stores `public_ip` as a node attribute at registration time, and the peer-to-peer mesh path is not implemented. This lab therefore validates Control One's agent-initiated orchestration path, not WireGuard/NAT traversal.
