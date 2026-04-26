import { useCallback, useEffect, useState, lazy, Suspense } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useEventStream } from '../hooks/useEventStream';
import { ConfirmModal } from '../components/ConfirmModal';
import type {
  CreateLogRulePayload,
  CreatePortRulePayload,
  LogRule,
  PortRule,
} from '../lib/api';

// Lazy-load the visual builder so its drag/drop state machine doesn't slow
// the initial page render for operators who only want to author rules in the
// flat form.
const RuleBuilder = lazy(() => import('./RuleBuilder').then((m) => ({ default: m.RuleBuilder })));

type Tab = 'port' | 'log' | 'builder';

export function Rules(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tab, setTab] = useState<Tab>('port');
  const [tenantId, setTenantId] = useState<string>('');
  const [portRules, setPortRules] = useState<PortRule[]>([]);
  const [logRules, setLogRules] = useState<LogRule[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    try {
      const [p, l] = await Promise.all([
        client.listPortRules({ tenantId, limit: 100, offset: 0 }),
        client.listLogRules({ tenantId, limit: 100, offset: 0 }),
      ]);
      setPortRules(p.data);
      setLogRules(l.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    }
  }, [client, tenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEventStream(tenantId, ['policy.updated', 'rule.triggered'], (ev) => {
    setNotice(`Realtime: ${ev.topic}`);
    refresh();
    window.setTimeout(() => setNotice(null), 3000);
  });

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Detection</p>
          <h2>Detection rules</h2>
          <p className="subtitle">Define what&apos;s allowed. Detect violations instantly. Real-time enforcement on every node.</p>
        </div>
        <select
          value={tenantId}
          onChange={(e) => setTenantId(e.target.value)}
          aria-label="Tenant"
          style={{ padding: '0.4rem' }}
        >
          {tenants.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}
      {notice ? <p className="muted">{notice}</p> : null}

      <div className="tab-row" role="tablist" style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem' }}>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'port'}
          className={tab === 'port' ? 'primary-button' : 'secondary-button'}
          onClick={() => setTab('port')}
        >
          Port rules ({portRules.length})
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'log'}
          className={tab === 'log' ? 'primary-button' : 'secondary-button'}
          onClick={() => setTab('log')}
        >
          Log rules ({logRules.length})
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'builder'}
          className={tab === 'builder' ? 'primary-button' : 'secondary-button'}
          onClick={() => setTab('builder')}
          title="Compose rules visually with drag-and-drop blocks"
        >
          Visual builder
        </button>
      </div>

      {tab === 'port' ? (
        <PortRulesPane tenantId={tenantId} rules={portRules} onRefresh={refresh} />
      ) : tab === 'log' ? (
        <LogRulesPane tenantId={tenantId} rules={logRules} onRefresh={refresh} />
      ) : (
        <Suspense fallback={<p className="muted">Loading builder…</p>}>
          <RuleBuilder />
        </Suspense>
      )}
    </section>
  );
}

