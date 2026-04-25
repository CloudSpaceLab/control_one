import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { Badge, severityToVariant } from '../components/Badge';
import { EmptyState } from '../components/EmptyState';
import { ConfirmModal } from '../components/ConfirmModal';
import type { CreateThreatFeedPayload, ThreatFeed, ThreatFeedType } from '../lib/api';

interface FeedTypeMeta {
  type: ThreatFeedType;
  label: string;
  description: string;
  needsURL: 'optional' | 'required' | 'never';
  needsAPIKey: boolean;
  defaultURL?: string;
}

// FEED_CATALOG describes every feed the platform knows how to fetch. The UI
// adapts the form fields to each entry — built-in feeds need only a name,
// commercial feeds want an API key, custom feeds want a URL. Adding a new
// feed type is a one-line entry here plus a case in the Go SourceFromConfig.
const FEED_CATALOG: FeedTypeMeta[] = [
  {
    type: 'spamhaus_drop',
    label: 'Spamhaus DROP',
    description: 'Hijacked / malicious netblocks. Free, no key. Updated daily.',
    needsURL: 'optional',
    needsAPIKey: false,
    defaultURL: 'https://www.spamhaus.org/drop/drop.txt',
  },
  {
    type: 'spamhaus_edrop',
    label: 'Spamhaus EDROP',
    description: 'Extended DROP list. Same format as DROP but wider coverage.',
    needsURL: 'optional',
    needsAPIKey: false,
    defaultURL: 'https://www.spamhaus.org/drop/edrop.txt',
  },
  {
    type: 'firehol_l1',
    label: 'FireHOL Level 1',
    description: 'Curated aggregate of community blocklists. Low false-positive.',
    needsURL: 'optional',
    needsAPIKey: false,
    defaultURL: 'https://iplists.firehol.org/files/firehol_level1.netset',
  },
  {
    type: 'tor_exit',
    label: 'Tor exit nodes',
    description: 'Exit-node IPs. Useful as a separate signal, not always malicious.',
    needsURL: 'optional',
    needsAPIKey: false,
    defaultURL: 'https://www.dan.me.uk/torlist/?exit',
  },
  {
    type: 'abuseipdb',
    label: 'AbuseIPDB blocklist',
    description: 'Confidence-scored bad IPs. API key required.',
    needsURL: 'never',
    needsAPIKey: true,
  },
  {
    type: 'otx',
    label: 'AlienVault OTX',
    description: 'Pulse-based community intelligence. API key required.',
    needsURL: 'never',
    needsAPIKey: true,
  },
  {
    type: 'custom_lines',
    label: 'Custom — line list',
    description: 'Any URL with one IP/CIDR per line. Comments via # or ; allowed.',
    needsURL: 'required',
    needsAPIKey: false,
  },
  {
    type: 'custom_spamhaus',
    label: 'Custom — Spamhaus format',
    description: 'Any URL using the "<cidr> ; evidence" format.',
    needsURL: 'required',
    needsAPIKey: false,
  },
];

const FEED_META: Record<ThreatFeedType, FeedTypeMeta> = FEED_CATALOG.reduce(
  (acc, m) => ({ ...acc, [m.type]: m }),
  {} as Record<ThreatFeedType, FeedTypeMeta>,
);

function emptyForm(tenantId: string): CreateThreatFeedPayload {
  return {
    tenant_id: tenantId,
    name: '',
    feed_type: 'spamhaus_drop',
    url: FEED_META.spamhaus_drop.defaultURL,
    score_floor: 50,
    refresh_seconds: 3600,
    enabled: true,
  };
}

