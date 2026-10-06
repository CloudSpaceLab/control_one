import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, DatabaseZap, RefreshCw, ShieldAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Alert, EmptyState, KpiTile, Panel, StatusTag } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { mapSettledBounded } from '@/lib/mapSettledBounded';
import type { Tenant } from '@/lib/api';

interface TenantSIEMSummary {
  tenant: Tenant;
  proposals: number;
  approvalRequired: number;
  sources: number;
  collecting: number;
  degraded: number;
  eventsReceived: number;
  error?: string;
}

export function AllTenantSIEMCoverage() {
  const api = useApiClient();
  const { tenants, setCurrentTenantId } = useTenant();
  const [rows, setRows] = useState<TenantSIEMSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    void mapSettledBounded(
      tenants,
      async (tenant) => {
        const [proposalResp, healthResp] = await Promise.all([
          api.listContentPackSourceProposals({
            tenantId: tenant.id,
            limit: 1,
            offset: 0,
          }),
          api.getContentPackSourceHealth(tenant.id, {
            limit: 1,
            offset: 0,
          }),
        ]);
        const byState = healthResp.totals?.by_state ?? {};
        return {
          tenant,
          proposals: proposalResp.summary?.total ?? proposalResp.pagination.total,
          approvalRequired: proposalResp.summary?.by_status?.approval_required ?? 0,
          sources: healthResp.totals?.sources ?? 0,
          collecting: (byState.collecting ?? 0) + (byState.deployed ?? 0),
          degraded:
            (byState.backpressured ?? 0)
            + (byState.parser_failed ?? 0)
            + (byState.silent ?? 0),
          eventsReceived: healthResp.totals?.metrics?.events_received ?? 0,
        } satisfies TenantSIEMSummary;
      },
      4,
    ).then((results) => {
      if (cancelled) return;
      const next = results.flatMap((result, index) => {
        if (result.status === 'fulfilled') return [result.value];
        const tenant = tenants[index];
        if (!tenant) return [];
        return [{
          tenant,
          proposals: 0,
          approvalRequired: 0,
          sources: 0,
          collecting: 0,
          degraded: 0,
          eventsReceived: 0,
          error: result.reason instanceof Error && result.reason.message
            ? result.reason.message
            : 'SIEM coverage unavailable.',
        }];
      });
      setRows(next);
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
        return acc;
      }
      acc.proposals += row.proposals;
      acc.approvalRequired += row.approvalRequired;
      acc.sources += row.sources;
      acc.collecting += row.collecting;
      acc.degraded += row.degraded;
      acc.eventsReceived += row.eventsReceived;
      return acc;
    },
    {
      proposals: 0,
      approvalRequired: 0,
      sources: 0,
      collecting: 0,
      degraded: 0,
      eventsReceived: 0,
      unavailable: 0,
    },
  ), [rows]);

  if (tenants.length === 0 && !loading) {
    return <EmptyState title="No tenants available" description="No tenant access is available for this account." />;
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold text-foreground">SIEM coverage</h2>
          <p className="mt-1 text-sm text-text-secondary">All tenants · source coverage and connector decisions.</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => setReloadToken((value) => value + 1)} loading={loading}>
          <RefreshCw className={loading ? 'animate-spin' : ''} />
          {loading ? 'Refreshing…' : 'Refresh'}
        </Button>
      </div>

      {totals.unavailable > 0 ? (
        <Alert variant="warning" title="SIEM coverage partially unavailable">
          {totals.unavailable} tenant {totals.unavailable === 1 ? 'view is' : 'views are'} unavailable. Estate totals exclude unavailable tenants.
        </Alert>
      ) : null}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <KpiTile label="Sources" value={loading ? '—' : totals.sources} loading={loading} icon={<DatabaseZap />} />
        <KpiTile label="Collecting" value={loading ? '—' : totals.collecting} tone={totals.degraded > 0 ? 'warning' : 'healthy'} loading={loading} />
        <KpiTile label="Degraded" value={loading ? '—' : totals.degraded} tone={totals.degraded > 0 ? 'critical' : 'healthy'} loading={loading} />
        <KpiTile label="Proposals" value={loading ? '—' : totals.proposals} loading={loading} />
        <KpiTile label="Needs approval" value={loading ? '—' : totals.approvalRequired} tone={totals.approvalRequired > 0 ? 'warning' : 'healthy'} loading={loading} icon={<ShieldAlert />} />
      </div>

      <Panel eyebrow="TENANTS" title="Coverage by tenant" toneAccent={totals.degraded > 0 ? 'warning' : 'brand'}>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((row) => (
            <div key={row.tenant.id} className="rounded-lg border border-border-subtle bg-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-foreground">{row.tenant.name}</p>
                  <p className="mt-1 text-xs text-text-muted">
                    {row.error
                      ? row.error
                      : `${row.sources} sources · ${row.eventsReceived.toLocaleString()} events received`}
                  </p>
                </div>
                <StatusTag tone={row.error ? 'warning' : row.degraded > 0 ? 'critical' : row.collecting > 0 ? 'healthy' : 'unknown'}>
                  {row.error ? 'Unavailable' : row.degraded > 0 ? `${row.degraded} degraded` : `${row.collecting} collecting`}
                </StatusTag>
              </div>
              {!row.error ? (
                <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-text-secondary">
                  <span>{row.proposals} proposals</span>
                  <span>{row.approvalRequired} need approval</span>
                </div>
              ) : null}
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="mt-3"
                onClick={() => setCurrentTenantId(row.tenant.id)}
              >
                Open tenant
                <ArrowRight />
              </Button>
            </div>
          ))}
        </div>
      </Panel>

      <Panel eyebrow="ACTIONS" title="Tenant selection required" toneAccent="brand">
        <p className="text-sm text-text-secondary">
          Connector policy, proposal decisions, collector configuration, and deployment actions remain tenant-specific.
        </p>
      </Panel>
    </div>
  );
}
