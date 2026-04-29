import { useCallback, useEffect, useState } from 'react';
import { Download, RefreshCw, FileText } from 'lucide-react';
import { Button } from '../components/ui/button';
import {
  DataTable,
  EmptyState,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import type { AuditReport } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

const FRAMEWORKS = ['SOC2', 'ISO27001', 'HIPAA', 'PCI-DSS', 'GDPR'];

function statusTone(status: string): StateTone {
  switch (status) {
    case 'ready':
      return 'healthy';
    case 'failed':
      return 'critical';
    default:
      return 'warning';
  }
}

function formatDate(v?: string | null): string {
  if (!v) return '—';
  const d = new Date(v);
  return isNaN(d.getTime()) ? v : d.toLocaleDateString();
}

export function AuditReports(): JSX.Element {
  const client = useApiClient();
  const { data: tenantList } = useTenants();

  const [selectedTenant, setSelectedTenant] = useState<string>('');
  const [reports, setReports] = useState<AuditReport[]>([]);
  const [loading, setLoading] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [genError, setGenError] = useState<string | null>(null);

  const [genForm, setGenForm] = useState({
    framework: FRAMEWORKS[0],
    period_start: '',
    period_end: '',
  });

  const load = useCallback(async () => {
    if (!selectedTenant) return;
    setLoading(true);
    try {
      const res = await client.listAuditReports({ tenantId: selectedTenant, limit: 50 });
      setReports(res.data);
    } catch {
      setReports([]);
    } finally {
      setLoading(false);
    }
  }, [client, selectedTenant]);

  useEffect(() => {
    void load();
  }, [load]);

  // Auto-select first tenant
  useEffect(() => {
    if (!selectedTenant && tenantList.length > 0) {
      setSelectedTenant(tenantList[0].id);
    }
  }, [tenantList, selectedTenant]);

  // Auto-refresh every 10 seconds to pick up status changes
  useEffect(() => {
    const id = setInterval(() => {
      void load();
    }, 10_000);
    return () => clearInterval(id);
  }, [load]);

  const handleGenerate = async () => {
    if (!selectedTenant || !genForm.period_start || !genForm.period_end) {
      setGenError('All fields are required.');
      return;
    }
    setGenerating(true);
    setGenError(null);
    try {
      await client.createAuditReport({
        tenant_id: selectedTenant,
        framework: genForm.framework,
        period_start: genForm.period_start,
        period_end: genForm.period_end,
      });
      void load();
    } catch (err: unknown) {
      setGenError(err instanceof Error ? err.message : 'Failed to create report');
    } finally {
      setGenerating(false);
    }
  };

  const columns: ColumnDef<AuditReport, unknown>[] = [
    { accessorKey: 'framework', header: 'Framework' },
    {
      accessorKey: 'period_start',
      header: 'Period Start',
      cell: ({ getValue }) => <span>{formatDate(getValue() as string)}</span>,
    },
    {
      accessorKey: 'period_end',
      header: 'Period End',
      cell: ({ getValue }) => <span>{formatDate(getValue() as string)}</span>,
    },
    {
      accessorKey: 'status',
      header: 'Status',
      cell: ({ getValue }) => {
        const v = getValue() as string;
        return <StatusTag tone={statusTone(v)}>{v}</StatusTag>;
      },
    },
    {
      accessorKey: 'generated_at',
      header: 'Generated',
      cell: ({ getValue }) => <span>{formatDate(getValue() as string | undefined)}</span>,
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => window.open(client.buildReportDownloadUrl(row.original.id), '_blank')}
          title="Download report"
        >
          <Download className="w-3.5 h-3.5" />
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <SectionHeader
        title="Audit Reports"
        description="Generate and download compliance audit reports."
      />

      <div className="flex flex-wrap gap-3 items-center">
        <select
          className="border rounded px-3 py-1.5 text-sm bg-background"
          value={selectedTenant}
          onChange={(e) => setSelectedTenant(e.target.value)}
        >
          <option value="">Select tenant...</option>
          {tenantList.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
        <Button variant="outline" size="sm" onClick={() => void load()}>
          <RefreshCw className="w-3.5 h-3.5 mr-1.5" />
          Refresh
        </Button>
      </div>

      {loading ? (
        <div className="text-muted-foreground text-sm py-4">Loading...</div>
      ) : reports.length === 0 ? (
        <EmptyState
          title="No reports yet"
          description="Generate your first compliance audit report below."
        />
      ) : (
        <DataTable columns={columns} rows={reports} />
      )}

      <Panel>
        <div className="p-4 flex flex-col gap-3">
          <h3 className="font-semibold text-sm flex items-center gap-2">
            <FileText className="w-4 h-4" />
            Generate report
          </h3>

          {genError && <p className="text-sm text-destructive">{genError}</p>}

          <div className="grid grid-cols-3 gap-3">
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium">Framework</label>
              <select
                className="border rounded px-3 py-1.5 text-sm bg-background"
                value={genForm.framework}
                onChange={(e) => setGenForm((p) => ({ ...p, framework: e.target.value }))}
              >
                {FRAMEWORKS.map((f) => (
                  <option key={f} value={f}>
                    {f}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium">Period start</label>
              <input
                type="date"
                className="border rounded px-3 py-1.5 text-sm bg-background"
                value={genForm.period_start}
                onChange={(e) => setGenForm((p) => ({ ...p, period_start: e.target.value }))}
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium">Period end</label>
              <input
                type="date"
                className="border rounded px-3 py-1.5 text-sm bg-background"
                value={genForm.period_end}
                onChange={(e) => setGenForm((p) => ({ ...p, period_end: e.target.value }))}
              />
            </div>
          </div>

          <div>
            <Button
              size="sm"
              onClick={() => void handleGenerate()}
              disabled={generating || !selectedTenant}
            >
              {generating ? 'Creating...' : 'Generate report'}
            </Button>
          </div>
        </div>
      </Panel>
    </div>
  );
}
