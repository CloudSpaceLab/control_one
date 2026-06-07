import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { Panel, SectionHeader, EmptyState, DataTable, StatusTag, SelectField } from '../components/kit';
import { KpiTile } from '../components/kit';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { ConfirmModal } from '../components/ConfirmModal';
import type { ColumnDef } from '@tanstack/react-table';
import { Database, RefreshCw, Search, ShieldAlert, ShieldCheck } from 'lucide-react';
import type { CreateThreatFeedPayload, ThreatFeed, ThreatFeedType, ThreatIntelSummary } from '../lib/api';

interface FeedTypeMeta {
  type: ThreatFeedType;
  label: string;
  description: string;
  needsURL: 'optional' | 'required' | 'never';
  apiKeyMode: 'optional' | 'required' | 'never';
  defaultURL?: string;
}

// FEED_CATALOG describes every feed the platform knows how to fetch. The UI
// adapts the form fields to each entry — built-in feeds need only a name,
// API-backed feeds download into the local blacklist snapshot; keys are for
// upstream refresh, not per-IP scans. Adding a new feed type is a one-line
// entry here plus a case in the Go SourceFromConfig.
const FEED_CATALOG: FeedTypeMeta[] = [
  {
    type: 'spamhaus_drop',
    label: 'Spamhaus DROP',
    description: 'Hijacked / malicious netblocks. Free, no key. Updated daily.',
    needsURL: 'optional',
    apiKeyMode: 'never',
    defaultURL: 'https://www.spamhaus.org/drop/drop.txt',
  },
  {
    type: 'spamhaus_edrop',
    label: 'Spamhaus EDROP',
    description: 'Extended DROP list. Same format as DROP but wider coverage.',
    needsURL: 'optional',
    apiKeyMode: 'never',
    defaultURL: 'https://www.spamhaus.org/drop/edrop.txt',
  },
  {
    type: 'firehol_l1',
    label: 'FireHOL Level 1',
    description: 'Curated aggregate of community blocklists. Low false-positive.',
    needsURL: 'optional',
    apiKeyMode: 'never',
    defaultURL: 'https://iplists.firehol.org/files/firehol_level1.netset',
  },
  {
    type: 'tor_exit',
    label: 'Tor exit nodes',
    description: 'Exit-node IPs. Useful as a separate signal, not always malicious.',
    needsURL: 'optional',
    apiKeyMode: 'never',
    defaultURL: 'https://check.torproject.org/exit-addresses',
  },
  {
    type: 'abuseipdb',
    label: 'AbuseIPDB blocklist',
    description: 'Confidence-scored bad IPs. Downloaded locally; key only refreshes upstream.',
    needsURL: 'never',
    apiKeyMode: 'optional',
  },
  {
    type: 'otx',
    label: 'AlienVault OTX',
    description: 'Pulse-based community intelligence. API key required.',
    needsURL: 'never',
    apiKeyMode: 'required',
  },
  {
    type: 'custom_lines',
    label: 'Custom — line list',
    description: 'Any URL with one IP/CIDR per line. Comments via # or ; allowed.',
    needsURL: 'required',
    apiKeyMode: 'never',
  },
  {
    type: 'custom_spamhaus',
    label: 'Custom — Spamhaus format',
    description: 'Any URL using the "<cidr> ; evidence" format.',
    needsURL: 'required',
    apiKeyMode: 'never',
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

function formatCount(value?: number): string {
  return new Intl.NumberFormat().format(value ?? 0);
}

function formatDateTime(value?: string): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString();
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
  const [summary, setSummary] = useState<ThreatIntelSummary | null>(null);
  const [summaryLoading, setSummaryLoading] = useState(false);
  const [summaryError, setSummaryError] = useState<string | null>(null);
  const [ipQuery, setIpQuery] = useState('');

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  useEffect(() => {
    setForm((prev) => ({ ...prev, tenant_id: tenantId }));
  }, [tenantId]);

  const refresh = useCallback(async (lookupIP?: string) => {
    if (!tenantId) return;
    setLoading(true);
    setSummaryLoading(true);
    const [feedsResp, summaryResp] = await Promise.allSettled([
      client.listThreatFeeds(tenantId),
      client.getThreatIntelSummary({ tenantId, ip: lookupIP?.trim() || undefined }),
    ]);
    if (feedsResp.status === 'fulfilled') {
      setFeeds(feedsResp.value.data);
      setError(null);
    } else {
      setError(feedsResp.reason instanceof Error ? feedsResp.reason.message : 'load failed');
    }
    if (summaryResp.status === 'fulfilled') {
      setSummary(summaryResp.value);
      setSummaryError(null);
    } else {
      setSummaryError(summaryResp.reason instanceof Error ? summaryResp.reason.message : 'blacklist summary failed');
    }
    setLoading(false);
    setSummaryLoading(false);
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
      refresh(summary?.lookup?.ip);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'create failed');
    } finally {
      setSubmitting(false);
    }
  };

  const toggleEnabled = async (feed: ThreatFeed) => {
    await client.updateThreatFeed(feed.id, { enabled: !feed.enabled });
    refresh(summary?.lookup?.ip);
  };

  const remove = async () => {
    if (!confirmId) return;
    await client.deleteThreatFeed(confirmId);
    setConfirmId(null);
    refresh(summary?.lookup?.ip);
  };

  const checkIP = async (e: React.FormEvent) => {
    e.preventDefault();
    await refresh(ipQuery);
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
              : s === 'pending' || s === 'refreshing' || s === 'stale'
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
        description="Choose which feeds to consume. Remote feeds are refreshed into a local blacklist database, so IP checks and scans do not require live reputation API calls."
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

      <Panel
        padding="md"
        eyebrow="RUNTIME BLACKLIST"
        title="Current blacklist database"
        toneAccent={summary?.lookup?.listed ? 'critical' : 'brand'}
        actions={
          <Button
            variant="ghost"
            size="sm"
            onClick={() => refresh(summary?.lookup?.ip)}
            disabled={summaryLoading}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${summaryLoading ? 'animate-spin' : ''}`} /> Refresh
          </Button>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
          <KpiTile
            label="Indicators loaded"
            value={summary?.available ? formatCount(summary.total_indicators) : 'pending'}
            tone={summary?.available ? 'healthy' : 'warning'}
            icon={<Database />}
            loading={summaryLoading && !summary}
          />
          <KpiTile
            label="Active sources"
            value={summary?.available ? String(summary.sources.length) : '0'}
            tone={(summary?.sources.length ?? 0) > 0 ? 'brand' : 'warning'}
            loading={summaryLoading && !summary}
          />
          <KpiTile
            label="Global indicators"
            value={summary?.available ? formatCount(summary.global_indicators) : '0'}
            tone="unknown"
            loading={summaryLoading && !summary}
          />
          <KpiTile
            label="Tenant indicators"
            value={summary?.available ? formatCount(summary.tenant_indicators) : '0'}
            tone={summary?.tenant_indicators ? 'brand' : 'unknown'}
            loading={summaryLoading && !summary}
          />
        </div>

        <form onSubmit={checkIP} className="mt-4 flex flex-col gap-2 md:flex-row md:items-center">
          <Input
            value={ipQuery}
            onChange={(e) => setIpQuery(e.target.value)}
            placeholder="Check an IP, e.g. 45.135.193.156"
            aria-label="Check IP against current blacklist database"
            className="md:max-w-sm"
          />
          <Button type="submit" variant="primary" disabled={!ipQuery.trim() || summaryLoading}>
            <Search className="h-3.5 w-3.5" /> Check IP
          </Button>
          {summary?.generated_at && (
            <span className="font-mono text-xs text-text-muted">
              refreshed {formatDateTime(summary.generated_at)}
            </span>
          )}
        </form>

        {summaryError && <p className="mt-3 text-sm text-state-critical">{summaryError}</p>}

        {summary?.lookup && (
          <div className="mt-4 rounded-md border border-border-subtle bg-surface p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                {summary.lookup.listed ? (
                  <ShieldAlert className="h-4 w-4 text-state-critical" />
                ) : (
                  <ShieldCheck className="h-4 w-4 text-state-healthy" />
                )}
                <span className="font-mono text-sm font-semibold text-foreground">{summary.lookup.ip}</span>
                <StatusTag tone={summary.lookup.listed ? 'critical' : 'healthy'}>
                  {summary.lookup.listed ? 'LISTED' : 'NOT LISTED'}
                </StatusTag>
              </div>
              <span className="font-mono text-xs text-text-muted">
                Confidence {summary.lookup.score}/100
              </span>
            </div>
            <div className="mt-3 h-2 overflow-hidden rounded-full bg-surface-2">
              <div
                className={`h-full ${summary.lookup.listed ? 'bg-state-critical' : 'bg-state-healthy'}`}
                style={{ width: `${Math.max(0, Math.min(summary.lookup.score, 100))}%` }}
              />
            </div>
            <div className="mt-3 flex flex-wrap gap-1.5">
              {summary.lookup.feeds.length > 0 ? summary.lookup.feeds.map((feed) => (
                <StatusTag key={feed} tone="critical">{feed}</StatusTag>
              )) : (
                <span className="text-xs text-text-muted">No matching feed entries.</span>
              )}
            </div>
            {summary.lookup.matches.length > 0 && (
              <div className="mt-3 overflow-x-auto">
                <table className="min-w-full text-left text-xs">
                  <thead className="border-b border-border-subtle uppercase tracking-wider text-text-muted">
                    <tr>
                      <th className="px-2 py-1">Feed</th>
                      <th className="px-2 py-1">Match</th>
                      <th className="px-2 py-1">Score</th>
                      <th className="px-2 py-1">First seen</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {summary.lookup.matches.map((match, idx) => (
                      <tr key={`${match.feed ?? 'feed'}-${match.cidr ?? match.ip ?? idx}`}>
                        <td className="px-2 py-1 font-medium text-foreground">{match.feed ?? 'unknown'}</td>
                        <td className="px-2 py-1 font-mono text-text-secondary">{match.cidr || match.ip || '-'}</td>
                        <td className="px-2 py-1 font-mono text-text-secondary">{match.score}</td>
                        <td className="px-2 py-1 font-mono text-text-secondary">{formatDateTime(match.first_seen)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {summary?.available ? (
          <div className="mt-4 overflow-x-auto rounded-md border border-border-subtle">
            <table className="min-w-full text-left text-sm">
              <thead className="bg-surface-2 text-xs uppercase tracking-wider text-text-muted">
                <tr>
                  <th className="px-3 py-2">Source</th>
                  <th className="px-3 py-2">Scope</th>
                  <th className="px-3 py-2">Indicators</th>
                  <th className="px-3 py-2">Max confidence</th>
                  <th className="px-3 py-2">Sample</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-subtle">
                {summary.sources.map((source) => (
                  <tr key={`${source.scope}-${source.feed}-${source.category ?? ''}`}>
                    <td className="px-3 py-2">
                      <span className="font-medium text-foreground">{source.feed}</span>
                      {source.category && <span className="ml-2 text-xs text-text-muted">{source.category}</span>}
                    </td>
                    <td className="px-3 py-2">
                      <StatusTag tone={source.scope === 'global' ? 'info' : 'healthy'}>{source.scope}</StatusTag>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-text-secondary">{formatCount(source.indicators)}</td>
                    <td className="px-3 py-2 font-mono text-xs text-text-secondary">{source.max_score}/100</td>
                    <td className="px-3 py-2">
                      <span className="font-mono text-xs text-text-muted">
                        {(source.sample ?? []).slice(0, 3).map((item) => item.cidr || item.ip).filter(Boolean).join(', ') || '-'}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="mt-4 text-sm text-text-muted">
            Blacklist cache is still warming up. It appears here after the next successful threat-intel refresh.
          </p>
        )}
      </Panel>

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

          {meta.apiKeyMode !== 'never' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="feed-api-key">
                API key{meta.apiKeyMode === 'optional' ? ' (optional)' : ''}
              </Label>
              <Input
                id="feed-api-key"
                required={meta.apiKeyMode === 'required'}
                type="password"
                value={form.api_key ?? ''}
                onChange={(e) => setForm({ ...form, api_key: e.target.value })}
                placeholder={meta.apiKeyMode === 'optional' ? 'paste key to refresh upstream' : 'paste key'}
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
