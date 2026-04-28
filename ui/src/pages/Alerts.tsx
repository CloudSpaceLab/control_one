import { useCallback, useEffect, useMemo, useState } from 'react';
import { Bell, RefreshCw, ShieldCheck } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Label } from '../components/ui/label';
import {
  DataTable,
  EmptyState,
  EntityChip,
  KpiTile,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
import { classifyValue } from '../lib/entity';
import type { Alert } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

const STATE_FILTERS = ['open', 'acked', 'resolved'] as const;

function severityTone(sev: string | undefined): StateTone {
  switch ((sev ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
      return 'info';
    case 'info':
      return 'info';
    default:
      return 'unknown';
  }
}

function stateTone(state: string): StateTone {
  switch (state) {
    case 'open':
      return 'critical';
    case 'acked':
      return 'warning';
    case 'resolved':
      return 'healthy';
    default:
      return 'unknown';
  }
}

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

  const counts = useMemo(() => {
    const c = { critical: 0, high: 0, total: alerts.length };
    for (const a of alerts) {
      const sev = (a.severity ?? '').toLowerCase();
      if (sev === 'critical') c.critical += 1;
      else if (sev === 'high') c.high += 1;
    }
    return c;
  }, [alerts]);

  const columns = useMemo<ColumnDef<Alert>[]>(() => [
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)} className="font-mono uppercase">
          {row.original.severity || 'unknown'}
        </StatusTag>
      ),
    },
    {
      id: 'title',
      header: 'Title',
      cell: ({ row }) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium text-foreground">{row.original.title}</span>
          {row.original.summary ? (
            <span className="text-xs text-text-muted">{row.original.summary}</span>
          ) : null}
        </div>
      ),
    },
    {
      accessorKey: 'source',
      header: 'Source',
      cell: ({ getValue }) => {
        const v = getValue() as string;
        const det = v ? classifyValue(v) : { type: 'unknown' as const };
        return det.type !== 'unknown' ? (
          <EntityChip type={det.type} value={v} />
        ) : (
          <span className="font-mono text-xs text-text-secondary">{v || '—'}</span>
        );
      },
    },
    {
      accessorKey: 'opened_at',
      header: 'Opened',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums text-text-secondary">
          {new Date(getValue() as string).toLocaleString()}
        </span>
      ),
    },
    {
      accessorKey: 'state',
      header: 'State',
      cell: ({ getValue }) => {
        const s = getValue() as string;
        return <StatusTag tone={stateTone(s)}>{s}</StatusTag>;
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          {row.original.state === 'open' ? (
            <Button variant="secondary" size="sm" onClick={() => ack(row.original.id)}>
              Ack
            </Button>
          ) : null}
          {row.original.state !== 'resolved' ? (
            <Button variant="primary" size="sm" onClick={() => resolve(row.original.id)}>
              Resolve
            </Button>
          ) : null}
        </div>
      ),
    },
    // ack/resolve don't change identity — eslint-disable: dependencies stable via closure
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  const allClear = !loading && alerts.length === 0 && state === 'open';

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="VISIBILITY · ALERTS"
        title="Alerts"
        description="Deduped inbox from correlation, rules, and compliance."
        actions={
          <Button variant="secondary" size="md" onClick={refresh} disabled={loading}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="OPEN ALERTS" value={counts.total} tone={counts.total > 0 ? 'warning' : 'healthy'} />
        <KpiTile label="CRITICAL" value={counts.critical} tone={counts.critical > 0 ? 'critical' : 'healthy'} />
        <KpiTile label="HIGH" value={counts.high} tone={counts.high > 0 ? 'degraded' : 'healthy'} />
      </div>

      <Panel padding="md" eyebrow="FILTERS" title="Refine">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
          <FilterSelect
            label="Tenant"
            value={tenantId}
            onChange={(v) => setTenantId(v)}
            options={tenants.map((t) => ({ label: t.name, value: t.id }))}
          />
          <FilterSelect
            label="State"
            value={state}
            onChange={(v) => setState(v as typeof STATE_FILTERS[number])}
            options={STATE_FILTERS.map((s) => ({ label: s, value: s }))}
          />
        </div>
      </Panel>

      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      )}

      <Panel
        padding="sm"
        tone="inset"
        eyebrow={`ALERTS · ${alerts.length}`}
        title="Inbox"
      >
        <DataTable
          columns={columns}
          rows={alerts}
          rowKey={(r) => r.id}
          loading={loading}
          compact
          empty={
            allClear ? (
              <EmptyState
                tone="success"
                icon={<ShieldCheck />}
                title="All clear"
                description="No open alerts. Detection rules are healthy and the inbox is empty."
              />
            ) : (
              <EmptyState
                icon={<Bell />}
                title="No alerts"
                description={`No alerts in state "${state}".`}
              />
            )
          }
        />
      </Panel>
    </div>
  );
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { label: string; value: string }[];
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="flex h-9 w-full rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  );
}
