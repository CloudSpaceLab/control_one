import { useMemo, useState } from 'react';
import { Download, FileText, RefreshCw } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Label } from '../components/ui/label';
import {
  Chart,
  DataTable,
  EmptyState,
  KpiTile,
  Panel,
  PostureBar,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useComplianceResults, useComplianceSummary, useComplianceTrends } from '../hooks/useCompliance';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import type { ComplianceResult } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

function severityTone(severity?: string): StateTone {
  switch ((severity ?? '').toLowerCase()) {
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

function exportToCSV(results: ComplianceResult[]): void {
  const headers = ['ID', 'Rule ID', 'Node ID', 'Passed', 'Severity', 'Checked At', 'Details'];
  const rows = results.map((r) => [
    r.id,
    r.rule_id,
    r.node_id || '',
    r.passed ? 'Yes' : 'No',
    r.severity || '',
    r.checked_at || '',
    r.details || '',
  ]);

  const csv = [headers.join(','), ...rows.map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))].join('\n');
  const blob = new Blob([csv], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `compliance-results-${new Date().toISOString().split('T')[0]}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

export function Compliance(): JSX.Element {
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedNode, setSelectedNode] = useState<string | undefined>(undefined);
  const [severityFilter, setSeverityFilter] = useState<string>('');
  const [passedFilter, setPassedFilter] = useState<boolean | undefined>(undefined);
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);

  const { data: tenants } = useTenants();
  const { data: nodes } = useNodes({ tenantId: selectedTenant, limit: 1000 });

  const {
    data: summary,
    loading: summaryLoading,
    error: summaryError,
    reload: reloadSummary,
  } = useComplianceSummary({
    tenant_id: selectedTenant,
    node_id: selectedNode,
  });

  const {
    data: trends,
    loading: trendsLoading,
    // error intentionally unused — trends errors handled by loading state
  } = useComplianceTrends({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    days: 30,
  });

  const {
    data: results,
    loading: resultsLoading,
    error: resultsError,
    pagination,
    reload: reloadResults,
  } = useComplianceResults({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    severity: severityFilter || undefined,
    passed: passedFilter,
    limit,
    offset,
  });

  const complianceScore = useMemo(() => {
    if (!summary || summary.total === 0) return null;
    return Math.round((summary.passed / summary.total) * 100);
  }, [summary]);

  const severityBreakdown = useMemo(() => {
    if (!summary) return [];
    return Object.entries(summary.by_severity || {})
      .map(([severity, count]) => ({ severity, count }))
      .sort((a, b) => {
        const order: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3, info: 4 };
        return (order[a.severity.toLowerCase()] ?? 99) - (order[b.severity.toLowerCase()] ?? 99);
      });
  }, [summary]);

  const handleExport = () => {
    if (results.length === 0) return;
    exportToCSV(results);
  };

  const handleRefresh = () => {
    reloadSummary();
    reloadResults();
  };

  const columns = useMemo<ColumnDef<ComplianceResult>[]>(() => [
    {
      accessorKey: 'rule_id',
      header: 'Rule ID',
      cell: ({ getValue }) => (
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] text-text-secondary">
          {getValue() as string}
        </code>
      ),
    },
    {
      id: 'node',
      header: 'Node',
      cell: ({ row }) => {
        const node = nodes.find((n) => n.id === row.original.node_id);
        return (
          <span className="text-sm text-foreground">
            {node?.hostname || row.original.node_id || '—'}
          </span>
        );
      },
    },
    {
      accessorKey: 'passed',
      header: 'Status',
      cell: ({ getValue }) => {
        const passed = getValue() as boolean;
        return (
          <StatusTag tone={passed ? 'healthy' : 'critical'}>
            {passed ? 'Passed' : 'Failed'}
          </StatusTag>
        );
      },
    },
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ getValue }) => {
        const sev = getValue() as string | undefined;
        if (!sev) return <span className="text-text-muted">—</span>;
        return (
          <StatusTag tone={severityTone(sev)} className="font-mono uppercase">
            {sev}
          </StatusTag>
        );
      },
    },
    {
      accessorKey: 'checked_at',
      header: 'Checked At',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums text-text-secondary">
          {formatDate(getValue() as string)}
        </span>
      ),
    },
    {
      accessorKey: 'details',
      header: 'Details',
      cell: ({ getValue }) => {
        const d = getValue() as string | undefined;
        return d ? (
          <details>
            <summary className="cursor-pointer text-xs text-text-secondary hover:text-foreground">
              View details
            </summary>
            <pre className="mt-1 overflow-x-auto rounded-md border border-border-subtle bg-surface-2 p-2 font-mono text-[0.7rem] leading-relaxed">
              {d}
            </pre>
          </details>
        ) : (
          <span className="text-text-muted">—</span>
        );
      },
    },
  ], [nodes]);

  const trendChartData = useMemo(() => {
    if (!trends.length) return null;
    const labels = trends.map((t) =>
      new Date(t.date).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
    );
    return {
      labels,
      datasets: [
        {
          label: 'Passed',
          data: trends.map((t) => t.passed),
          borderColor: 'var(--state-healthy)',
          backgroundColor: 'var(--state-healthy)',
        },
        {
          label: 'Failed',
          data: trends.map((t) => t.failed),
          borderColor: 'var(--state-critical)',
          backgroundColor: 'var(--state-critical)',
        },
      ],
    };
  }, [trends]);

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE · COMPLIANCE"
        title="Compliance posture"
        description="Find violations, fix them, prove continuous control."
        actions={
          <>
            <Button variant="secondary" size="md" onClick={handleRefresh} disabled={summaryLoading || resultsLoading}>
              <RefreshCw className="h-4 w-4" /> Refresh
            </Button>
            <Button variant="primary" size="md" onClick={handleExport} disabled={results.length === 0}>
              <Download className="h-4 w-4" /> Export CSV
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <KpiTile
          label="COMPLIANCE SCORE"
          value={complianceScore !== null ? `${complianceScore}%` : '—'}
          tone={
            complianceScore === null
              ? 'unknown'
              : complianceScore >= 80
              ? 'healthy'
              : complianceScore >= 60
              ? 'warning'
              : 'critical'
          }
          loading={summaryLoading}
          hint={
            summary ? `${summary.passed} of ${summary.total} checks passed` : undefined
          }
        />
        <KpiTile
          label="TOTAL CHECKS"
          value={summary?.total ?? 0}
          tone="brand"
          loading={summaryLoading}
        />
        <KpiTile
          label="PASSED"
          value={summary?.passed ?? 0}
          tone="healthy"
          loading={summaryLoading}
        />
        <KpiTile
          label="FAILED"
          value={summary?.failed ?? 0}
          tone={summary && summary.failed > 0 ? 'critical' : 'healthy'}
          loading={summaryLoading}
        />
      </div>

      {complianceScore !== null && (
        <Panel padding="md" eyebrow="FRAMEWORK SCORE" title="Posture">
          <PostureBar
            score={complianceScore}
            ariaLabel={`Compliance score ${complianceScore}%`}
            showLabels
          />
        </Panel>
      )}

      <Panel padding="md" eyebrow="FILTERS" title="Refine">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
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
          <FilterSelect
            label="Severity"
            value={severityFilter}
            onChange={(v) => {
              setSeverityFilter(v);
              setOffset(0);
            }}
            options={[
              { label: 'All severities', value: '' },
              { label: 'Critical', value: 'critical' },
              { label: 'High', value: 'high' },
              { label: 'Medium', value: 'medium' },
              { label: 'Low', value: 'low' },
            ]}
          />
          <FilterSelect
            label="Status"
            value={passedFilter === undefined ? '' : passedFilter.toString()}
            onChange={(v) => {
              setPassedFilter(v === '' ? undefined : v === 'true');
              setOffset(0);
            }}
            options={[
              { label: 'All', value: '' },
              { label: 'Passed', value: 'true' },
              { label: 'Failed', value: 'false' },
            ]}
          />
        </div>
      </Panel>

      {summaryError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load summary">
          <p className="text-sm text-state-critical">{summaryError}</p>
        </Panel>
      )}

      {resultsError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load results">
          <p className="text-sm text-state-critical">{resultsError}</p>
        </Panel>
      )}

      {severityBreakdown.length > 0 && (
        <Panel padding="md" eyebrow="VIOLATIONS" title="By severity">
          <div className="flex flex-wrap gap-3">
            {severityBreakdown.map(({ severity, count }) => (
              <div
                key={severity}
                className="flex items-center gap-2 rounded-md border border-border-subtle bg-surface px-3 py-1.5"
              >
                <StatusTag tone={severityTone(severity)} className="font-mono uppercase">
                  {severity}
                </StatusTag>
                <span className="font-mono text-sm tabular-nums text-foreground">{count}</span>
              </div>
            ))}
          </div>
        </Panel>
      )}

      {trendChartData && (
        <Panel padding="md" eyebrow="TRENDS · 30 DAYS" title="Compliance trend" loading={trendsLoading}>
          <div className="h-56">
            <Chart kind="line" data={trendChartData} ariaLabel="Compliance trend" />
          </div>
        </Panel>
      )}

      <Panel
        padding="sm"
        tone="inset"
        eyebrow={`RESULTS · ${results.length} of ${pagination.total}`}
        title="Compliance results"
      >
        <DataTable
          columns={columns}
          rows={results}
          rowKey={(r) => r.id}
          loading={resultsLoading}
          compact
          empty={
            <EmptyState
              icon={<FileText />}
              title="No compliance results"
              description="No results match the current filters."
            />
          }
        />
        <div className="flex items-center justify-between gap-2 border-t border-border-subtle p-3">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setOffset(Math.max(0, offset - limit))}
            disabled={offset === 0 || resultsLoading}
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
            disabled={offset + limit >= pagination.total || resultsLoading}
          >
            Next
          </Button>
        </div>
      </Panel>
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
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <select
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className="flex h-9 w-full rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:cursor-not-allowed disabled:opacity-60"
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
