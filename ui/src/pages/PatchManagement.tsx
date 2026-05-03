import { useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, RefreshCw, ShieldAlert, ShieldCheck, Clock, Server, Plus, Trash2 } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Skeleton } from '../components/ui/skeleton';
import { SectionHeader, EmptyState, StatusTag, KpiTile } from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { toast } from 'sonner';
import type {
  PatchDeployment,
  NodePatchState,
  NodePatchConfig,
  MaintenanceWindow,
  SquidProxy,
} from '../lib/api';

// PatchManagement is the operator console for fleet OS-package patching.
// Wave C extends the page with Squid proxy management, maintenance window
// scheduling, and per-node mode configuration on top of the direct-mode
// MVP shipped in PR #30.
type Tab = 'deployments' | 'proxies' | 'windows';

export function PatchManagement(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [tab, setTab] = useState<Tab>('deployments');
  const [deployments, setDeployments] = useState<PatchDeployment[]>([]);
  const [proxies, setProxies] = useState<SquidProxy[]>([]);
  const [windows, setWindows] = useState<MaintenanceWindow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<PatchDeployment | null>(null);
  const [showProxyForm, setShowProxyForm] = useState(false);
  const [showWindowForm, setShowWindowForm] = useState(false);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const [deps, proxyList, windowList] = await Promise.all([
        client.listPatchDeployments({ tenantId: currentTenantId, limit: 50 }),
        client.listSquidProxies(currentTenantId).catch(() => ({ proxies: [] as SquidProxy[] })),
        client.listMaintenanceWindows(currentTenantId).catch(() => ({ windows: [] as MaintenanceWindow[] })),
      ]);
      setDeployments(deps.deployments ?? []);
      setProxies(proxyList.proxies ?? []);
      setWindows(windowList.windows ?? []);
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
    if (!confirm('Deploy patches to every enrolled node in this tenant? Each node passes through the 4-gate safety pipeline.')) {
      return;
    }
    setBusy(true);
    try {
      const resp = await client.createPatchDeployment({ tenant_id: currentTenantId, mode: 'auto' });
      const blocked = resp.gate_blocked?.length ?? 0;
      const failedCount = resp.failed?.length ?? 0;
      let msg = `Patch deployment dispatched to ${resp.node_count} node${resp.node_count === 1 ? '' : 's'}`;
      if (blocked > 0) msg += `; ${blocked} blocked by safety gate`;
      if (failedCount > 0) msg += `; ${failedCount} failed to dispatch`;
      toast.success(msg);
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
        description="Direct, proxy and airgapped OS-package upgrades fanned out per node. Every deploy passes through the same opt-out / change-window / circuit-breaker / approval gates the compliance remediation engine uses."
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

      <div className="flex items-center gap-1 border-b border-border">
        <TabButton active={tab === 'deployments'} onClick={() => setTab('deployments')} label="Deployments" />
        <TabButton active={tab === 'proxies'} onClick={() => setTab('proxies')} label={`Proxies (${proxies.length})`} />
        <TabButton active={tab === 'windows'} onClick={() => setTab('windows')} label={`Windows (${windows.length})`} />
      </div>

      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}

      {tab === 'deployments' && (
        <DeploymentsPanel
          loading={loading}
          deployments={deployments}
          onSelect={setSelected}
        />
      )}
      {tab === 'proxies' && (
        <ProxiesPanel
          proxies={proxies}
          tenantId={currentTenantId}
          onCreate={() => setShowProxyForm(true)}
          onChanged={refresh}
        />
      )}
      {tab === 'windows' && (
        <WindowsPanel
          windows={windows}
          tenantId={currentTenantId}
          onCreate={() => setShowWindowForm(true)}
          onChanged={refresh}
        />
      )}

      {selected && <DeploymentNodeDetail deployment={selected} onClose={() => setSelected(null)} />}
      {showProxyForm && currentTenantId && (
        <ProxyForm
          tenantId={currentTenantId}
          onClose={() => setShowProxyForm(false)}
          onCreated={() => {
            setShowProxyForm(false);
            refresh();
          }}
        />
      )}
      {showWindowForm && currentTenantId && (
        <WindowForm
          tenantId={currentTenantId}
          onClose={() => setShowWindowForm(false)}
          onCreated={() => {
            setShowWindowForm(false);
            refresh();
          }}
        />
      )}
    </div>
  );
}

function TabButton({ active, onClick, label }: { active: boolean; onClick: () => void; label: string }): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`px-3 py-2 text-sm font-medium border-b-2 -mb-px ${
        active ? 'border-primary text-text-primary' : 'border-transparent text-text-secondary hover:text-text-primary'
      }`}
    >
      {label}
    </button>
  );
}

