import { useEffect, useState } from 'react';
import { SectionHeader, Panel, EmptyState, DataTable, StatusTag } from '../components/kit';
import { Button } from '@/components/ui/button';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import type { BehavioralBaseline, BehavioralAnomaly } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';
import { Activity } from 'lucide-react';

type Tab = 'baselines' | 'anomalies';

function formatDate(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString();
}

export function Behavioral(): JSX.Element {
  const api = useApiClient();
  const { data: tenants } = useTenants();
  const [tab, setTab] = useState<Tab>('baselines');
  const [tenantId, setTenantId] = useState('');
  const [reloadToken, setReloadToken] = useState(0);

  const [baselines, setBaselines] = useState<BehavioralBaseline[]>([]);
  const [baselinesLoading, setBaselinesLoading] = useState(true);

  const [anomalies, setAnomalies] = useState<BehavioralAnomaly[]>([]);
  const [anomaliesLoading, setAnomaliesLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setBaselinesLoading(true);
    api
      .listBehavioralBaselines({ tenantId: tenantId || undefined })
      .then((r) => {
        if (!cancelled) setBaselines(r.data ?? []);
      })
      .catch(() => {
        if (!cancelled) setBaselines([]);
      })
      .finally(() => {
        if (!cancelled) setBaselinesLoading(false);
      });
    return () => { cancelled = true; };
  }, [api, tenantId, reloadToken]);

  useEffect(() => {
    let cancelled = false;
    setAnomaliesLoading(true);
    api
      .listAnomalies({ tenantId: tenantId || undefined, resolved: false })
      .then((r) => {
        if (!cancelled) setAnomalies(r.data ?? []);
      })
      .catch(() => {
        if (!cancelled) setAnomalies([]);
      })
      .finally(() => {
        if (!cancelled) setAnomaliesLoading(false);
      });
    return () => { cancelled = true; };
  }, [api, tenantId, reloadToken]);

  const baselineColumns: ColumnDef<BehavioralBaseline>[] = [
    {
      header: 'Metric',
      accessorKey: 'metric',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.metric}</span>,
    },
    {
      header: 'Window',
      accessorKey: 'window',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.window}</span>,
    },
    {
      header: 'Mean',
      accessorKey: 'mean',
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.mean.toFixed(2)}</span>,
    },
    {
      header: 'Std dev',
      accessorKey: 'stddev',
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.stddev.toFixed(2)}</span>,
    },
    {
      header: 'Samples',
      accessorKey: 'sample_count',
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.sample_count}</span>,
    },
    {
      header: 'Node',
      accessorKey: 'node_id',
      cell: ({ row }) => (
        <code className="font-mono text-xs text-text-muted">{row.original.node_id ?? 'all'}</code>
      ),
    },
    {
      header: 'Updated',
      accessorKey: 'updated_at',
      cell: ({ row }) => (
        <span className="text-xs text-text-muted">{formatDate(row.original.updated_at)}</span>
      ),
    },
  ];

  const anomalyColumns: ColumnDef<BehavioralAnomaly>[] = [
    {
      header: 'Metric',
      accessorKey: 'metric',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.metric}</span>,
    },
    {
      header: 'Observed',
      accessorKey: 'observed_value',
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.observed_value.toFixed(2)}</span>
      ),
    },
    {
      header: 'Z-score',
      accessorKey: 'z_score',
      cell: ({ row }) => {
        const z = row.original.z_score;
        const tone = z > 4 ? 'critical' : z > 2.5 ? 'warning' : 'unknown';
        return <StatusTag tone={tone}>{z.toFixed(2)}</StatusTag>;
      },
    },
    {
      header: 'Status',
      accessorKey: 'resolved',
      cell: ({ row }) =>
        row.original.resolved ? (
          <StatusTag tone="healthy">Resolved</StatusTag>
        ) : (
          <StatusTag tone="warning">Open</StatusTag>
        ),
    },
    {
      header: 'Node',
      accessorKey: 'node_id',
      cell: ({ row }) => (
        <code className="font-mono text-xs text-text-muted">{row.original.node_id ?? '—'}</code>
      ),
    },
    {
      header: 'Detected',
      accessorKey: 'created_at',
      cell: ({ row }) => (
        <span className="text-xs text-text-muted">{formatDate(row.original.created_at)}</span>
      ),
    },
  ];

  const tabClass = (t: Tab) =>
    `px-4 py-2 text-sm font-medium rounded-t-md transition-colors ${
      tab === t
        ? 'bg-surface text-foreground border border-border-subtle border-b-surface'
        : 'text-text-secondary hover:text-foreground'
    }`;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="SECURITY · BEHAVIORAL"
        title="Behavioral analytics"
        description="Baseline deviations and runtime anomaly detection across the fleet."
      />

      <div className="flex items-center gap-3">
        <select
          className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-brand-500"
          value={tenantId}
          onChange={(e) => setTenantId(e.target.value)}
        >
          <option value="">All tenants</option>
          {tenants.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
      </div>

      <div className="flex gap-1 border-b border-border-subtle">
        <button type="button" className={tabClass('baselines')} onClick={() => setTab('baselines')}>
          Baselines
        </button>
        <button type="button" className={tabClass('anomalies')} onClick={() => setTab('anomalies')}>
          Anomalies
          {anomalies.length > 0 && (
            <span className="ml-1.5 rounded-full bg-state-warning/20 px-1.5 py-0.5 text-xs text-state-warning">
              {anomalies.length}
            </span>
          )}
        </button>
      </div>

      {tab === 'baselines' && (
        <Panel
          padding="md"
          eyebrow="BASELINES"
          title="Learned baselines"
          actions={
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setReloadToken((n) => n + 1)}
              disabled={baselinesLoading}
            >
              Refresh
            </Button>
          }
        >
          <DataTable
            columns={baselineColumns}
            rows={baselines}
            loading={baselinesLoading}
            rowKey={(row) => row.id}
            empty={
              <EmptyState
                title="No baselines yet"
                description="Baselines are computed automatically as the collector accumulates metric samples."
                icon={<Activity />}
              />
            }
          />
        </Panel>
      )}

      {tab === 'anomalies' && (
        <Panel
          padding="md"
          eyebrow="ANOMALIES"
          title="Open anomalies"
          actions={
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setReloadToken((n) => n + 1)}
              disabled={anomaliesLoading}
            >
              Refresh
            </Button>
          }
        >
          <DataTable
            columns={anomalyColumns}
            rows={anomalies}
            loading={anomaliesLoading}
            rowKey={(row) => row.id}
            empty={
              <EmptyState
                title="No open anomalies"
                description="All behavioral metrics are within learned baseline bounds."
                icon={<Activity />}
              />
            }
          />
        </Panel>
      )}
    </div>
  );
}
