import { useMemo, useState } from 'react';
import { Download, FileText, RefreshCw } from 'lucide-react';
import { Button } from '../components/ui/button';
import {
  Chart,
  DataTable,
  EmptyState,
  EntityChip,
  ExpandableCode,
  KpiTile,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { useAuditLogs } from '../hooks/useAuditLogs';
import { useTenants } from '../hooks/useTenants';
import { classifyValue } from '../lib/entity';
import type { AuditLog } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';
import { AuditReports } from './AuditReports';

function formatDate(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function formatRelativeTime(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  const diff = Date.now() - parsed.getTime();
  const m = Math.floor(diff / 60_000);
  if (m < 1) return 'Just now';
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(diff / 3_600_000);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(diff / 86_400_000);
  if (d < 7) return `${d}d ago`;
  return parsed.toLocaleDateString();
}

function actionTone(action: string): StateTone {
  if (action.includes('.delete') || action.includes('.error') || action.includes('.failed')) return 'critical';
  if (action.includes('.create') || action.includes('.success') || action.includes('.succeeded')) return 'healthy';
  if (action.includes('.update')) return 'info';
  return 'unknown';
}

function exportToCSV(logs: AuditLog[]): void {
  const headers = ['Timestamp', 'Actor Type', 'Action', 'Resource Type', 'Resource ID', 'Tenant ID', 'Metadata'];
  const rows = logs.map((log) => [
    log.created_at,
    log.actor_type,
    log.action,
    log.resource_type,
    log.resource_id || '',
    log.tenant_id || '',
    JSON.stringify(log.metadata || {}),
  ]);
  const csv = [headers.join(','), ...rows.map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))].join('\n');
  const blob = new Blob([csv], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `audit-logs-${new Date().toISOString().split('T')[0]}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

export function Audit(): JSX.Element {
  const [tab, setTab] = useState<'logs' | 'reports'>('logs');
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>();
  const [actorTypeFilter, setActorTypeFilter] = useState('');
  const [actionFilter, setActionFilter] = useState('');
  const [resourceTypeFilter, setResourceTypeFilter] = useState('');
  const [search, setSearch] = useState('');
  const [limit] = useState(100);
  const [offset, setOffset] = useState(0);

  const { data: tenants } = useTenants();
  const { data: logs, loading, error, pagination, reload } = useAuditLogs({
    tenant_id: selectedTenant,
    actor_type: actorTypeFilter || undefined,
    action: actionFilter || undefined,
    resource_type: resourceTypeFilter || undefined,
    limit,
    offset,
  });

  const uniqueActions = useMemo(() => {
    const a = new Set<string>();
    logs.forEach((l) => a.add(l.action));
    return Array.from(a).sort();
  }, [logs]);

  const uniqueResourceTypes = useMemo(() => {
    const t = new Set<string>();
    logs.forEach((l) => t.add(l.resource_type));
    return Array.from(t).sort();
  }, [logs]);

  const columns = useMemo<ColumnDef<AuditLog>[]>(() => [
    {
      accessorKey: 'created_at',
      header: 'When',
      cell: ({ getValue }) => (
        <div className="flex flex-col">
          <span className="font-mono text-xs tabular-nums">{formatDate(getValue() as string)}</span>
          <span className="text-[0.65rem] text-text-muted">{formatRelativeTime(getValue() as string)}</span>
        </div>
      ),
    },
    {
      id: 'actor',
      header: 'Actor',
      cell: ({ row }) => (
        <div className="inline-flex items-center gap-2">
          <StatusTag tone={row.original.actor_type === 'system' ? 'info' : 'healthy'}>
            {row.original.actor_type}
          </StatusTag>
          {row.original.actor_id && (
            <span className="font-mono text-[0.65rem] text-text-muted">
              {row.original.actor_id.slice(0, 8)}…
            </span>
          )}
        </div>
      ),
    },
    {
      accessorKey: 'action',
      header: 'Action',
      cell: ({ getValue }) => {
        const a = getValue() as string;
        return <StatusTag tone={actionTone(a)} className="font-mono">{a}</StatusTag>;
      },
    },
    {
      id: 'resource',
      header: 'Resource',
      cell: ({ row }) => {
        const id = row.original.resource_id;
        const det = id ? classifyValue(id) : { type: 'unknown' as const };
        return (
          <div className="flex items-center gap-2">
            <span className="font-mono text-xs text-text-secondary">{row.original.resource_type}</span>
            {id && det.type !== 'unknown' ? (
              <EntityChip type={det.type} value={id} />
            ) : id ? (
              <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] text-text-secondary">
                {id.length > 20 ? `${id.slice(0, 20)}…` : id}
              </code>
            ) : null}
          </div>
        );
      },
    },
    {
      id: 'metadata',
      header: 'Details',
      cell: ({ row }) =>
        row.original.metadata && Object.keys(row.original.metadata).length > 0 ? (
          <ExpandableCode label="View metadata" content={JSON.stringify(row.original.metadata, null, 2)} />
        ) : (
          <span className="text-text-muted">—</span>
        ),
    },
  ], []);

  const filtered = useMemo(() => {
    if (!search.trim()) return logs;
    const q = search.toLowerCase();
    return logs.filter(
      (l) =>
        l.action.toLowerCase().includes(q) ||
        l.resource_type.toLowerCase().includes(q) ||
        (l.resource_id ?? '').toLowerCase().includes(q) ||
        (l.actor_id ?? '').toLowerCase().includes(q),
    );
  }, [logs, search]);

  const userCount = logs.filter((l) => l.actor_type === 'user').length;
  const systemCount = logs.filter((l) => l.actor_type === 'system').length;

  const actionBarData = useMemo(() => {
    const counts: Record<string, number> = {};
    logs.forEach((l) => { counts[l.action] = (counts[l.action] ?? 0) + 1; });
    const sorted = Object.entries(counts).sort((a, b) => b[1] - a[1]).slice(0, 8);
    return {
      labels: sorted.map(([k]) => k),
      datasets: [{
        label: 'Events',
        data: sorted.map(([, v]) => v),
        backgroundColor: 'var(--state-info)',
        borderRadius: 4,
      }],
    };
  }, [logs]);

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE · AUDIT"
        title="Audit trail"
        description="Who did what, when. Full record for SOC 2, ISO 27001, and incident review."
        actions={
          <>
            <Button variant="secondary" size="md" onClick={reload} disabled={loading}>
              <RefreshCw className="h-4 w-4" /> Refresh
            </Button>
            <Button variant="primary" size="md" onClick={() => exportToCSV(logs)} disabled={logs.length === 0}>
              <Download className="h-4 w-4" /> Export CSV
            </Button>
          </>
        }
      />

      <Tabs value={tab} onValueChange={(v) => setTab(v as 'logs' | 'reports')}>
        <TabsList>
          <TabsTrigger value="logs">Audit Log</TabsTrigger>
          <TabsTrigger value="reports">Reports</TabsTrigger>
        </TabsList>

        <TabsContent value="logs" className="mt-4 flex flex-col gap-5">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <KpiTile label="TOTAL EVENTS" value={pagination.total.toLocaleString()} tone="brand" />
            <KpiTile label="USER ACTIONS" value={userCount} tone="healthy" />
            <KpiTile label="SYSTEM EVENTS" value={systemCount} tone="info" />
          </div>

          {logs.length > 0 && (
            <Panel padding="md" eyebrow="ANALYTICS" title="Top actions by volume">
              <Chart
                kind="bar"
                height={180}
                data={actionBarData}
                options={{
                  indexAxis: 'y' as const,
                  plugins: { legend: { display: false } },
                  scales: {
                    x: { grid: { color: 'var(--border-subtle)' } },
                    y: { ticks: { font: { size: 10, family: 'monospace' } } },
                  },
                }}
              />
            </Panel>
          )}

          <Panel padding="md" eyebrow="FILTERS" title="Refine">
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
              <FilterSelect
                label="Tenant"
                value={selectedTenant ?? ''}
                onChange={(v) => { setSelectedTenant(v || undefined); setOffset(0); }}
                options={[
                  { label: 'All tenants', value: '' },
                  ...tenants.map((t) => ({ label: t.name, value: t.id })),
                ]}
              />
              <FilterSelect
                label="Actor type"
                value={actorTypeFilter}
                onChange={(v) => { setActorTypeFilter(v); setOffset(0); }}
                options={[
                  { label: 'All types', value: '' },
                  { label: 'User', value: 'user' },
                  { label: 'System', value: 'system' },
                ]}
              />
              <FilterSelect
                label="Action"
                value={actionFilter}
                onChange={(v) => { setActionFilter(v); setOffset(0); }}
                options={[
                  { label: 'All actions', value: '' },
                  ...uniqueActions.map((a) => ({ label: a, value: a })),
                ]}
              />
              <FilterSelect
                label="Resource"
                value={resourceTypeFilter}
                onChange={(v) => { setResourceTypeFilter(v); setOffset(0); }}
                options={[
                  { label: 'All resources', value: '' },
                  ...uniqueResourceTypes.map((t) => ({ label: t, value: t })),
                ]}
              />
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="audit-search">Search</Label>
                <Input
                  id="audit-search"
                  placeholder="action, resource id…"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              </div>
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
            eyebrow={`AUDIT LOG · ${filtered.length} of ${pagination.total}`}
            title="Entries"
          >
            <DataTable
              columns={columns}
              rows={filtered}
              rowKey={(r) => r.id}
              loading={loading}
              compact
              empty={
                <EmptyState
                  icon={<FileText />}
                  title="No audit entries"
                  description="No events match the current filters."
                />
              }
            />
            <div className="flex items-center justify-between gap-2 border-t border-border-subtle p-3">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setOffset(Math.max(0, offset - limit))}
                disabled={offset === 0 || loading}
              >
                Previous
              </Button>
              <span className="font-mono text-xs text-text-muted">
                Page {Math.floor(offset / limit) + 1} of {Math.ceil(pagination.total / limit) || 1}
              </span>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setOffset(offset + limit)}
                disabled={offset + limit >= pagination.total || loading}
              >
                Next
              </Button>
            </div>
          </Panel>
        </TabsContent>

        <TabsContent value="reports" className="mt-4">
          <AuditReports />
        </TabsContent>
      </Tabs>
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
    <SelectField
      label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </SelectField>
  );
}
