import { useMemo, useState } from 'react';
import { Activity, FileText, RefreshCw } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import {
  Chart,
  DataTable,
  EmptyState,
  ExpandableCode,
  KpiTile,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useTelemetryMetrics, useTelemetryLogs } from '../hooks/useTelemetry';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import type { TelemetryLog, TelemetryMetric } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

function formatDate(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function logLevelTone(level: string): StateTone {
  const v = level.toLowerCase();
  if (v === 'error' || v === 'critical' || v === 'fatal') return 'critical';
  if (v === 'warn' || v === 'warning') return 'warning';
  if (v === 'info') return 'info';
  if (v === 'debug' || v === 'trace') return 'unknown';
  return 'unknown';
}

export function Telemetry(): JSX.Element {
  const [viewMode, setViewMode] = useState<'metrics' | 'logs'>('metrics');
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedNode, setSelectedNode] = useState<string | undefined>(undefined);
  const [metricNameFilter, setMetricNameFilter] = useState<string>('');
  const [logLevelFilter, setLogLevelFilter] = useState<string>('');
  const [logSourceFilter, setLogSourceFilter] = useState<string>('');
  const [search, setSearch] = useState('');
  const [limit] = useState(100);
  const [offset, setOffset] = useState(0);

  const { data: tenants } = useTenants();
  const { data: nodes } = useNodes({ tenantId: selectedTenant, limit: 1000 });

  const {
    data: metrics,
    loading: metricsLoading,
    error: metricsError,
    pagination: metricsPagination,
    reload: reloadMetrics,
  } = useTelemetryMetrics({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    metric_name: metricNameFilter || undefined,
    limit,
    offset: viewMode === 'metrics' ? offset : 0,
  });

  const {
    data: logs,
    loading: logsLoading,
    error: logsError,
    pagination: logsPagination,
    reload: reloadLogs,
  } = useTelemetryLogs({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    log_level: logLevelFilter || undefined,
    log_source: logSourceFilter || undefined,
    limit,
    offset: viewMode === 'logs' ? offset : 0,
  });

  const uniqueMetricNames = useMemo(() => {
    const names = new Set<string>();
    metrics.forEach((m) => names.add(m.metric_name));
    return Array.from(names).sort();
  }, [metrics]);

  const uniqueLogLevels = useMemo(() => {
    const levels = new Set<string>();
    logs.forEach((l) => levels.add(l.log_level));
    return Array.from(levels).sort();
  }, [logs]);

  const uniqueLogSources = useMemo(() => {
    const sources = new Set<string>();
    logs.forEach((l) => {
      if (l.log_source) sources.add(l.log_source);
    });
    return Array.from(sources).sort();
  }, [logs]);

  const handleRefresh = () => {
    if (viewMode === 'metrics') {
      reloadMetrics();
    } else {
      reloadLogs();
    }
  };

  const currentLoading = viewMode === 'metrics' ? metricsLoading : logsLoading;
  const currentError = viewMode === 'metrics' ? metricsError : logsError;

  // KPIs and chart data for metrics view
  const errorCount = useMemo(() => logs.filter((l) => /error|critical|fatal/i.test(l.log_level)).length, [logs]);
  const warnCount = useMemo(() => logs.filter((l) => /warn/i.test(l.log_level)).length, [logs]);

  const chartData = useMemo(() => {
    // Group metric values into time-ordered series, max ~50 points
    const sorted = [...metrics].sort(
      (a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime(),
    );
    const trimmed = sorted.slice(-50);
    return {
      labels: trimmed.map((m) => new Date(m.timestamp).toLocaleTimeString()),
      datasets: [
        {
          label: metricNameFilter || 'metric value',
          data: trimmed.map((m) => m.metric_value),
          borderColor: 'rgb(99, 102, 241)',
          backgroundColor: 'rgba(99, 102, 241, 0.15)',
          fill: true,
          tension: 0.35,
          pointRadius: 0,
        },
      ],
    };
  }, [metrics, metricNameFilter]);

  const metricColumns = useMemo<ColumnDef<TelemetryMetric>[]>(() => [
    {
      accessorKey: 'timestamp',
      header: 'Timestamp',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums">{formatDate(getValue() as string)}</span>
      ),
    },
    {
      accessorKey: 'metric_name',
      header: 'Metric',
      cell: ({ getValue }) => (
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] text-text-secondary">
          {String(getValue())}
        </code>
      ),
    },
    {
      accessorKey: 'metric_value',
      header: 'Value',
      cell: ({ getValue }) => (
        <span className="font-mono tabular-nums">{(getValue() as number).toLocaleString()}</span>
      ),
    },
    {
      accessorKey: 'metric_unit',
      header: 'Unit',
      cell: ({ getValue }) => (getValue() as string) || <span className="text-text-muted">—</span>,
    },
    {
      id: 'node',
      header: 'Node',
      cell: ({ row }) => {
        const node = nodes.find((n) => n.id === row.original.node_id);
        return node?.hostname || row.original.node_id || <span className="text-text-muted">—</span>;
      },
    },
    {
      id: 'labels',
      header: 'Labels',
      cell: ({ row }) =>
        row.original.labels && Object.keys(row.original.labels).length > 0 ? (
          <ExpandableCode label="View labels" content={JSON.stringify(row.original.labels, null, 2)} />
        ) : (
          <span className="text-text-muted">—</span>
        ),
    },
  ], [nodes]);

  const logColumns = useMemo<ColumnDef<TelemetryLog>[]>(() => [
    {
      accessorKey: 'log_level',
      header: 'Level',
      cell: ({ getValue }) => {
        const level = String(getValue());
        return <StatusTag tone={logLevelTone(level)}>{level.toUpperCase()}</StatusTag>;
      },
    },
    {
      accessorKey: 'timestamp',
      header: 'Timestamp',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums">{formatDate(getValue() as string)}</span>
      ),
    },
    {
      accessorKey: 'log_source',
      header: 'Source',
      cell: ({ getValue }) =>
        (getValue() as string) || <span className="text-text-muted">—</span>,
    },
    {
      accessorKey: 'log_program',
      header: 'Program',
      cell: ({ getValue }) =>
        (getValue() as string) || <span className="text-text-muted">—</span>,
    },
    {
      id: 'node',
      header: 'Node',
      cell: ({ row }) => {
        const node = nodes.find((n) => n.id === row.original.node_id);
        return node?.hostname || row.original.node_id || <span className="text-text-muted">—</span>;
      },
    },
    {
      accessorKey: 'log_message',
      header: 'Message',
      cell: ({ getValue }) => (
        <span className="line-clamp-2 break-all text-xs">{String(getValue() ?? '')}</span>
      ),
    },
  ], [nodes]);

  const filteredLogs = useMemo(() => {
    if (!search.trim()) return logs;
    const q = search.toLowerCase();
    return logs.filter(
      (l) =>
        l.log_message.toLowerCase().includes(q) ||
        (l.log_source ?? '').toLowerCase().includes(q) ||
        (l.log_program ?? '').toLowerCase().includes(q),
    );
  }, [logs, search]);

  const filteredMetrics = useMemo(() => {
    if (!search.trim()) return metrics;
    const q = search.toLowerCase();
    return metrics.filter((m) => m.metric_name.toLowerCase().includes(q));
  }, [metrics, search]);

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="OBSERVABILITY · TELEMETRY"
        title="Telemetry dashboard"
        description="Monitor metrics and logs from your infrastructure."
        actions={
          <>
            <div className="inline-flex rounded-md border border-border-subtle bg-surface p-0.5" role="group" aria-label="View mode">
              <Button
                variant={viewMode === 'metrics' ? 'primary' : 'ghost'}
                size="sm"
                onClick={() => {
                  setViewMode('metrics');
                  setOffset(0);
                }}
              >
                Metrics
              </Button>
              <Button
                variant={viewMode === 'logs' ? 'primary' : 'ghost'}
                size="sm"
                onClick={() => {
                  setViewMode('logs');
                  setOffset(0);
                }}
              >
                Logs
              </Button>
            </div>
            <Button variant="secondary" size="md" onClick={handleRefresh} disabled={currentLoading}>
              <RefreshCw className="h-4 w-4" /> Refresh
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="METRIC SAMPLES" value={metricsPagination.total.toLocaleString()} tone="brand" />
        <KpiTile label="LOG ENTRIES" value={logsPagination.total.toLocaleString()} tone="info" />
        <KpiTile
          label="ERRORS / WARNINGS"
          value={`${errorCount} / ${warnCount}`}
          tone={errorCount > 0 ? 'critical' : warnCount > 0 ? 'warning' : 'healthy'}
        />
      </div>

      <Panel padding="md" eyebrow="FILTERS" title="Refine">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
          <FilterSelect
            label="Tenant"
            value={selectedTenant ?? ''}
            onChange={(v) => {
              setSelectedTenant(v || undefined);
              setSelectedNode(undefined);
              setOffset(0);
            }}
            options={[
              { label: 'All tenants', value: '' },
              ...tenants.map((t) => ({ label: t.name, value: t.id })),
            ]}
          />
          <FilterSelect
            label="Node"
            value={selectedNode ?? ''}
            onChange={(v) => {
              setSelectedNode(v || undefined);
              setOffset(0);
            }}
            options={[
              { label: 'All nodes', value: '' },
              ...nodes.map((n) => ({ label: n.hostname, value: n.id })),
            ]}
            disabled={!selectedTenant}
          />
          {viewMode === 'metrics' ? (
            <FilterSelect
              label="Metric name"
              value={metricNameFilter}
              onChange={(v) => {
                setMetricNameFilter(v);
                setOffset(0);
              }}
              options={[
                { label: 'All metrics', value: '' },
                ...uniqueMetricNames.map((n) => ({ label: n, value: n })),
              ]}
            />
          ) : (
            <>
              <FilterSelect
                label="Log level"
                value={logLevelFilter}
                onChange={(v) => {
                  setLogLevelFilter(v);
                  setOffset(0);
                }}
                options={[
                  { label: 'All levels', value: '' },
                  ...uniqueLogLevels.map((lvl) => ({ label: lvl.toUpperCase(), value: lvl })),
                ]}
              />
              <FilterSelect
                label="Log source"
                value={logSourceFilter}
                onChange={(v) => {
                  setLogSourceFilter(v);
                  setOffset(0);
                }}
                options={[
                  { label: 'All sources', value: '' },
                  ...uniqueLogSources.map((src) => ({ label: src, value: src })),
                ]}
              />
            </>
          )}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="telemetry-search">Search</Label>
            <Input
              id="telemetry-search"
              placeholder={viewMode === 'metrics' ? 'metric name…' : 'message, source…'}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
        </div>
      </Panel>

      {currentError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load">
          <p className="text-sm text-state-critical">{currentError}</p>
        </Panel>
      )}

      {viewMode === 'metrics' ? (
        <>
          {metrics.length > 0 && (
            <Panel padding="md" eyebrow="TIME SERIES" title={metricNameFilter || 'Recent metric values'}>
              <Chart kind="line" data={chartData} height={220} ariaLabel="metric time series" />
            </Panel>
          )}
          <Panel
            padding="sm"
            tone="inset"
            eyebrow={`METRICS · ${filteredMetrics.length} of ${metricsPagination.total}`}
            title="Samples"
          >
            <DataTable
              columns={metricColumns}
              rows={filteredMetrics}
              rowKey={(r) => r.id}
              loading={currentLoading}
              compact
              empty={
                <EmptyState
                  icon={<Activity />}
                  title="No metrics"
                  description="No metric samples match the current filters."
                />
              }
            />
            <Pagination
              offset={offset}
              limit={limit}
              total={metricsPagination.total}
              loading={currentLoading}
              onPrev={() => setOffset(Math.max(0, offset - limit))}
              onNext={() => setOffset(offset + limit)}
            />
          </Panel>
        </>
      ) : (
        <Panel
          padding="sm"
          tone="inset"
          eyebrow={`LOGS · ${filteredLogs.length} of ${logsPagination.total}`}
          title="Entries"
        >
          <DataTable
            columns={logColumns}
            rows={filteredLogs}
            rowKey={(r) => r.id}
            loading={currentLoading}
            compact
            empty={
              <EmptyState
                icon={<FileText />}
                title="No logs"
                description="No log entries match the current filters."
              />
            }
          />
          <Pagination
            offset={offset}
            limit={limit}
            total={logsPagination.total}
            loading={currentLoading}
            onPrev={() => setOffset(Math.max(0, offset - limit))}
            onNext={() => setOffset(offset + limit)}
          />
        </Panel>
      )}
    </div>
  );
}

function Pagination({
  offset,
  limit,
  total,
  loading,
  onPrev,
  onNext,
}: {
  offset: number;
  limit: number;
  total: number;
  loading: boolean;
  onPrev: () => void;
  onNext: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2 border-t border-border-subtle p-3">
      <Button variant="secondary" size="sm" onClick={onPrev} disabled={offset === 0 || loading}>
        Previous
      </Button>
      <span className="font-mono text-xs text-text-muted">
        Page {Math.floor(offset / limit) + 1} of {Math.ceil(total / limit) || 1}
      </span>
      <Button
        variant="secondary"
        size="sm"
        onClick={onNext}
        disabled={offset + limit >= total || loading}
      >
        Next
      </Button>
    </div>
  );
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { label: string; value: string }[];
  disabled?: boolean;
}) {
  return (
    <SelectField
      label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </SelectField>
  );
}
