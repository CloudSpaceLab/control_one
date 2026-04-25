import { useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import type { ReportDesc } from '../lib/api';

const RANGE_PRESETS: { label: string; days: number }[] = [
  { label: 'Last 24h', days: 1 },
  { label: 'Last 7 days', days: 7 },
  { label: 'Last 30 days', days: 30 },
  { label: 'Last 90 days', days: 90 },
];

export function Reports(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState('');
  const [reports, setReports] = useState<ReportDesc[]>([]);
  const [range, setRange] = useState(30);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  useEffect(() => {
    (async () => {
      try {
        const resp = await client.listReports();
        setReports(resp.data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'load failed');
      }
    })();
  }, [client]);

  const download = (slug: string) => {
    const since = new Date(Date.now() - range * 24 * 60 * 60 * 1000).toISOString();
    const url = client.buildReportExportUrl(slug, { tenantId, since });
    window.open(url, '_blank');
  };

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Exports</p>
          <h2>Reports</h2>
          <p className="subtitle">Download CSV extracts for compliance, audit, alerts, and access.</p>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} aria-label="Tenant">
            {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
          <select value={range} onChange={(e) => setRange(Number(e.target.value))} aria-label="Range">
            {RANGE_PRESETS.map((p) => <option key={p.days} value={p.days}>{p.label}</option>)}
          </select>
        </div>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '1rem' }}>
        {reports.map((rep) => (
          <article key={rep.slug} className="stat-card" style={{ padding: '1rem' }}>
            <p style={{ marginTop: 0 }}><strong>{rep.title}</strong></p>
            <p className="muted" style={{ minHeight: '3em' }}>{rep.description}</p>
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
              {rep.formats.map((fmt) => (
                <button
                  key={fmt}
                  type="button"
                  className="primary-button"
                  onClick={() => download(rep.slug)}
                >
                  Download {fmt.toUpperCase()}
                </button>
              ))}
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}
