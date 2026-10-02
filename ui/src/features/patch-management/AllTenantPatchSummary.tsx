import { useEffect, useState } from 'react';
import { ArrowRight, RefreshCw, ShieldAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Alert, EmptyState, KpiTile, Panel } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import type { PatchDeploymentSummary } from '@/lib/api';

export function AllTenantPatchSummary() {
  const api = useApiClient();
  const { tenants, setCurrentTenantId } = useTenant();
  const [summary, setSummary] = useState<PatchDeploymentSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api.getPatchSummary()
      .then((value) => {
        if (!cancelled) setSummary(value);
      })
      .catch((err) => {
        if (cancelled) return;
        setSummary(null);
        setError(err instanceof Error && err.message ? err.message : 'Patch summary unavailable.');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [api, reloadToken]);

  if (tenants.length === 0 && !loading) {
    return <EmptyState title="No tenants available" description="No tenant access is available for this account." />;
  }

  const inFlight = (summary?.pending ?? 0) + (summary?.in_progress ?? 0);
  const failed = (summary?.failed ?? 0) + (summary?.partial ?? 0);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold text-foreground">Patch management</h2>
          <p className="mt-1 text-sm text-text-secondary">All tenants · deployment and approval status.</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => setReloadToken((value) => value + 1)} loading={loading}>
          <RefreshCw className={loading ? 'animate-spin' : ''} />
          {loading ? 'Refreshing…' : 'Refresh'}
        </Button>
      </div>

      {error ? (
        <Alert variant="critical" title="Patch summary unavailable">
          {error}
        </Alert>
      ) : null}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <KpiTile label="Deployments" value={summary?.total ?? '—'} loading={loading} />
        <KpiTile label="In flight" value={summary ? inFlight : '—'} tone={inFlight > 0 ? 'warning' : 'healthy'} loading={loading} />
        <KpiTile label="Completed" value={summary?.completed ?? '—'} tone="healthy" loading={loading} />
        <KpiTile label="Failed / partial" value={summary ? failed : '—'} tone={failed > 0 ? 'critical' : 'healthy'} loading={loading} />
        <KpiTile label="Needs approval" value={summary?.pending_approvals ?? '—'} tone={(summary?.pending_approvals ?? 0) > 0 ? 'warning' : 'healthy'} loading={loading} />
      </div>

      <Panel eyebrow="TENANTS" title="Open a tenant to make changes" toneAccent="brand">
        <p className="mb-3 text-sm text-text-secondary">
          Deployments, proxy settings, maintenance windows, and approval decisions are tenant-specific.
        </p>
        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
          {tenants.map((tenant) => (
            <Button
              key={tenant.id}
              type="button"
              variant="outline"
              className="justify-between"
              onClick={() => setCurrentTenantId(tenant.id)}
            >
              <span className="truncate">{tenant.name}</span>
              <ArrowRight />
            </Button>
          ))}
        </div>
      </Panel>

      {(summary?.pending_approvals ?? 0) > 0 ? (
        <Panel eyebrow="ATTENTION" title="Approval required" toneAccent="warning">
          <div className="flex items-center gap-2 text-sm text-text-secondary">
            <ShieldAlert className="h-4 w-4 text-state-warning" />
            Select the affected tenant to review and decide pending patch approvals.
          </div>
        </Panel>
      ) : null}
    </div>
  );
}