function PortRulesPane({
  tenantId,
  rules,
  onRefresh,
}: {
  tenantId: string;
  rules: PortRule[];
  onRefresh: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [form, setForm] = useState<CreatePortRulePayload>({
    tenant_id: tenantId,
    name: '',
    port: 22,
    protocol: 'tcp',
    expected_state: 'closed',
    severity: 'medium',
    action: 'notify',
    enabled: true,
  });
  const [submitting, setSubmitting] = useState(false);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  useEffect(() => {
    setForm((f) => ({ ...f, tenant_id: tenantId }));
  }, [tenantId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    try {
      await client.createPortRule(form);
      setForm({ ...form, name: '' });
      onRefresh();
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (id: string) => {
    await client.deletePortRule(id);
    onRefresh();
  };

  return (
    <div>
      <ConfirmModal
        open={confirmDeleteId !== null}
        title="Delete port rule?"
        body="This cannot be undone."
        variant="danger"
        confirmLabel="Delete"
        onConfirm={() => {
          if (confirmDeleteId) remove(confirmDeleteId);
          setConfirmDeleteId(null);
        }}
        onCancel={() => setConfirmDeleteId(null)}
      />
      <form className="form-row" onSubmit={submit} style={{ display: 'grid', gridTemplateColumns: 'repeat(6, 1fr)', gap: '0.5rem', alignItems: 'end' }}>
        <label htmlFor="pr-name">
          Name
          <input id="pr-name" required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </label>
        <label htmlFor="pr-port">
          Port
          <input
            id="pr-port"
            type="number"
            min={1}
            max={65535}
            required
            value={form.port}
            onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
          />
        </label>
        <label htmlFor="pr-protocol">
          Protocol
          <select id="pr-protocol" value={form.protocol} onChange={(e) => setForm({ ...form, protocol: e.target.value as 'tcp' | 'udp' })}>
            <option value="tcp">tcp</option>
            <option value="udp">udp</option>
          </select>
        </label>
        <label htmlFor="pr-expected">
          Expected
          <select
            id="pr-expected"
            value={form.expected_state}
            onChange={(e) => setForm({ ...form, expected_state: e.target.value as 'open' | 'closed' })}
          >
            <option value="closed">closed</option>
            <option value="open">open</option>
          </select>
        </label>
        <label htmlFor="pr-severity">
          Severity
          <select id="pr-severity" value={form.severity} onChange={(e) => setForm({ ...form, severity: e.target.value })}>
            <option value="low">low</option>
            <option value="medium">medium</option>
            <option value="high">high</option>
            <option value="critical">critical</option>
          </select>
        </label>
        <button type="submit" className="primary-button" disabled={submitting}>
          Add rule
        </button>
      </form>

      <table className="data-table" style={{ marginTop: '1rem', width: '100%' }}>
        <thead>
          <tr>
            <th>Name</th>
            <th>Port</th>
            <th>Proto</th>
            <th>Expected</th>
            <th>Severity</th>
            <th>Enabled</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {rules.length === 0 ? (
            <tr>
              <td colSpan={7} className="muted">No port rules yet.</td>
            </tr>
          ) : (
            rules.map((r) => (
              <tr key={r.id}>
                <td>{r.name}</td>
                <td>{r.port}</td>
                <td>{r.protocol}</td>
                <td>{r.expected_state}</td>
                <td>{r.severity}</td>
                <td>{r.enabled ? 'yes' : 'no'}</td>
                <td>
                  <button type="button" className="secondary-button" onClick={() => setConfirmDeleteId(r.id)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}

function LogRulesPane({
  tenantId,
  rules,
  onRefresh,
}: {
  tenantId: string;
  rules: LogRule[];
  onRefresh: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [form, setForm] = useState<CreateLogRulePayload>({
    tenant_id: tenantId,
    name: '',
    log_source: 'auth',
    pattern: '',
    severity: 'high',
    window_seconds: 60,
    threshold: 3,
    action: 'notify',
    enabled: true,
  });
  const [submitting, setSubmitting] = useState(false);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  useEffect(() => {
    setForm((f) => ({ ...f, tenant_id: tenantId }));
  }, [tenantId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    try {
      await client.createLogRule(form);
      setForm({ ...form, name: '', pattern: '' });
      onRefresh();
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (id: string) => {
    await client.deleteLogRule(id);
    onRefresh();
  };

  return (
    <div>
      <ConfirmModal
        open={confirmDeleteId !== null}
        title="Delete log rule?"
        body="This cannot be undone."
        variant="danger"
        confirmLabel="Delete"
        onConfirm={() => {
          if (confirmDeleteId) remove(confirmDeleteId);
          setConfirmDeleteId(null);
        }}
        onCancel={() => setConfirmDeleteId(null)}
      />
      <form className="form-row" onSubmit={submit} style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: '0.5rem', alignItems: 'end' }}>
        <label htmlFor="lr-name">
          Name
          <input id="lr-name" required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </label>
        <label htmlFor="lr-source">
          Source
          <input id="lr-source" required value={form.log_source} onChange={(e) => setForm({ ...form, log_source: e.target.value })} />
        </label>
        <label htmlFor="lr-pattern" style={{ gridColumn: 'span 2' }}>
          Pattern (regex)
          <input id="lr-pattern" required value={form.pattern} onChange={(e) => setForm({ ...form, pattern: e.target.value })} />
        </label>
        <label htmlFor="lr-window">
          Window (s)
          <input
            id="lr-window"
            type="number"
            min={1}
            value={form.window_seconds}
            onChange={(e) => setForm({ ...form, window_seconds: Number(e.target.value) })}
          />
        </label>
        <label htmlFor="lr-threshold">
          Threshold
          <input
            id="lr-threshold"
            type="number"
            min={1}
            value={form.threshold}
            onChange={(e) => setForm({ ...form, threshold: Number(e.target.value) })}
          />
        </label>
        <button type="submit" className="primary-button" disabled={submitting}>
          Add rule
        </button>
      </form>

      <table className="data-table" style={{ marginTop: '1rem', width: '100%' }}>
        <thead>
          <tr>
            <th>Name</th>
            <th>Source</th>
            <th>Pattern</th>
            <th>Win</th>
            <th>Thresh</th>
            <th>Sev</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {rules.length === 0 ? (
            <tr>
              <td colSpan={7} className="muted">No log rules yet.</td>
            </tr>
          ) : (
            rules.map((r) => (
              <tr key={r.id}>
                <td>{r.name}</td>
                <td>{r.log_source}</td>
                <td><code>{r.pattern}</code></td>
                <td>{r.window_seconds}s</td>
                <td>{r.threshold}</td>
                <td>{r.severity}</td>
                <td>
                  <button type="button" className="secondary-button" onClick={() => setConfirmDeleteId(r.id)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
