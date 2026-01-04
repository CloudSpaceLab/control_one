# Observability Toolkit

This directory contains local monitoring assets for the Control One stack.

## Prometheus

- **Config**: `prometheus.yml` scrapes the control plane API at `controlplane:8443/metrics`.
- **Dev compose**: `docker-compose.dev.yml` mounts this config into a Prometheus container (`http://localhost:9090`).

### Adding additional scrape targets

1. Edit `infra/observability/prometheus.yml` and append another `scrape_configs` entry.
2. Restart Prometheus (e.g., `docker compose restart prometheus`).

## Grafana

- **Provisioning**: `docker-compose.dev.yml` mounts `infra/observability/grafana-provisioning/` so Grafana auto-configures the Prometheus datasource and loads dashboards from `infra/observability/dashboards/`.
- **Dashboard**: `dashboards/controlplane-overview.json` highlights request throughput, goroutines, heap allocations, and worker job rates. It appears automatically under the “Control One” folder after Grafana starts.
- Grafana default credentials: `admin` / `admin`.

### Suggested workflow

1. Start the dev stack: `make docker-up` (Grafana listens on `http://localhost:3000`).
2. Log into Grafana; confirm the “Control One” folder and `Control One - Overview` dashboard exist.
3. Exercise API endpoints or trigger jobs to see live metrics refresh.

## Troubleshooting

- If Grafana shows “No data” for panels, confirm Prometheus is reachable from Grafana (`Configuration → Data sources → Prometheus`).
- Verify the control plane exposes `/metrics` by hitting `https://localhost:8443/metrics` (self-signed TLS).
- Adjust scrape interval/timeout if you see `context deadline exceeded` errors in Prometheus logs.
