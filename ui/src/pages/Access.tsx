import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import type { AccessRequest, CreateAccessRequestPayload } from '../lib/api';

type Tab = 'pending' | 'mine' | 'all';

export function Access(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState('');
  const [tab, setTab] = useState<Tab>('pending');
  const [items, setItems] = useState<AccessRequest[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    try {
      const status = tab === 'pending' ? 'pending' : undefined;
      const resp = await client.listAccessRequests({ tenantId, status, limit: 100, offset: 0 });
      setItems(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    }
  }, [client, tenantId, tab]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const approve = async (id: string) => {
    await client.approveAccessRequest(id, '');
    refresh();
  };
  const deny = async (id: string) => {
    await client.denyAccessRequest(id, '');
    refresh();
  };

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Just-in-time access</p>
          <h2>Privileged access requests</h2>
          <p className="subtitle">Request, approve, auto-revoke. No standing admin credentials.</p>
        </div>
        <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} aria-label="Tenant">
          {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      <div className="tab-row" style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem' }}>
        <button type="button" className={tab === 'pending' ? 'primary-button' : 'secondary-button'} onClick={() => setTab('pending')}>Pending</button>
        <button type="button" className={tab === 'all' ? 'primary-button' : 'secondary-button'} onClick={() => setTab('all')}>All</button>
      </div>

      <RequestForm tenantId={tenantId} onCreated={refresh} />

      <table className="data-table" style={{ marginTop: '1rem', width: '100%' }}>
        <thead>
          <tr>
            <th>Type</th>
            <th>Access</th>
            <th>Justification</th>
            <th>Status</th>
            <th>Requested</th>
            <th>Expires</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {items.length === 0 ? (
            <tr><td colSpan={7} className="muted">No requests.</td></tr>
          ) : (
            items.map((req) => (
              <tr key={req.id}>
                <td>{req.target_resource_type}</td>
                <td><code>{req.requested_access}</code></td>
                <td>{req.justification ?? '—'}</td>
                <td>{req.status}</td>
                <td>{new Date(req.requested_at).toLocaleString()}</td>
                <td>{req.expires_at ? new Date(req.expires_at).toLocaleString() : '—'}</td>
                <td>
                  {req.status === 'pending' ? (
                    <>
                      <button type="button" className="primary-button" onClick={() => approve(req.id)}>Approve</button>
                      <button type="button" className="secondary-button" onClick={() => deny(req.id)} style={{ marginLeft: '0.4rem' }}>Deny</button>
                    </>
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

function RequestForm({ tenantId, onCreated }: { tenantId: string; onCreated: () => void }): JSX.Element {
  const client = useApiClient();
  const [form, setForm] = useState<CreateAccessRequestPayload>({
    tenant_id: tenantId,
    target_resource_type: 'ssh',
    requested_access: 'root',
    justification: '',
    ttl_seconds: 1800,
  });
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setForm((f) => ({ ...f, tenant_id: tenantId }));
  }, [tenantId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    try {
      await client.createAccessRequest(form);
      setForm({ ...form, justification: '' });
      onCreated();
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={submit} style={{ display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: '0.5rem', alignItems: 'end' }}>
      <label>
        Type
        <select value={form.target_resource_type} onChange={(e) => setForm({ ...form, target_resource_type: e.target.value as 'ssh' | 'rdp' | 'db' })}>
          <option value="ssh">ssh</option>
          <option value="rdp">rdp</option>
          <option value="db">db</option>
        </select>
      </label>
      <label>
        Access
        <input required value={form.requested_access} onChange={(e) => setForm({ ...form, requested_access: e.target.value })} />
      </label>
      <label style={{ gridColumn: 'span 2' }}>
        Justification
        <input value={form.justification ?? ''} onChange={(e) => setForm({ ...form, justification: e.target.value })} />
      </label>
      <label>
        TTL (s)
        <input type="number" min={60} value={form.ttl_seconds ?? 1800} onChange={(e) => setForm({ ...form, ttl_seconds: Number(e.target.value) })} />
      </label>
      <button type="submit" className="primary-button" disabled={submitting} style={{ gridColumn: 'span 5' }}>
        Request access
      </button>
    </form>
  );
}
