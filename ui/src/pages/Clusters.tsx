import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useClusters } from '../hooks/useClusters';
import { useTenants } from '../hooks/useTenants';
import type { Cluster, ClusterHealthState } from '../lib/api';
import './Clusters.css';

const STATE_LABEL: Record<ClusterHealthState, string> = {
  healthy: 'Healthy',
  degraded: 'Degraded',
  unhealthy: 'Unhealthy',
  empty: 'Empty',
};

export function clusterStateLabel(state: ClusterHealthState | undefined): string {
  if (!state) {
    return 'Unknown';
  }
  return STATE_LABEL[state] ?? state;
}

export function clusterStateClass(state: ClusterHealthState | undefined): string {
  switch (state) {
    case 'healthy':
      return 'cluster-badge cluster-badge--healthy';
    case 'degraded':
      return 'cluster-badge cluster-badge--degraded';
    case 'unhealthy':
      return 'cluster-badge cluster-badge--unhealthy';
    case 'empty':
      return 'cluster-badge cluster-badge--empty';
    default:
      return 'cluster-badge cluster-badge--unknown';
  }
}

// Renders the rollout in-flight progress bar if the cluster has a latest
// rollout in a non-terminal state. "terminal" = completed or aborted.
function RolloutProgressInline({ cluster }: { cluster: Cluster }): JSX.Element | null {
  const rollout = cluster.latest_rollout;
  if (!rollout) {
    return null;
  }
  if (rollout.state === 'completed' || rollout.state === 'aborted') {
    return (
      <span className="cluster-rollout-inline muted">Last rollout: {rollout.state}</span>
    );
  }

  // For an in-flight rollout we don't know total waves server-side without a
  // separate call, but current_wave + wave_size gives the list view enough
  // signal. Detail view shows the full waves table.
  return (
    <span className="cluster-rollout-inline">
      Rollout {rollout.state} — wave {rollout.current_wave + 1}
    </span>
  );
}

export function Clusters(): JSX.Element {
  const { data: tenants } = useTenants();
  const [tenantFilter, setTenantFilter] = useState<string>('');
  const [limit] = useState(20);
  const [offset, setOffset] = useState(0);

  const params = useMemo(
    () => ({
      tenantId: tenantFilter || undefined,
      limit,
      offset,
    }),
    [tenantFilter, limit, offset],
  );

  const { data: clusters, pagination, loading, error, reload } = useClusters(params);

  const tenantNames = useMemo(() => {
    const map = new Map<string, string>();
    tenants.forEach((tenant) => map.set(tenant.id, tenant.name));
    return map;
  }, [tenants]);

  const summary = useMemo(() => {
    let healthy = 0;
    let degraded = 0;
    let unhealthy = 0;
    clusters.forEach((cluster) => {
      if (!cluster.health) {
        return;
      }
      if (cluster.health.state === 'healthy') {
        healthy += 1;
      } else if (cluster.health.state === 'degraded') {
        degraded += 1;
      } else if (cluster.health.state === 'unhealthy') {
        unhealthy += 1;
      }
    });
    return { healthy, degraded, unhealthy, total: pagination.total };
  }, [clusters, pagination.total]);

  return (
    <section className="clusters-page">
      <div className="page-header">
        <div>
          <h2>Clusters</h2>
          <p>Grouped server fleets managed as a single unit.</p>
        </div>
      </div>

      <div className="stat-card-grid">
        <article className="stat-card">
          <span className="muted">Total clusters</span>
          <strong>{summary.total}</strong>
          <small className="muted">Across current filter</small>
        </article>
        <article className="stat-card">
          <span className="muted">Healthy</span>
          <strong>{summary.healthy}</strong>
          <small className="muted">All members up</small>
        </article>
        <article className="stat-card">
          <span className="muted">Degraded</span>
          <strong>{summary.degraded}</strong>
          <small className="muted">Quorum but not full</small>
        </article>
        <article className="stat-card">
          <span className="muted">Unhealthy</span>
          <strong>{summary.unhealthy}</strong>
          <small className="muted">Below quorum</small>
        </article>
      </div>

      <div className="panel clusters-list">
        <div className="toolbar clusters-toolbar">
          <label htmlFor="cluster-tenant-filter">
            Tenant
            <select
              id="cluster-tenant-filter"
              value={tenantFilter}
              onChange={(event) => {
                setTenantFilter(event.target.value);
                setOffset(0);
              }}
            >
              <option value="">All tenants</option>
              {tenants.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.name}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="ghost-button" onClick={reload} disabled={loading}>
            {loading ? 'Refreshing…' : 'Refresh'}
          </button>
        </div>

        {loading ? <p className="muted">Loading clusters&hellip;</p> : null}
        {error ? <p className="form-error">Failed to load clusters: {error}</p> : null}
        {!loading && !error && clusters.length === 0 ? (
          <p className="muted">No clusters yet. Create one via POST /api/v1/clusters.</p>
        ) : null}

        {!loading && !error && clusters.length > 0 ? (
          <>
            <table className="clusters-table" data-testid="clusters-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Tenant</th>
                  <th>Provider</th>
                  <th>Size</th>
                  <th>Health</th>
                  <th>Rollout</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {clusters.map((cluster) => {
                  const healthState = cluster.health?.state as ClusterHealthState | undefined;
                  const healthyCount = cluster.health?.healthy_count ?? 0;
                  const totalCount = cluster.health?.total_count ?? 0;
                  return (
                    <tr key={cluster.id}>
                      <td>
                        <Link to={`/clusters/${cluster.id}`} className="cluster-link">
                          {cluster.name}
                        </Link>
                      </td>
                      <td>{tenantNames.get(cluster.tenant_id) ?? cluster.tenant_id}</td>
                      <td>{cluster.provider}</td>
                      <td>
                        {totalCount} / {cluster.desired_size}
                      </td>
                      <td>
                        <span className={clusterStateClass(healthState)}>
                          {clusterStateLabel(healthState)}
                        </span>
                        {totalCount > 0 ? (
                          <small className="muted cluster-health-subtext">
                            {healthyCount}/{totalCount} up
                          </small>
                        ) : null}
                      </td>
                      <td>
                        <RolloutProgressInline cluster={cluster} />
                      </td>
                      <td>
                        <Link to={`/clusters/${cluster.id}`} className="ghost-button">
                          View
                        </Link>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <div className="pagination">
              <button
                type="button"
                disabled={pagination.prevOffset === null || pagination.prevOffset === undefined}
                onClick={() => setOffset(pagination.prevOffset ?? 0)}
              >
                Previous
              </button>
              <span>
                Showing {clusters.length} of {pagination.total} clusters
              </span>
              <button
                type="button"
                disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
                onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
              >
                Next
              </button>
            </div>
          </>
        ) : null}
      </div>
    </section>
  );
}