export function ThreatFeeds(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState('');
  const [feeds, setFeeds] = useState<ThreatFeed[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<CreateThreatFeedPayload>(emptyForm(''));
  const [submitting, setSubmitting] = useState(false);
  const [confirmId, setConfirmId] = useState<string | null>(null);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  useEffect(() => {
    setForm((prev) => ({ ...prev, tenant_id: tenantId }));
  }, [tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    try {
      const resp = await client.listThreatFeeds(tenantId);
      setFeeds(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    setError(null);
    try {
      await client.createThreatFeed({ ...form, tenant_id: tenantId });
      setForm(emptyForm(tenantId));
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'create failed');
    } finally {
      setSubmitting(false);
    }
  };

  const toggleEnabled = async (feed: ThreatFeed) => {
    await client.updateThreatFeed(feed.id, { enabled: !feed.enabled });
    refresh();
  };

  const remove = async () => {
    if (!confirmId) return;
    await client.deleteThreatFeed(confirmId);
    setConfirmId(null);
    refresh();
  };

  const meta = FEED_META[form.feed_type];

  const onTypeChange = (type: ThreatFeedType) => {
    const m = FEED_META[type];
    setForm((prev) => ({
      ...prev,
      feed_type: type,
      url: m.defaultURL ?? '',
      api_key: undefined,
    }));
  };

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Threat intelligence</p>
          <h2>Abuse IP data sources</h2>
          <p className="subtitle">
            Choose which feeds to consume. Built-in lists are free; commercial feeds need an API key. Custom URLs are
            supported for in-house honeypots and partner shares.
          </p>
        </div>
        <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} aria-label="Tenant">
          {tenants.map((t) => (
            <option key={t.id} value={t.id}>{t.name}</option>
          ))}
        </select>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      <form
        onSubmit={submit}
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
          gap: '0.75rem',
          alignItems: 'end',
          padding: '1rem',
          background: 'rgba(255,255,255,0.025)',
          borderRadius: 10,
          border: '1px solid rgba(255,255,255,0.06)',
          marginBottom: '1rem',
        }}
      >
        <label>
          Name
          <input
            required
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="e.g. Spamhaus production"
          />
        </label>
        <label>
          Source
          <select
            value={form.feed_type}
            onChange={(e) => onTypeChange(e.target.value as ThreatFeedType)}
          >
            {FEED_CATALOG.map((m) => (
              <option key={m.type} value={m.type}>{m.label}</option>
            ))}
          </select>
        </label>
        {meta.needsURL !== 'never' ? (
          <label>
            URL{meta.needsURL === 'optional' ? ' (optional)' : ''}
            <input
              required={meta.needsURL === 'required'}
              value={form.url ?? ''}
              onChange={(e) => setForm({ ...form, url: e.target.value })}
              placeholder={meta.defaultURL ?? 'https://example.com/list.txt'}
            />
          </label>
        ) : null}
        {meta.needsAPIKey ? (
          <label>
            API key
            <input
              required
              type="password"
              value={form.api_key ?? ''}
              onChange={(e) => setForm({ ...form, api_key: e.target.value })}
              placeholder="paste key"
              autoComplete="off"
            />
          </label>
        ) : null}
        <label>
          Score floor
          <input
            type="number"
            min={0}
            max={100}
            value={form.score_floor ?? 50}
            onChange={(e) => setForm({ ...form, score_floor: Number(e.target.value) })}
          />
        </label>
        <label>
          Refresh (s)
          <input
            type="number"
            min={60}
            value={form.refresh_seconds ?? 3600}
            onChange={(e) => setForm({ ...form, refresh_seconds: Number(e.target.value) })}
          />
        </label>
        <button type="submit" className="primary-button" disabled={submitting || !tenantId}>
          {submitting ? 'Adding…' : 'Add source'}
        </button>
        <p className="muted" style={{ gridColumn: '1 / -1', margin: 0, fontSize: '0.8rem' }}>
          {meta.description}
        </p>
      </form>

      {loading && feeds.length === 0 ? (
        <p className="muted">Loading sources…</p>
      ) : feeds.length === 0 ? (
        <EmptyState
          title="No threat sources configured yet"
          description="Add Spamhaus DROP for free baseline coverage, or paste a custom URL from your honeypot or SOC team."
        />
      ) : (
        <table className="data-table" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Last refresh</th>
              <th>Indicators</th>
              <th>Status</th>
              <th>Score</th>
              <th>Refresh</th>
              <th>Enabled</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {feeds.map((f) => (
              <tr key={f.id}>
                <td>
                  <strong>{f.name}</strong>
                  {f.url ? <small style={{ display: 'block', color: 'var(--text-muted)' }}>{f.url}</small> : null}
                </td>
                <td>{FEED_META[f.feed_type]?.label ?? f.feed_type}</td>
                <td>{f.last_refreshed_at ? new Date(f.last_refreshed_at).toLocaleString() : '—'}</td>
                <td>{f.last_indicator_count.toLocaleString()}</td>
                <td>
                  {f.last_status === 'ok' ? <Badge variant="success" size="sm">healthy</Badge>
                    : f.last_status === 'error' ? <Badge variant="error" size="sm">error</Badge>
                    : <Badge variant="neutral" size="sm">pending</Badge>}
                  {f.last_error ? <small style={{ display: 'block', color: 'var(--text-muted)', marginTop: 2 }}>{f.last_error}</small> : null}
                </td>
                <td>
                  <Badge variant={severityToVariant(f.score_floor >= 80 ? 'high' : f.score_floor >= 50 ? 'medium' : 'low')} size="sm">
                    ≥ {f.score_floor}
                  </Badge>
                </td>
                <td>{Math.round(f.refresh_seconds / 60)} min</td>
                <td>
                  <button
                    type="button"
                    className={f.enabled ? 'primary-button' : 'secondary-button'}
                    onClick={() => toggleEnabled(f)}
                  >
                    {f.enabled ? 'On' : 'Off'}
                  </button>
                </td>
                <td>
                  <button type="button" className="secondary-button" onClick={() => setConfirmId(f.id)}>
                    Remove
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <ConfirmModal
        open={confirmId !== null}
        title="Remove threat source?"
        body="The platform will stop pulling from this feed on the next refresh tick. Existing indicators in cache stay until they age out."
        variant="danger"
        confirmLabel="Remove"
        onConfirm={remove}
        onCancel={() => setConfirmId(null)}
      />
    </section>
  );
}
