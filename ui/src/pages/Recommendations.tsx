import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
import type { Recommendation } from '../lib/api';

export function Recommendations(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const { showToast } = useToast();
  const [tenantId, setTenantId] = useState('');
  const [items, setItems] = useState<Recommendation[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    try {
      const resp = await client.listRecommendations(tenantId);
      setItems(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    }
  }, [client, tenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const promote = async (rec: Recommendation) => {
    if (rec.kind !== 'port_rule') {
      showToast('Promote only supported for port-rule drafts in this build.', 'info');
      return;
    }
    try {
      const d = rec.draft;
      await client.createPortRule({
        tenant_id: tenantId,
        name: String(rec.title),
        port: Number(d.port),
        protocol: String(d.protocol) as 'tcp' | 'udp',
        expected_state: String(d.expected_state) as 'open' | 'closed',
        severity: String(d.severity ?? 'medium'),
        action: String(d.action ?? 'notify'),
        enabled: true,
      });
      showToast('Promoted to port rule.', 'success');
      refresh();
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Promote failed', 'error');
    }
  };

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Behavioral</p>
          <h2>Recommendations</h2>
          <p className="subtitle">Derived from 30 days of port observations.</p>
        </div>
        <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} aria-label="Tenant">
          {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      {items.length === 0 ? (
        <p className="muted">No recommendations yet. Data needs ≥30 days of observations.</p>
      ) : (
        <ul style={{ listStyle: 'none', padding: 0 }}>
          {items.map((rec, i) => (
            <li key={i} style={{ border: '1px solid var(--border)', padding: '0.75rem', marginBottom: '0.5rem', borderRadius: 4 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <strong>{rec.title}</strong>
                  <div className="muted">{rec.rationale}</div>
                  <small>Confidence: {(rec.confidence * 100).toFixed(1)}%</small>
                </div>
                <button type="button" className="primary-button" onClick={() => promote(rec)}>Promote</button>
              </div>
              <details style={{ marginTop: '0.5rem' }}>
                <summary>Draft</summary>
                <pre>{JSON.stringify(rec.draft, null, 2)}</pre>
              </details>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
