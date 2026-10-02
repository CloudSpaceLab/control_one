import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { EmptyState, KpiTile, Panel, StatusTag } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { mapSettledBounded } from '@/lib/mapSettledBounded';
import type { ControlRoomOverview, IPBehaviorOverview, Tenant } from '@/lib/api';

export type AllTenantNetworkMode = 'ip-behavior' | 'approvals' | 'blocks' | 'firewall';

interface TenantNetworkSummary {
  tenant: Tenant;
  primary: number;
  secondary: number;
  tertiary: number;
  error?: string;
}

interface AllTenantNetworkSummaryProps {
  mode: AllTenantNetworkMode;
  since?: string;
}

export function AllTenantNetworkSummary({ mode, since }: AllTenantNetworkSummaryProps) {
  const api = useApiClient();
  const { tenants, setCurrentTenantId } = useTenant();
  const [rows, setRows] = useState<TenantNetworkSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [errorCount, setErrorCount] = useState(0);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setErrorCount(0);

    void mapSettledBounded(
      tenants,
      async (tenant) => summarizeTenant(mode, tenant, api, since),
      6,
    ).then((results) => {
      if (cancelled) return;
      let failures = 0;
      const next = results.flatMap((result, index) => {
        if (result.status === 'fulfilled') return [result.value];
        failures += 1;
        const tenant = tenants[index];
        if (!tenant) return [];
        return [{
          tenant,
          primary: 0,
          secondary: 0,
          tertiary: 0,
          error: errorMessage(result.reason),
        }];
      });
      setRows(next);
      setErrorCount(failures);
      setLoading(false);
    });

    return () => {
      cancelled = true;
    };
  }, [api, mode, reloadToken, since, tenants]);

  const totals = useMemo(() => rows.reduce(
    (acc, row) => {
      if (!row.error) {
        acc.primary += row.primary;
        acc.secondary += row.secondary;
        acc.tertiary += row.tertiary;
      }
      return acc;
    },
    { primary: 0, secondary: 0, tertiary: 0 },
  ), [rows]);

  if (tenants.length === 0 && !loading) {
    return <EmptyState title="No tenants available" description="No tenant access is available for this account." />;
  }

  const labels = summaryLabels(mode);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label={labels.primary} value={formatNumber(totals.primary)} loading={loading} />
        <KpiTile label={labels.secondary} value={formatNumber(totals.secondary)} loading={loading} />
        <KpiTile label={labels.tertiary} value={formatNumber(totals.tertiary)} loading={loading} />
      </div>

      <Panel
        eyebrow="ALL TENANTS"
        title={`${rows.length - errorCount} of ${tenants.length} tenant views loaded`}
        toneAccent={errorCount > 0 ? 'warning' : 'brand'}
        actions={
          <Button type="button" variant="outline" size="sm" onClick={() => setReloadToken((value) => value + 1)} loading={loading}>
            <RefreshCw className={loading ? 'animate-spin' : ''} />
            {loading ? 'Refreshing…' : 'Refresh'}
          </Button>
        }
      >
        {errorCount > 0 ? (
          <p className="text-sm text-state-warning">
            {errorCount} tenant {errorCount === 1 ? 'view is' : 'views are'} unavailable. Totals exclude unavailable tenants.
          </p>
        ) : null}

        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((row) => (
            <div key={row.tenant.id} className="rounded-lg border border-border-subtle bg-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-foreground">{row.tenant.name}</p>
                  {row.error ? (
                    <p className="mt-1 text-xs text-state-warning">{row.error}</p>
                  ) : (
                    <p className="mt-1 text-xs text-text-muted">{labels.description}</p>
                  )}
                </div>
                <StatusTag tone={row.error ? 'warning' : row.primary > 0 ? 'info' : 'healthy'}>
                  {row.error ? 'Unavailable' : formatNumber(row.primary)}
                </StatusTag>
              </div>

              {!row.error ? (
                <div className="mt-3 grid grid-cols-3 gap-2 text-xs">
                  <Fact label={labels.primary} value={row.primary} />
                  <Fact label={labels.secondary} value={row.secondary} />
                  <Fact label={labels.tertiary} value={row.tertiary} />
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
    </div>
  );
}

async function summarizeTenant(
  mode: AllTenantNetworkMode,
  tenant: Tenant,
  api: ReturnType<typeof useApiClient>,
  since?: string,
): Promise<TenantNetworkSummary> {
  switch (mode) {
    case 'ip-behavior': {
      const overview = await api.getIPBehaviorOverview({ tenantId: tenant.id, since });
      return ipBehaviorSummary(tenant, overview);
    }
    case 'approvals': {
      const response = await api.listBlockProposals({ tenantId: tenant.id, status: 'proposed', limit: 1, offset: 0 });
      return {
        tenant,
        primary: response.pagination.total,
        secondary: response.pagination.total > 0 ? 1 : 0,
        tertiary: 0,
      };
    }
    case 'blocks': {
      const response = await api.listBlockProposals({ tenantId: tenant.id, status: 'active', limit: 1, offset: 0 });
      return {
        tenant,
        primary: response.pagination.total,
        secondary: response.pagination.total > 0 ? 1 : 0,
        tertiary: 0,
      };
    }
    case 'firewall': {
      const overview = await api.getControlRoomOverview(tenant.id, '24h');
      return firewallSummary(tenant, overview);
    }
  }
}

function ipBehaviorSummary(tenant: Tenant, overview: IPBehaviorOverview): TenantNetworkSummary {
  const status = overview.status_counts ?? {};
  const authFailures = (status['401'] ?? 0) + (status['403'] ?? 0);
  const serverErrors = (status['500'] ?? 0) + (status['502'] ?? 0) + (status['503'] ?? 0) + (status['5xx'] ?? 0);
  return {
    tenant,
    primary: overview.request_count ?? 0,
    secondary: authFailures,
    tertiary: serverErrors,
  };
}

function firewallSummary(tenant: Tenant, overview: ControlRoomOverview): TenantNetworkSummary {
  return {
    tenant,
    primary: overview.firewall.enabled,
    secondary: overview.firewall.default_deny,
    tertiary: overview.firewall.unknown + overview.firewall.disabled + overview.firewall.stale,
  };
}

function summaryLabels(mode: AllTenantNetworkMode) {
  switch (mode) {
    case 'ip-behavior':
      return {
        primary: 'Requests',
        secondary: '401/403',
        tertiary: '5xx',
        description: 'IP behavior summary for the selected window.',
      };
    case 'approvals':
      return {
        primary: 'Waiting',
        secondary: 'Tenants waiting',
        tertiary: 'Unavailable',
        description: 'Block responses awaiting a tenant-scoped decision.',
      };
    case 'blocks':
      return {
        primary: 'Active blocks',
        secondary: 'Tenants active',
        tertiary: 'Unavailable',
        description: 'Active block proposals for this tenant.',
      };
    case 'firewall':
      return {
        primary: 'Firewall active',
        secondary: 'Default deny',
        tertiary: 'Unknown/off/stale',
        description: 'Latest host firewall posture.',
      };
  }
}

function Fact({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-md bg-surface-2 px-2 py-2">
      <p className="truncate text-text-muted">{label}</p>
      <p className="mt-1 font-mono text-sm font-semibold tabular-nums text-foreground">{formatNumber(value)}</p>
    </div>
  );
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

function errorMessage(error: unknown): string {
  return error instanceof Error && error.message ? error.message : 'Data unavailable';
}
