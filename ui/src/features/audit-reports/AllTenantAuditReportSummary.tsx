import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, FileText, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Alert, EmptyState, KpiTile, Panel, StatusTag } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { mapSettledBounded } from '@/lib/mapSettledBounded';
import { useTenant } from '@/providers/TenantProvider';
import type { AuditReport, Tenant } from '@/lib/api';

interface TenantReportSummary {
  tenant: Tenant;
  total: number;
  latest: AuditReport | null;
  error?: string;
}

export function AllTenantAuditReportSummary() {
  const api = useApiClient();
  const { tenants, setCurrentTenantId } = useTenant();
  const [rows, setRows] = useState<TenantReportSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    void mapSettledBounded(
      tenants,
      async (tenant) => {
        const response = await api.listAuditReports({
          tenantId: tenant.id,
          limit: 1,
          offset: 0,
        });
        return {
          tenant,
          total: response.pagination.total,
          latest: response.data[0] ?? null,
        } satisfies TenantReportSummary;
      },
      6,
    ).then((results) => {
      if (cancelled) return;
      setRows(results.flatMap((result, index) => {
        if (result.status === 'fulfilled') return [result.value];
        const tenant = tenants[index];
        if (!tenant) return [];
        return [{
          tenant,
          total: 0,
          latest: null,
          error: result.reason instanceof Error && result.reason.message
            ? result.reason.message
            : 'Report history unavailable.',
        }];
      }));
      setLoading(false);
    });

    return () => {
      cancelled = true;
    };
  }, [api, reloadToken, tenants]);

  const totals = useMemo(() => rows.reduce(
    (acc, row) => {
      if (row.error) {
        acc.unavailable += 1;
      } else {
        acc.reports += row.total;
        if (row.total > 0) acc.tenantsWithReports += 1;
      }
      return acc;
    },
    { reports: 0, tenantsWithReports: 0, unavailable: 0 },
  ), [rows]);

  if (tenants.length === 0 && !loading) {
    return <EmptyState title="No tenants available" description="No tenant access is available for this account." />;
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold text-foreground">Audit reports</h2>
          <p className="mt-1 text-sm text-text-secondary">All tenants · generated compliance reports.</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => setReloadToken((value) => value + 1)} loading={loading}>
          <RefreshCw className={loading ? 'animate-spin' : ''} />
          {loading ? 'Refreshing…' : 'Refresh'}
        </Button>
      </div>

      {totals.unavailable > 0 ? (
        <Alert variant="warning" title="Report history partially unavailable">
          {totals.unavailable} tenant {totals.unavailable === 1 ? 'view is' : 'views are'} unavailable. Totals exclude unavailable tenants.
        </Alert>
      ) : null}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="Reports" value={loading ? '—' : totals.reports} icon={<FileText />} loading={loading} />
        <KpiTile label="Tenants with reports" value={loading ? '—' : totals.tenantsWithReports} loading={loading} />
        <KpiTile label="Unavailable" value={loading ? '—' : totals.unavailable} tone={totals.unavailable > 0 ? 'warning' : 'healthy'} loading={loading} />
      </div>

      <Panel eyebrow="TENANTS" title="Report history by tenant" toneAccent="brand">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((row) => (
            <div key={row.tenant.id} className="rounded-lg border border-border-subtle bg-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-foreground">{row.tenant.name}</p>
                  {row.error ? (
                    <p className="mt-1 text-xs text-state-warning">{row.error}</p>
                  ) : row.latest ? (
                    <p className="mt-1 text-xs text-text-muted">
                      Latest: {row.latest.framework} · {formatDate(row.latest.period_end)}
                    </p>
                  ) : (
                    <p className="mt-1 text-xs text-text-muted">No generated reports</p>
                  )}
                </div>
                <StatusTag tone={row.error ? 'warning' : row.latest?.status === 'failed' ? 'critical' : row.total > 0 ? 'info' : 'unknown'}>
                  {row.error ? 'Unavailable' : row.total}
                </StatusTag>
              </div>
              <Button type="button" variant="ghost" size="sm" className="mt-3" onClick={() => setCurrentTenantId(row.tenant.id)}>
                Open tenant
                <ArrowRight />
              </Button>
            </div>
          ))}
        </div>
      </Panel>

      <Panel eyebrow="ACTIONS" title="Tenant selection required" toneAccent="brand">
        <p className="text-sm text-text-secondary">
          Report generation and downloads remain tenant-specific.
        </p>
      </Panel>
    </div>
  );
}

function formatDate(value?: string | null): string {
  if (!value) return 'N/A';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleDateString();
}
