import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { Panel, SectionHeader, EmptyState, DataTable, StatusTag, SelectField } from '../components/kit';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { ConfirmModal } from '../components/ConfirmModal';
import type { ColumnDef } from '@tanstack/react-table';
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

  const columns = useMemo<ColumnDef<ThreatFeed, unknown>[]>(
    () => [
      {
        id: 'name',
        header: 'Name',
        accessorKey: 'name',
        cell: ({ row }) => (
          <span className="font-medium text-foreground">{row.original.name}</span>
        ),
      },
      {
        id: 'type',
        header: 'Type',
        accessorKey: 'feed_type',
        cell: ({ row }) => FEED_META[row.original.feed_type]?.label ?? row.original.feed_type,
      },
      {
        id: 'last_refresh',
        header: 'Last refresh',
        accessorKey: 'last_refreshed_at',
        cell: ({ row }) =>
          row.original.last_refreshed_at
            ? new Date(row.original.last_refreshed_at).toLocaleString()
            : '—',
      },
      {
        id: 'indicators',
        header: 'Indicators',
        accessorKey: 'last_indicator_count',
        cell: ({ row }) =>
          row.original.last_indicator_count != null
            ? row.original.last_indicator_count.toLocaleString()
            : '—',
      },
      {
        id: 'status',
        header: 'Status',
        accessorKey: 'last_status',
        cell: ({ row }) => {
          const s = row.original.last_status;
          const tone =
            s === 'active' || s === 'ok'
              ? 'healthy'
              : s === 'error' || s === 'failed'
              ? 'critical'
              : s === 'pending' || s === 'refreshing'
              ? 'warning'
              : 'unknown';
          return <StatusTag tone={tone}>{s ?? 'unknown'}</StatusTag>;
        },
      },
      {
        id: 'score_floor',
        header: 'Score floor',
        accessorKey: 'score_floor',
        cell: ({ row }) => row.original.score_floor,
      },
      {
        id: 'refresh',
        header: 'Refresh',
        accessorKey: 'refresh_seconds',
        cell: ({ row }) => `${row.original.refresh_seconds}s`,
      },
      {
        id: 'enabled',
        header: 'Enabled',
        accessorKey: 'enabled',
        cell: ({ row }) => (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => toggleEnabled(row.original)}
          >
            {row.original.enabled ? 'Enabled' : 'Disabled'}
          </Button>
        ),
      },
      {
        id: 'actions',
        header: '',
        cell: ({ row }) => (
          <Button
            variant="danger"
            size="sm"
            onClick={() => setConfirmId(row.original.id)}
          >
            Remove
          </Button>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="DETECT & RESPOND · THREAT FEEDS"
        title="Abuse IP data sources"
        description="Choose which feeds to consume. Built-in lists are free; commercial feeds need an API key. Custom URLs are supported for in-house honeypots and partner shares."
        actions={
          <SelectField
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
            aria-label="Tenant"
          >
            {tenants.map((t) => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </SelectField>
        }
      />

      <Panel padding="md" eyebrow="ADD SOURCE" title="Register a threat feed" toneAccent="brand">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <SelectField
              id="feed-type"
              label="Source"
              value={form.feed_type}
              onChange={(e) => onTypeChange(e.target.value as ThreatFeedType)}
            >
              {FEED_CATALOG.map((m) => (
                <option key={m.type} value={m.type}>{m.label}</option>
              ))}
            </SelectField>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="feed-name">Name</Label>
              <Input
                id="feed-name"
                required
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="e.g. Spamhaus production"
              />
            </div>
          </div>

          {FEED_CATALOG.find((c) => c.type === form.feed_type)?.description && (
            <p className="text-xs text-text-muted">
              {FEED_CATALOG.find((c) => c.type === form.feed_type)?.description}
            </p>
          )}

          {meta.needsURL !== 'never' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="feed-url">
                URL{meta.needsURL === 'optional' ? ' (optional)' : ''}
              </Label>
              <Input
                id="feed-url"
                required={meta.needsURL === 'required'}
                value={form.url ?? ''}
                onChange={(e) => setForm({ ...form, url: e.target.value })}
                placeholder={meta.defaultURL ?? 'https://example.com/list.txt'}
              />
            </div>
          )}

          {meta.needsAPIKey && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="feed-api-key">API key</Label>
              <Input
                id="feed-api-key"
                required
                type="password"
                value={form.api_key ?? ''}
                onChange={(e) => setForm({ ...form, api_key: e.target.value })}
                placeholder="paste key"
                autoComplete="off"
              />
            </div>
          )}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="feed-score-floor">Score floor</Label>
              <Input
                id="feed-score-floor"
                type="number"
                min={0}
                max={100}
                value={form.score_floor ?? 50}
                onChange={(e) => setForm({ ...form, score_floor: Number(e.target.value) })}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="feed-refresh">Refresh (seconds)</Label>
              <Input
                id="feed-refresh"
                type="number"
                min={60}
                value={form.refresh_seconds ?? 3600}
                onChange={(e) => setForm({ ...form, refresh_seconds: Number(e.target.value) })}
              />
            </div>
          </div>

          {error && <p className="text-sm text-state-critical">{error}</p>}

          <div className="flex items-center justify-end gap-2">
            <Button type="submit" variant="primary" loading={submitting}>
              Add source
            </Button>
          </div>
        </form>
      </Panel>

      <DataTable<ThreatFeed>
        columns={columns}
        rows={feeds}
        rowKey={(row) => row.id}
        loading={loading && feeds.length === 0}
        empty={
          <EmptyState
            title="No threat feeds configured"
            description="Add a source above to start enriching alerts with threat intelligence."
          />
        }
      />

      <ConfirmModal
        open={confirmId !== null}
        title="Remove threat source?"
        body="The platform will stop pulling from this feed on the next refresh tick. Existing indicators in cache stay until they age out."
        variant="danger"
        confirmLabel="Remove"
        onConfirm={remove}
        onCancel={() => setConfirmId(null)}
      />
    </div>
  );
}
