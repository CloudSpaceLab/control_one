import { useCallback, useEffect, useState } from 'react';
import { Download, RefreshCw, FileText } from 'lucide-react';
import { Button } from '../components/ui/button';
import {
  Alert,
  DataTable,
  EmptyState,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useTenant } from '../providers/TenantProvider';
import { saveBlob } from '../lib/download';
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
  if (!v) return 'N/A';
  const d = new Date(v);
  return isNaN(d.getTime()) ? v : d.toLocaleDateString();
}

function fallbackReportFilename(report: AuditReport): string {
  const framework = (report.framework || 'report').toLowerCase().replace(/[^a-z0-9]+/g, '-');
  const periodEnd = (report.period_end || new Date().toISOString()).slice(0, 10);
  return `compliance-report-${framework}-${periodEnd}.txt`;
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

function isReportReady(report: AuditReport): boolean {
  return report.status.toLowerCase() === 'ready';
}

export function AuditReports(): JSX.Element {
  const client = useApiClient();
  const { data: tenantList } = useTenants();
  const { currentTenantId } = useTenant();

  const [selectedTenant, setSelectedTenant] = useState<string>('');
  const [reports, setReports] = useState<AuditReport[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);
  const [genError, setGenError] = useState<string | null>(null);
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const [downloadingId, setDownloadingId] = useState<string | null>(null);

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
      setLoadError(null);
    } catch (err: unknown) {
      setReports([]);
      setLoadError(errorMessage(err, 'Failed to load report history'));
    } finally {
      setLoading(false);
    }
  }, [client, selectedTenant]);

  useEffect(() => {
    void load();
  }, [load]);

  // Auto-select the active tenant when the shared tenant scope is available.
  useEffect(() => {
    if (selectedTenant || tenantList.length === 0) return;
    const activeTenant = currentTenantId
      ? tenantList.find((tenant) => tenant.id === currentTenantId)
      : undefined;
    setSelectedTenant((activeTenant ?? tenantList[0]).id);
  }, [currentTenantId, tenantList, selectedTenant]);

  useEffect(() => {
    if (!selectedTenant || tenantList.length === 0) return;
    if (!tenantList.some((tenant) => tenant.id === selectedTenant)) {
      setSelectedTenant('');
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
    if (genForm.period_start > genForm.period_end) {
      setGenError('Period start must be on or before period end.');
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
      setGenError(errorMessage(err, 'Failed to create report'));
    } finally {
      setGenerating(false);
    }
  };

  const handleDownload = async (report: AuditReport) => {
    if (!selectedTenant) return;
    setDownloadingId(report.id);
    setDownloadError(null);
    try {
      const file = await client.downloadAuditReport(report.id, selectedTenant);
      saveBlob(file.blob, file.filename || fallbackReportFilename(report));
    } catch (err: unknown) {
      setDownloadError(errorMessage(err, 'Failed to download report'));
    } finally {
      setDownloadingId(null);
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
      cell: ({ row }) => {
        const ready = isReportReady(row.original);
        const label = ready
          ? `Download ${row.original.framework} report ending ${formatDate(row.original.period_end)}`
          : `${row.original.framework} report is not ready`;
        return (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => void handleDownload(row.original)}
            disabled={!ready || downloadingId === row.original.id}
            title={label}
            aria-label={label}
          >
            <Download className="w-3.5 h-3.5" />
          </Button>
        );
      },
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
          id="audit-report-tenant"
          aria-label="Report tenant"
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
        <Button variant="outline" size="sm" onClick={() => void load()} disabled={!selectedTenant || loading}>
          <RefreshCw className="w-3.5 h-3.5 mr-1.5" />
          Refresh
        </Button>
      </div>
      {loadError && (
        <Alert
          variant="critical"
          title="Report history unavailable"
          actions={
            <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
              Retry
            </Button>
          }
        >
          {loadError}
        </Alert>
      )}
      {downloadError && (
        <Alert variant="critical" title="Report download failed">
          {downloadError}
        </Alert>
      )}

      {loading ? (
        <div className="text-muted-foreground text-sm py-4">Loading report history.</div>
      ) : loadError ? (
        <EmptyState
          title="Report history unavailable"
          description="Compliance report history could not be loaded for the selected tenant."
        />
      ) : reports.length === 0 ? (
        <EmptyState
          title="No generated reports"
          description="No compliance report files exist for the selected tenant."
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
              <label htmlFor="audit-report-framework" className="text-xs font-medium">
                Framework
              </label>
              <select
                id="audit-report-framework"
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
              <label htmlFor="audit-report-period-start" className="text-xs font-medium">
                Period start
              </label>
              <input
                id="audit-report-period-start"
                type="date"
                className="border rounded px-3 py-1.5 text-sm bg-background"
                value={genForm.period_start}
                onChange={(e) => setGenForm((p) => ({ ...p, period_start: e.target.value }))}
              />
            </div>
            <div className="flex flex-col gap-1">
              <label htmlFor="audit-report-period-end" className="text-xs font-medium">
                Period end
              </label>
              <input
                id="audit-report-period-end"
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
