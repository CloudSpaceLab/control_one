import { useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, RefreshCw, ShieldAlert } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Skeleton } from '../components/ui/skeleton';
import { SectionHeader, EmptyState, StatusTag, KpiTile } from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { toast } from 'sonner';
import type { PatchDeployment, NodePatchState } from '../lib/api';

// PatchManagement is the operator console for fleet OS-package patching.
// PR 4 ships the direct-deploy mode (apt/dnf/winget on the node itself).
// Proxy mode + airgapped mode + Squid management land in a follow-up.
export function PatchManagement(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [deployments, setDeployments] = useState<PatchDeployment[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<PatchDeployment | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await client.listPatchDeployments({ tenantId: currentTenantId, limit: 50 });
      setDeployments(resp.deployments ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const totals = useMemo(() => {
    return deployments.reduce(
      (acc, d) => {
        acc.total += 1;
        if (d.Status === 'in_progress' || d.Status === 'pending') acc.inFlight += 1;
        if (d.Status === 'completed') acc.completed += 1;
        if (d.Status === 'failed' || d.Status === 'partial') acc.failed += 1;
        return acc;
      },
      { total: 0, inFlight: 0, completed: 0, failed: 0 },
    );
  }, [deployments]);

  const deployFleet = async () => {
    if (!currentTenantId) {
      toast.error('Select a tenant first');
      return;
    }
    if (!confirm('Deploy patches to every enrolled node in this tenant?')) {
      return;
    }
    setBusy(true);
    try {
      const resp = await client.createPatchDeployment({ tenant_id: currentTenantId, mode: 'direct' });
      toast.success(`Patch deployment dispatched to ${resp.node_count} node${resp.node_count === 1 ? '' : 's'}`);
      refresh();
    } catch (err) {
      toast.error(`Deploy failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(false);
    }
  };

  if (!currentTenantId) {
    return (
      <div className="space-y-6 p-6">
        <SectionHeader title="Patch management" description="Fleet OS-package upgrades dispatched via the agent." />
        <EmptyState title="Select a tenant" description="Choose a tenant from the header to view patch deployments." />
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6">
      <SectionHeader
        title="Patch management"
        description="Direct-mode OS package upgrades dispatched fleet-wide via the agent. Proxy + airgapped modes ship in a follow-up."
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
              <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
            <Button size="sm" onClick={deployFleet} disabled={busy}>
              {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <ShieldAlert className="mr-2 h-4 w-4" />}
              Deploy fleet-wide
            </Button>
          </div>
        }
      />

      <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
        <KpiTile label="Total deployments" value={String(totals.total)} />
        <KpiTile label="In flight" value={String(totals.inFlight)} tone={totals.inFlight > 0 ? 'warning' : 'unknown'} />
        <KpiTile label="Completed" value={String(totals.completed)} tone="healthy" />
        <KpiTile label="Failed / partial" value={String(totals.failed)} tone={totals.failed > 0 ? 'critical' : 'unknown'} />
      </div>

      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}

      {!loading && deployments.length === 0 ? (
        <EmptyState
          title="No deployments yet"
          description="Click Deploy fleet-wide to run apt-get / dnf / winget upgrade on every enrolled node."
        />
      ) : (
        <div className="rounded border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">Requested</th>
                <th className="px-3 py-2">Mode</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Applied / Total</th>
                <th className="px-3 py-2">Failed</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {deployments.map((d) => (
                <tr key={d.ID} className="border-t border-border hover:bg-hover">
                  <td className="px-3 py-2 text-text-secondary">{new Date(d.RequestedAt).toLocaleString()}</td>
                  <td className="px-3 py-2">{d.Mode}</td>
                  <td className="px-3 py-2">
                    <StatusTag tone={statusTone(d.Status)}>{d.Status}</StatusTag>
                  </td>
                  <td className="px-3 py-2">
                    {(d.nodes_applied ?? 0)}/{d.TargetNodeCount}
                  </td>
                  <td className="px-3 py-2">{d.nodes_failed ?? 0}</td>
                  <td className="px-3 py-2 text-right">
                    <Button variant="ghost" size="sm" onClick={() => setSelected(d)}>
                      Per-node
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {selected && <DeploymentNodeDetail deployment={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}

function statusTone(s: PatchDeployment['Status']): 'healthy' | 'warning' | 'critical' | 'unknown' {
  switch (s) {
    case 'completed':
      return 'healthy';
    case 'pending':
    case 'in_progress':
      return 'warning';
    case 'failed':
    case 'partial':
      return 'critical';
    default:
      return 'unknown';
  }
}

function nodeStatusTone(s: NodePatchState['Status']): 'healthy' | 'warning' | 'critical' {
  switch (s) {
    case 'applied':
      return 'healthy';
    case 'pending':
      return 'warning';
    case 'failed':
    default:
      return 'critical';
  }
}

function DeploymentNodeDetail({
  deployment,
  onClose,
}: {
  deployment: PatchDeployment;
  onClose: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [rows, setRows] = useState<NodePatchState[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancel = false;
    (async () => {
      setLoading(true);
      try {
        const resp = await client.listPatchDeploymentNodes(deployment.ID);
        if (!cancel) setRows(resp.rows ?? []);
      } catch (err) {
        if (!cancel) setError(err instanceof Error ? err.message : 'load failed');
      } finally {
        if (!cancel) setLoading(false);
      }
    })();
    return () => {
      cancel = true;
    };
  }, [deployment.ID, client]);

  return (
    <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-2xl overflow-y-auto border-l border-border bg-elevated p-6 shadow-2xl">
      <div className="mb-4 flex items-start justify-between">
        <div>
          <p className="text-xs uppercase tracking-wider text-text-secondary">Deployment</p>
          <h3 className="font-mono text-lg">{deployment.ID.slice(0, 8)}</h3>
          <p className="text-xs text-text-secondary">
            {deployment.Mode} · {new Date(deployment.RequestedAt).toLocaleString()}
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      {loading && <Skeleton className="h-32 w-full" />}
      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}
      {!loading && rows.length === 0 && <EmptyState title="No nodes" description="No nodes received this deployment." />}
      {rows.length > 0 && (
        <table className="w-full text-sm">
          <thead className="text-left text-xs uppercase tracking-wider text-text-secondary">
            <tr>
              <th className="px-2 py-1">Node</th>
              <th className="px-2 py-1">Status</th>
              <th className="px-2 py-1">Pkgs</th>
              <th className="px-2 py-1">Error / log tail</th>
              <th className="px-2 py-1">Applied</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.ID} className="border-t border-border align-top">
                <td className="px-2 py-1 font-mono text-xs">{r.NodeID.slice(0, 8)}</td>
                <td className="px-2 py-1">
                  <StatusTag tone={nodeStatusTone(r.Status)}>{r.Status}</StatusTag>
                </td>
                <td className="px-2 py-1 text-xs">{r.PackagesUpgraded ?? '—'}</td>
                <td className="px-2 py-1 text-xs text-text-secondary">
                  {r.Error ? <span className="text-destructive">{r.Error}</span> : null}
                  {r.LogTail ? (
                    <pre className="whitespace-pre-wrap text-[10px] leading-tight text-text-secondary">{r.LogTail}</pre>
                  ) : null}
                  {!r.Error && !r.LogTail ? '—' : null}
                </td>
                <td className="px-2 py-1 text-xs text-text-secondary">
                  {r.AppliedAt ? new Date(r.AppliedAt).toLocaleString() : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </aside>
  );
}

export default PatchManagement;
