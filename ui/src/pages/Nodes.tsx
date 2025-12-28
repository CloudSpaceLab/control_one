import { useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';

export function Nodes(): JSX.Element {
  const { data: tenants } = useTenants();
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const { data: nodes, loading, error } = useNodes(
    selectedTenant ? { tenantId: selectedTenant } : {},
  );

  const tenantOptions = useMemo(() => tenants, [tenants]);
  const tenantNames = useMemo(() => {
    const entries = new Map<string, string>();
    for (const tenant of tenants) {
      entries.set(tenant.id, tenant.name);
    }
    return entries;
  }, [tenants]);

  return (
    <section>
      <h2>Nodes</h2>
      <p>Connected agents reporting into the control plane.</p>

      <div className="toolbar">
        <label htmlFor="tenant-filter">Filter by tenant</label>
        <select
          id="tenant-filter"
          value={selectedTenant ?? ''}
          onChange={(event) => {
            const value = event.target.value;
            setSelectedTenant(value === '' ? undefined : value);
          }}
        >
          <option value="">All tenants</option>
          {tenantOptions.map((tenant) => (
            <option key={tenant.id} value={tenant.id}>
              {tenant.name}
            </option>
          ))}
        </select>
      </div>

      {loading ? <p className="muted">Loading nodes&hellip;</p> : null}
      {error ? <p className="form-error">Failed to load nodes: {error}</p> : null}
      {!loading && !error && nodes.length === 0 ? <p className="muted">No nodes registered.</p> : null}

      {!loading && !error && nodes.length > 0 ? (
        <div className="card-grid">
          {nodes.map((node) => (
            <article key={node.id} className="node-card">
              <header>
                <h3>{node.hostname}</h3>
                <span className="badge">{node.os ?? 'unknown OS'}</span>
              </header>
              <dl>
                <div>
                  <dt>Node ID</dt>
                  <dd>{node.id}</dd>
                </div>
                <div>
                  <dt>Tenant</dt>
                  <dd>{tenantNames.get(node.tenant_id) ?? node.tenant_id}</dd>
                </div>
                {node.public_ip ? (
                  <div>
                    <dt>Public IP</dt>
                    <dd>{node.public_ip}</dd>
                  </div>
                ) : null}
                {node.arch ? (
                  <div>
                    <dt>Architecture</dt>
                    <dd>{node.arch}</dd>
                  </div>
                ) : null}
              </dl>
            </article>
          ))}
        </div>
      ) : null}
    </section>
  );
}
