# Control One Helm Chart

This Helm chart deploys the Control One platform on Kubernetes, including the control plane API, worker services, UI, and optional observability stack.

## Prerequisites

- Kubernetes 1.24+
- Helm 3.8+
- PostgreSQL database (or use included Bitnami PostgreSQL chart)
- Redis (or use included Bitnami Redis chart)
- Ingress controller (nginx, traefik, etc.)
- cert-manager (optional, for TLS certificates)

## Installation

### Quick Start

```bash
# Add Helm repository (if using remote)
helm repo add control-one https://charts.control-one.io
helm repo update

# Install with default values
helm install control-one control-one/control-one

# Install with custom values
helm install control-one control-one/control-one -f my-values.yaml

# Install in specific namespace
helm install control-one control-one/control-one --namespace control-one --create-namespace
```

### Custom Values

Create a `values.yaml` file:

```yaml
controlplane:
  replicaCount: 3
  ingress:
    enabled: true
    hosts:
      - host: control-one.example.com
        paths:
          - path: /
            pathType: Prefix

database:
  url: "postgresql://user:pass@postgres.example.com:5432/controlone?sslmode=require"

redis:
  host: "redis.example.com:6379"
  password: "secret"
```

Then install:

```bash
helm install control-one control-one/control-one -f values.yaml
```

## Configuration

### Control Plane

The control plane service handles API requests and job orchestration.

Key settings:
- `controlplane.replicaCount`: Number of replicas
- `controlplane.resources`: CPU/memory limits
- `controlplane.autoscaling`: HPA configuration
- `controlplane.ingress`: Ingress configuration

### Worker

Worker services process background jobs (provisioning, compliance, etc.).

Key settings:
- `worker.replicaCount`: Number of worker replicas
- `worker.resources`: CPU/memory limits
- `worker.config.worker.concurrency`: Concurrent job processing

### UI

The web UI provides the operator interface.

Key settings:
- `ui.replicaCount`: Number of UI replicas
- `ui.ingress`: Ingress configuration
- `ui.env`: Environment variables for API endpoints

### Database

PostgreSQL database configuration:

- Use external database: Set `postgresql.enabled: false` and configure connection
- Use included database: Set `postgresql.enabled: true` and configure credentials

### Redis

Redis configuration for job queue:

- Use external Redis: Set `redis.enabled: false` and configure connection
- Use included Redis: Set `redis.enabled: true` and configure password

### Observability

Optional observability stack:

- `observability.prometheus.enabled`: Enable Prometheus
- `observability.grafana.enabled`: Enable Grafana
- `observability.loki.enabled`: Enable Loki for logs

## Upgrading

```bash
# Update repository
helm repo update

# Upgrade release
helm upgrade control-one control-one/control-one

# Upgrade with custom values
helm upgrade control-one control-one/control-one -f values.yaml
```

## Uninstallation

```bash
helm uninstall control-one

# Remove namespace
kubectl delete namespace control-one
```

## Production Considerations

### High Availability

1. **Control Plane**: Set `replicaCount: 3` or enable autoscaling
2. **Worker**: Set `replicaCount: 3+` for redundancy
3. **Database**: Use external managed database with multi-AZ
4. **Redis**: Use external Redis cluster or enable replication

### Security

1. **TLS**: Enable TLS certificates via cert-manager
2. **Secrets**: Use Kubernetes secrets or external secret management
3. **Network Policies**: Restrict pod-to-pod communication
4. **RBAC**: Configure appropriate service account permissions

### Monitoring

1. Enable Prometheus for metrics collection
2. Configure Grafana dashboards
3. Set up alerting rules
4. Enable Loki for log aggregation

### Backup

1. **Database**: Configure automated backups
2. **Secrets**: Backup Kubernetes secrets
3. **ConfigMaps**: Version control configuration

## Troubleshooting

### Pods Not Starting

```bash
# Check pod status
kubectl get pods -n control-one

# Check logs
kubectl logs -n control-one deployment/control-one-controlplane

# Check events
kubectl describe pod -n control-one <pod-name>
```

### Database Connection Issues

```bash
# Verify database credentials
kubectl get secret control-one-postgres -n control-one -o yaml

# Test connection
kubectl exec -it -n control-one deployment/control-one-controlplane -- \
  psql $DATABASE_URL
```

### Ingress Not Working

```bash
# Check ingress status
kubectl get ingress -n control-one

# Check ingress controller
kubectl get pods -n ingress-nginx
```

## Values Reference

See [values.yaml](values.yaml) for all available configuration options.

## Support

For issues or questions:
- [Documentation](../../docs/)
- [Deployment Guide](../../docs/deployment.md)
- [Operational Runbooks](../../docs/runbooks.md)


