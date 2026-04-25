import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
import type { Alert } from '../lib/api';

const STATE_FILTERS = ['open', 'acked', 'resolved'] as const;

export function Alerts(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState('');
  const [state, setState] = useState<typeof STATE_FILTERS[number]>('open');
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    try {
      const resp = await client.listAlerts({ tenantId, state, limit: 100, offset: 0 });
      setAlerts(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId, state]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEventStream(tenantId, ['alert.opened'], () => refresh());

  const ack = async (id: string) => {
    await client.ackAlert(id);
    refresh();
  };
  const resolve = async (id: string) => {
    await client.resolveAlert(id);
    refresh();
  };

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Operations</p>
          <h2>Alerts</h2>
          <p className="subtitle">Deduped inbox from correlation, rules, and compliance.</p>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} aria-label="Tenant">
            {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
          <select value={state} onChange={(e) => setState(e.target.value as typeof STATE_FILTERS[number])} aria-label="State">
            {STATE_FILTERS.map((s) => <option key={s} value={s}>{s}</option>)}
          </select>
          <button type="button" className="primary-button" onClick={refresh}>Refresh</button>
        </div>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      <table className="data-table" style={{ width: '100%' }}>
        <thead>
          <tr>
            <th>Severity</th>
            <th>Title</th>
            <th>Source</th>
            <th>Opened</th>
            <th>State</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr><td colSpan={6} className="muted">Loading…</td></tr>
          ) : alerts.length === 0 ? (
            <tr><td colSpan={6} className="muted">No alerts.</td></tr>
          ) : (
            alerts.map((a) => (
              <tr key={a.id}>
                <td><span className={`status-pill status-${a.severity}`}>{a.severity}</span></td>
                <td>
                  <strong>{a.title}</strong>
                  {a.summary ? <div className="muted">{a.summary}</div> : null}
                </td>
                <td>{a.source}</td>
                <td>{new Date(a.opened_at).toLocaleString()}</td>
                <td>{a.state}</td>
                <td>
                  {a.state === 'open' ? (
                    <button type="button" className="secondary-button" onClick={() => ack(a.id)}>Ack</button>
                  ) : null}
                  {a.state !== 'resolved' ? (
                    <button type="button" className="secondary-button" onClick={() => resolve(a.id)} style={{ marginLeft: '0.4rem' }}>Resolve</button>
                  ) : null}
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </section>
  );
}
