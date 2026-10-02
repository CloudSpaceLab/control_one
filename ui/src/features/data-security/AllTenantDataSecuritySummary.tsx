import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, Database, RefreshCw, ShieldAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Alert, EmptyState, KpiTile, Panel, StatusTag } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { mapSettledBounded } from '@/lib/mapSettledBounded';
import { useTenant } from '@/providers/TenantProvider';

interface TenantDataSecuritySummary {
  id: string;
  name: string;
  findings: number;
  openFindings: number;
  columns: number;
  rules: number;
  error?: string;
}

export function AllTenantDataSecuritySummary() {
  const api = useApiClient();
  const { tenants, setCurrentTenantId } = useTenant();
  const [rows, setRows] = useState<TenantDataSecuritySummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    void mapSettledBounded(
      tenants,
      async (tenant) => {
        const [allFindings, openFindings, columns, rules] = await Promise.all([
          api.listPIIFindings({ tenantId: tenant.id, limit: 1, offset: 0 }),
          api.listPIIFindings({ tenantId: tenant.id, resolved: false, limit: 1, offset: 0 }),
          api.listColumnClassifications({ tenantId: tenant.id, limit: 1, offset: 0 }),
          api.listDLPRules(tenant.id),
        ]);
        return {
          id: tenant.id,
          name: tenant.name,
          findings: allFindings.pagination.total,
          openFindings: openFindings.pagination.total,
          columns: columns.pagination.total,
          rules: rules.data?.length ?? 0,
        } satisfies TenantDataSecuritySummary;
      },
      5,
    ).then((results) => {
      if (cancelled) return;
      setRows(results.flatMap((result, index) => {
        if (result.status === 'fulfilled') return [result.value];
        const tenant = tenants[index];
        if (!tenant) return [];
        return [{
          id: tenant.id,
          name: tenant.name,
          findings: 0,
          openFindings: 0,
          columns: 0,
          rules: 0,
          error: result.reason instanceof Error && result.reason.message
            ? result.reason.message
            : 'Data security unavailable.',
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
        acc.findings += row.findings;
        acc.openFindings += row.openFindings;
        acc.columns += row.columns;
        acc.rules += row.rules;
      }
      return acc;
    },
    { findings: 0, openFindings: 0, columns: 0, rules: 0, unavailable: 0 },
  ), [rows]);

  if (tenants.length === 0 && !loading) {
    return <EmptyState title="No tenants available" description="No tenant access is available for this account." />;
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-text-muted">DETECT & RESPOND / DATA SECURITY</p>
          <h1 className="mt-1 text-xl font-semibold text-foreground">Data security</h1>
          <p className="mt-1 text-sm text-text-secondary">All tenants · PII findings, classified columns, and DLP rules.</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => setReloadToken((value) => value + 1)} loading={loading}>
          <RefreshCw className={loading ? 'animate-spin' : ''} />
          {loading ? 'Refreshing…' : 'Refresh'}
        </Button>
      </div>

      {totals.unavailable > 0 ? (
        <Alert variant="warning" title="Data security partially unavailable">
          {totals.unavailable} tenant {totals.unavailable === 1 ? 'view is' : 'views are'} unavailable. Estate totals exclude unavailable tenants.
        </Alert>
      ) : null}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <KpiTile label="Open findings" value={loading ? '—' : totals.openFindings} tone={totals.openFindings > 0 ? 'warning' : 'healthy'} icon={<ShieldAlert />} loading={loading} />
        <KpiTile label="Findings" value={loading ? '—' : totals.findings} loading={loading} />
        <KpiTile label="Classified columns" value={loading ? '—' : totals.columns} icon={<Database />} loading={loading} />
        <KpiTile label="DLP rules" value={loading ? '—' : totals.rules} loading={loading} />
      </div>

      <Panel eyebrow="TENANTS" title="Data security by tenant" toneAccent={totals.openFindings > 0 ? 'warning' : 'brand'}>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((row) => (
            <div key={row.id} className="rounded-lg border border-border-subtle bg-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-foreground">{row.name}</p>
                  <p className="mt-1 text-xs text-text-muted">
                    {row.error ?? `${row.columns} columns · ${row.rules} rules`}
                  </p>
                </div>
                <StatusTag tone={row.error ? 'warning' : row.openFindings > 0 ? 'warning' : 'healthy'}>
                  {row.error ? 'Unavailable' : `${row.openFindings} open`}
                </StatusTag>
              </div>
              <Button type="button" variant="ghost" size="sm" className="mt-3" onClick={() => setCurrentTenantId(row.id)}>
                Open tenant
                <ArrowRight />
              </Button>
            </div>
          ))}
        </div>
      </Panel>

      <Panel eyebrow="ACTIONS" title="Tenant selection required" toneAccent="brand">
        <p className="text-sm text-text-secondary">
          Resolving findings and managing DLP rules remain tenant-specific.
        </p>
      </Panel>
    </div>
  );
}
