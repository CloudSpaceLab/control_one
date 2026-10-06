import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, RefreshCw, Server } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Alert, EmptyState, KpiTile, Panel, StatusTag } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { mapSettledBounded } from '@/lib/mapSettledBounded';
import { useTenant } from '@/providers/TenantProvider';

interface TenantWebserverSummary {
  id: string;
  name: string;
  detected: number;
  error?: string;
}

export function AllTenantWebserverSummary() {
  const api = useApiClient();
  const { tenants, setCurrentTenantId } = useTenant();
  const [rows, setRows] = useState<TenantWebserverSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    void mapSettledBounded(
      tenants,
      async (tenant) => {
        const response = await api.listWebserverInstances({
          tenantId: tenant.id,
          limit: 1,
          offset: 0,
        });
        return {
          id: tenant.id,
          name: tenant.name,
          detected: response.pagination.total,
        } satisfies TenantWebserverSummary;
      },
      6,
    ).then((results) => {
      if (cancelled) return;
      setRows(results.flatMap((result, index) => {
        if (result.status === 'fulfilled') return [result.value];
        const tenant = tenants[index];
        if (!tenant) return [];
        return [{
          id: tenant.id,
          name: tenant.name,
          detected: 0,
          error: result.reason instanceof Error && result.reason.message
            ? result.reason.message
            : 'Webserver inventory unavailable.',
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
        acc.detected += row.detected;
        if (row.detected > 0) acc.tenantsWithInventory += 1;
      }
      return acc;
    },
    { detected: 0, tenantsWithInventory: 0, unavailable: 0 },
  ), [rows]);

  if (tenants.length === 0 && !loading) {
    return <EmptyState title="No tenants available" description="No tenant access is available for this account." />;
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-text-muted">WEBSERVERS</p>
          <h1 className="mt-1 text-xl font-semibold text-foreground">Webserver controls</h1>
          <p className="mt-1 text-sm text-text-secondary">All tenants · detected webserver inventory.</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => setReloadToken((value) => value + 1)} loading={loading}>
          <RefreshCw className={loading ? 'animate-spin' : ''} />
          {loading ? 'Refreshing…' : 'Refresh'}
        </Button>
      </div>

      {totals.unavailable > 0 ? (
        <Alert variant="warning" title="Webserver inventory partially unavailable">
          {totals.unavailable} tenant {totals.unavailable === 1 ? 'view is' : 'views are'} unavailable. Estate totals exclude unavailable tenants.
        </Alert>
      ) : null}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="Detected" value={loading ? '—' : totals.detected} icon={<Server />} loading={loading} />
        <KpiTile label="Tenants with inventory" value={loading ? '—' : totals.tenantsWithInventory} loading={loading} />
        <KpiTile label="Unavailable" value={loading ? '—' : totals.unavailable} tone={totals.unavailable > 0 ? 'warning' : 'healthy'} loading={loading} />
      </div>

      <Panel eyebrow="TENANTS" title="Open a tenant to manage webservers" toneAccent="brand">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((row) => (
            <div key={row.id} className="rounded-lg border border-border-subtle bg-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-foreground">{row.name}</p>
                  <p className="mt-1 text-xs text-text-muted">
                    {row.error ?? `${row.detected} detected webserver${row.detected === 1 ? '' : 's'}`}
                  </p>
                </div>
                <StatusTag tone={row.error ? 'warning' : row.detected > 0 ? 'info' : 'unknown'}>
                  {row.error ? 'Unavailable' : row.detected}
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
          Planning, capture, enforcement, rollback, and action history remain tenant-specific.
        </p>
      </Panel>
    </div>
  );
}