function DeploymentsPanel({
  loading,
  deployments,
  onSelect,
}: {
  loading: boolean;
  deployments: PatchDeployment[];
  onSelect: (d: PatchDeployment) => void;
}): JSX.Element {
  if (loading && deployments.length === 0) return <Skeleton className="h-32 w-full" />;
  if (!loading && deployments.length === 0) {
    return (
      <EmptyState
        title="No deployments yet"
        description="Click Deploy fleet-wide to run apt-get / dnf / winget upgrade on every enrolled node. Each node passes through the 4 safety gates."
      />
    );
  }
  return (
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
                <Button variant="ghost" size="sm" onClick={() => onSelect(d)}>
                  Per-node
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ProxiesPanel({
  proxies,
  tenantId,
  onCreate,
  onChanged,
}: {
  proxies: SquidProxy[];
  tenantId: string;
  onCreate: () => void;
  onChanged: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [busy, setBusy] = useState<string | null>(null);
  void tenantId;

  const remove = async (p: SquidProxy) => {
    if (!confirm(`Remove proxy ${p.Host}:${p.Port}?`)) return;
    setBusy(p.ID);
    try {
      await client.removeSquidProxy(p.ID);
      toast.success('Proxy removal queued');
      onChanged();
    } catch (err) {
      toast.error(`Remove failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <Button size="sm" onClick={onCreate}>
          <Plus className="mr-2 h-4 w-4" /> Install proxy
        </Button>
      </div>
      {proxies.length === 0 ? (
        <EmptyState
          title="No managed proxies"
          description="Install a Squid proxy on a designated bastion to relay package-manager traffic for proxy-mode patch deploys."
        />
      ) : (
        <div className="rounded border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">Host</th>
                <th className="px-3 py-2">Port</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Whitelist</th>
                <th className="px-3 py-2">Last validated</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {proxies.map((p) => (
                <tr key={p.ID} className="border-t border-border align-top">
                  <td className="px-3 py-2 font-mono text-xs">{p.Host}</td>
                  <td className="px-3 py-2">{p.Port}</td>
                  <td className="px-3 py-2">
                    <StatusTag tone={proxyStatusTone(p.Status)}>{p.Status}</StatusTag>
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">
                    {p.Whitelist.length === 0 ? '—' : `${p.Whitelist.length} hosts`}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">
                    {p.LastValidatedAt ? new Date(p.LastValidatedAt).toLocaleString() : '—'}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => remove(p)}
                      disabled={busy === p.ID || p.Status === 'removing' || p.Status === 'removed'}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function WindowsPanel({
  windows,
  tenantId,
  onCreate,
  onChanged,
}: {
  windows: MaintenanceWindow[];
  tenantId: string;
  onCreate: () => void;
  onChanged: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [busy, setBusy] = useState<string | null>(null);
  void tenantId;

  const open = async (w: MaintenanceWindow) => {
    setBusy(w.ID);
    try {
      await client.openMaintenanceWindow(w.ID);
      toast.success('Window open queued — firewall allow rules dispatching');
      onChanged();
    } catch (err) {
      toast.error(`Open failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(null);
    }
  };

  const close = async (w: MaintenanceWindow) => {
    setBusy(w.ID);
    try {
      await client.closeMaintenanceWindow(w.ID);
      toast.success('Window close queued');
      onChanged();
    } catch (err) {
      toast.error(`Close failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(null);
    }
  };

  const forceClose = async (w: MaintenanceWindow) => {
    if (!confirm(`Force-close window "${w.Name}"? This stamps force_closed_at immediately and bypasses normal teardown.`)) return;
    setBusy(w.ID);
    try {
      await client.forceCloseMaintenanceWindow(w.ID);
      toast.success('Window force-closed');
      onChanged();
    } catch (err) {
      toast.error(`Force close failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <Button size="sm" onClick={onCreate}>
          <Plus className="mr-2 h-4 w-4" /> Schedule window
        </Button>
      </div>
      {windows.length === 0 ? (
        <EmptyState
          title="No maintenance windows"
          description="Schedule a window to open allow-repo firewall rules during a defined timespan for airgapped or proxy-mode patch deploys."
        />
      ) : (
        <div className="rounded border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">Name</th>
                <th className="px-3 py-2">Window</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Nodes</th>
                <th className="px-3 py-2">Allow repos</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {windows.map((w) => (
                <tr key={w.ID} className="border-t border-border align-top">
                  <td className="px-3 py-2">
                    <div className="font-medium">{w.Name}</div>
                    <div className="text-[10px] text-text-secondary">{countdownText(w)}</div>
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">
                    <Clock className="inline h-3 w-3 mr-1" />
                    {new Date(w.OpensAt).toLocaleString()}
                    <br />→ {new Date(w.ClosesAt).toLocaleString()}
                  </td>
                  <td className="px-3 py-2">
                    <StatusTag tone={windowStatusTone(w.Status)}>{w.Status}</StatusTag>
                  </td>
                  <td className="px-3 py-2 text-xs">
                    <Server className="inline h-3 w-3 mr-1" />
                    {w.NodeIDs.length}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">
                    {w.AllowRepos.length === 0 ? '—' : `${w.AllowRepos.length} hosts`}
                  </td>
                  <td className="px-3 py-2 text-right whitespace-nowrap">
                    {w.Status === 'scheduled' && (
                      <Button variant="ghost" size="sm" onClick={() => open(w)} disabled={busy === w.ID}>
                        <ShieldCheck className="h-4 w-4 mr-1" /> Open
                      </Button>
                    )}
                    {(w.Status === 'open' || w.Status === 'closing') && (
                      <>
                        <Button variant="ghost" size="sm" onClick={() => close(w)} disabled={busy === w.ID}>
                          Close
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => forceClose(w)} disabled={busy === w.ID}>
                          Force
                        </Button>
                      </>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function ProxyForm({
  tenantId,
  onClose,
  onCreated,
}: {
  tenantId: string;
  onClose: () => void;
  onCreated: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [host, setHost] = useState('');
  const [port, setPort] = useState(3128);
  const [whitelist, setWhitelist] = useState('archive.ubuntu.com\nsecurity.ubuntu.com');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!host.trim()) {
      toast.error('Host required');
      return;
    }
    setBusy(true);
    try {
      const list = whitelist
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean);
      await client.createSquidProxy({ tenant_id: tenantId, host, port, whitelist: list });
      toast.success('Proxy created');
      onCreated();
    } catch (err) {
      toast.error(`Create failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-lg overflow-y-auto border-l border-border bg-elevated p-6 shadow-2xl">
      <div className="mb-4 flex justify-between">
        <h3 className="text-lg font-semibold">Install Squid proxy</h3>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      <div className="space-y-3">
        <label className="block text-sm">
          <span className="text-text-secondary">Host (IP or DNS)</span>
          <input
            type="text"
            value={host}
            onChange={(e) => setHost(e.target.value)}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
            placeholder="10.0.0.5 or proxy.example.com"
          />
        </label>
        <label className="block text-sm">
          <span className="text-text-secondary">Port</span>
          <input
            type="number"
            value={port}
            onChange={(e) => setPort(Number(e.target.value))}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
          />
        </label>
        <label className="block text-sm">
          <span className="text-text-secondary">Whitelist (one host per line)</span>
          <textarea
            value={whitelist}
            onChange={(e) => setWhitelist(e.target.value)}
            rows={8}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm font-mono text-xs"
          />
        </label>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" onClick={submit} disabled={busy}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Create proxy
          </Button>
        </div>
      </div>
    </aside>
  );
}

function WindowForm({
  tenantId,
  onClose,
  onCreated,
}: {
  tenantId: string;
  onClose: () => void;
  onCreated: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [name, setName] = useState('');
  const [opensAt, setOpensAt] = useState('');
  const [closesAt, setClosesAt] = useState('');
  const [allowRepos, setAllowRepos] = useState('archive.ubuntu.com\nsecurity.ubuntu.com');
  const [nodeIDs, setNodeIDs] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!name.trim() || !opensAt || !closesAt) {
      toast.error('Name, opens_at, closes_at all required');
      return;
    }
    setBusy(true);
    try {
      await client.createMaintenanceWindow({
        tenant_id: tenantId,
        name,
        opens_at: new Date(opensAt).toISOString(),
        closes_at: new Date(closesAt).toISOString(),
        allow_repos: allowRepos
          .split(/\r?\n/)
          .map((s) => s.trim())
          .filter(Boolean),
        node_ids: nodeIDs
          .split(/[\s,]+/)
          .map((s) => s.trim())
          .filter(Boolean),
      });
      toast.success('Window scheduled');
      onCreated();
    } catch (err) {
      toast.error(`Create failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-lg overflow-y-auto border-l border-border bg-elevated p-6 shadow-2xl">
      <div className="mb-4 flex justify-between">
        <h3 className="text-lg font-semibold">Schedule maintenance window</h3>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      <div className="space-y-3">
        <label className="block text-sm">
          <span className="text-text-secondary">Name</span>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
          />
        </label>
        <label className="block text-sm">
          <span className="text-text-secondary">Opens at</span>
          <input
            type="datetime-local"
            value={opensAt}
            onChange={(e) => setOpensAt(e.target.value)}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
          />
        </label>
        <label className="block text-sm">
          <span className="text-text-secondary">Closes at</span>
          <input
            type="datetime-local"
            value={closesAt}
            onChange={(e) => setClosesAt(e.target.value)}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
          />
        </label>
        <label className="block text-sm">
          <span className="text-text-secondary">Allow repos (one host per line)</span>
          <textarea
            value={allowRepos}
            onChange={(e) => setAllowRepos(e.target.value)}
            rows={5}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm font-mono text-xs"
          />
        </label>
        <label className="block text-sm">
          <span className="text-text-secondary">Node IDs (comma or whitespace separated)</span>
          <textarea
            value={nodeIDs}
            onChange={(e) => setNodeIDs(e.target.value)}
            rows={3}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm font-mono text-xs"
          />
        </label>
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" onClick={submit} disabled={busy}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Schedule
          </Button>
        </div>
      </div>
    </aside>
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

function proxyStatusTone(s: SquidProxy['Status']): 'healthy' | 'warning' | 'degraded' | 'critical' | 'unknown' {
  switch (s) {
    case 'healthy':
      return 'healthy';
    case 'installing':
      return 'warning';
    case 'degraded':
      return 'degraded';
    case 'removing':
      return 'warning';
    case 'removed':
      return 'unknown';
    default:
      return 'unknown';
  }
}

function windowStatusTone(s: MaintenanceWindow['Status']): 'healthy' | 'warning' | 'critical' | 'info' | 'unknown' {
  switch (s) {
    case 'open':
      return 'healthy';
    case 'closing':
      return 'warning';
    case 'aborted':
      return 'critical';
    case 'closed':
      return 'unknown';
    case 'scheduled':
    default:
      return 'info';
  }
}

function countdownText(w: MaintenanceWindow): string {
  const now = Date.now();
  const opens = new Date(w.OpensAt).getTime();
  const closes = new Date(w.ClosesAt).getTime();
  if (w.Status === 'scheduled' && opens > now) {
    return `opens in ${formatDuration(opens - now)}`;
  }
  if ((w.Status === 'open' || w.Status === 'closing') && closes > now) {
    return `closes in ${formatDuration(closes - now)}`;
  }
  return `${w.Status}`;
}

function formatDuration(ms: number): string {
  const sec = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
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
  const [editingNode, setEditingNode] = useState<NodePatchState | null>(null);

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
              <th className="px-2 py-1"></th>
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
                <td className="px-2 py-1 text-right">
                  <Button variant="ghost" size="sm" onClick={() => setEditingNode(r)}>
                    Config
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {editingNode && (
        <NodeConfigEditor
          node={editingNode}
          onClose={() => setEditingNode(null)}
        />
      )}
    </aside>
  );
}

function NodeConfigEditor({
  node,
  onClose,
}: {
  node: NodePatchState;
  onClose: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [cfg, setCfg] = useState<NodePatchConfig | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancel = false;
    (async () => {
      try {
        const data = await client.getNodePatchConfig(node.NodeID);
        if (!cancel) setCfg(data);
      } catch {
        if (!cancel) setCfg({ NodeID: node.NodeID, Mode: 'direct', UpdatedAt: '' });
      }
    })();
    return () => {
      cancel = true;
    };
  }, [client, node.NodeID]);

  const save = async (mode: 'direct' | 'proxy' | 'airgapped') => {
    setBusy(true);
    try {
      const updated = await client.upsertNodePatchConfig({ node_id: node.NodeID, mode });
      setCfg(updated);
      toast.success('Patch config updated');
    } catch (err) {
      toast.error(`Save failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <aside className="fixed right-0 top-0 z-50 h-full w-full max-w-md overflow-y-auto border-l border-border bg-elevated p-6 shadow-2xl">
      <div className="mb-4 flex justify-between">
        <h3 className="text-lg font-semibold">Per-node patch mode</h3>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      <p className="mb-3 text-xs text-text-secondary">
        Node <span className="font-mono">{node.NodeID.slice(0, 8)}</span>
      </p>
      <div className="space-y-2">
        {(['direct', 'proxy', 'airgapped'] as const).map((m) => (
          <button
            key={m}
            type="button"
            disabled={busy}
            onClick={() => save(m)}
            className={`w-full rounded border p-3 text-left text-sm ${
              cfg?.Mode === m ? 'border-primary bg-surface-2' : 'border-border bg-surface-1'
            }`}
          >
            <div className="font-medium">{m}</div>
            <div className="mt-1 text-xs text-text-secondary">
              {m === 'direct' && 'Run apt/dnf/winget upgrade against the upstream repos directly.'}
              {m === 'proxy' && 'Route package-manager traffic through a managed Squid proxy.'}
              {m === 'airgapped' && 'Read packages from a pre-staged repo path on the node — no upstream traffic.'}
            </div>
          </button>
        ))}
      </div>
    </aside>
  );
}

export default PatchManagement;
