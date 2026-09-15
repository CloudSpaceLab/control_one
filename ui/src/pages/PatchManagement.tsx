import { useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, RefreshCw, ShieldAlert, ShieldCheck, Clock, Server, Plus, Trash2, Check, X, Hourglass } from 'lucide-react';
import { ConfirmModal } from '@/components/ConfirmModal';
import { Button } from '../components/ui/button';
import { Skeleton } from '../components/ui/skeleton';
import { Alert, SectionHeader, EmptyState, StatusTag, KpiTile, type StateTone } from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { toast } from 'sonner';
import type {
  PatchDeployment,
  NodePatchState,
  NodePatchConfig,
  MaintenanceWindow,
  SquidProxy,
  NodeSummary,
  PatchApproval,
} from '../lib/api';

// PatchManagement is the operator console for fleet OS-package patching.
// Wave C extends the page with Squid proxy management, maintenance window
// scheduling, per-node mode configuration, approval gates, per-node selection,
// and the approval queue.
type Tab = 'deployments' | 'proxies' | 'windows' | 'approvals';
type LoadErrorKey = 'deployments' | 'proxies' | 'windows' | 'approvals';
type LoadErrors = Partial<Record<LoadErrorKey, string>>;

interface InlineActionState {
  busy?: boolean;
  message?: string;
  tone?: StateTone;
}

type PendingProxyRemoval = { proxy: SquidProxy; error?: string };
type PendingForceClose = { window: MaintenanceWindow; error?: string };
type PendingApprovalDeny = { approval: PatchApproval; error?: string };

export function PatchManagement(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [tab, setTab] = useState<Tab>('deployments');
  const [deployments, setDeployments] = useState<PatchDeployment[]>([]);
  const [proxies, setProxies] = useState<SquidProxy[]>([]);
  const [windows, setWindows] = useState<MaintenanceWindow[]>([]);
  const [pendingApprovals, setPendingApprovals] = useState<PatchApproval[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadErrors, setLoadErrors] = useState<LoadErrors>({});
  const [selected, setSelected] = useState<PatchDeployment | null>(null);
  const [showProxyForm, setShowProxyForm] = useState(false);
  const [showWindowForm, setShowWindowForm] = useState(false);
  const [showDeployForm, setShowDeployForm] = useState(false);

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setDeployments([]);
      setProxies([]);
      setWindows([]);
      setPendingApprovals([]);
      setLoadErrors({});
      return;
    }
    setLoading(true);
    setLoadErrors({});
    const nextErrors: LoadErrors = {};
    const [deps, proxyList, windowList, approvals] = await Promise.allSettled([
      client.listPatchDeployments({ tenantId: currentTenantId, limit: 50 }),
      client.listSquidProxies(currentTenantId),
      client.listMaintenanceWindows(currentTenantId),
      client.listPatchApprovals({ status: 'pending', tenantId: currentTenantId, limit: 100 }),
    ]);

    if (deps.status === 'fulfilled') {
      setDeployments(deps.value.deployments ?? []);
    } else {
      setDeployments([]);
      nextErrors.deployments = errorMessage(deps.reason, 'Patch deployments could not be loaded.');
    }

    if (proxyList.status === 'fulfilled') {
      setProxies(proxyList.value.proxies ?? []);
    } else {
      setProxies([]);
      nextErrors.proxies = errorMessage(proxyList.reason, 'Proxy inventory could not be loaded.');
    }

    if (windowList.status === 'fulfilled') {
      setWindows(windowList.value.windows ?? []);
    } else {
      setWindows([]);
      nextErrors.windows = errorMessage(windowList.reason, 'Maintenance windows could not be loaded.');
    }

    if (approvals.status === 'fulfilled') {
      setPendingApprovals(approvals.value.data ?? []);
    } else {
      setPendingApprovals([]);
      nextErrors.approvals = errorMessage(approvals.reason, 'Patch approvals could not be loaded.');
    }

    setLoadErrors(nextErrors);
    setLoading(false);
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
  const hasLoadErrors = Object.keys(loadErrors).length > 0;

  if (!currentTenantId) {
    return (
      <div className="space-y-6">
        <SectionHeader title="Patch management" description="Fleet OS-package upgrades dispatched via the agent." />
        <EmptyState title="Select a tenant" description="Choose a tenant from the header to view patch deployments." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <SectionHeader
        title="Patch management"
        description="Direct, proxy and airgapped OS-package upgrades fanned out per node. Every deploy passes through the same opt-out / change-window / circuit-breaker / approval gates the compliance remediation engine uses."
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
              <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
            <Button size="sm" onClick={() => setShowDeployForm(true)}>
              <ShieldAlert className="mr-2 h-4 w-4" />
              Deploy patches…
            </Button>
          </div>
        }
      />

      <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
        <KpiTile label="Total deployments" value={loadErrors.deployments ? 'N/A' : String(totals.total)} />
        <KpiTile
          label="In flight"
          value={loadErrors.deployments ? 'N/A' : String(totals.inFlight)}
          tone={totals.inFlight > 0 ? 'warning' : 'unknown'}
        />
        <KpiTile label="Completed" value={loadErrors.deployments ? 'N/A' : String(totals.completed)} tone="healthy" />
        <KpiTile
          label="Failed / partial"
          value={loadErrors.deployments ? 'N/A' : String(totals.failed)}
          tone={totals.failed > 0 || loadErrors.deployments ? 'critical' : 'unknown'}
        />
      </div>

      <div className="flex items-center gap-1 overflow-x-auto border-b border-border">
        <TabButton
          active={tab === 'deployments'}
          onClick={() => setTab('deployments')}
          label={tabLabel('Deployments', deployments.length, loadErrors.deployments)}
        />
        <TabButton
          active={tab === 'proxies'}
          onClick={() => setTab('proxies')}
          label={tabLabel('Proxies', proxies.length, loadErrors.proxies)}
        />
        <TabButton
          active={tab === 'windows'}
          onClick={() => setTab('windows')}
          label={tabLabel('Windows', windows.length, loadErrors.windows)}
        />
        <TabButton
          active={tab === 'approvals'}
          onClick={() => setTab('approvals')}
          label={tabLabel('Approvals', pendingApprovals.length, loadErrors.approvals)}
        />
      </div>

      {hasLoadErrors ? (
        <Alert
          variant={loadErrors.deployments ? 'critical' : 'warning'}
          title={loadErrors.deployments ? 'Patch management data unavailable' : 'Patch management data partially unavailable'}
          actions={
            <Button type="button" variant="secondary" size="sm" onClick={() => void refresh()} disabled={loading}>
              Retry
            </Button>
          }
        >
          {loadErrorSummary(loadErrors)}
        </Alert>
      ) : null}

      {tab === 'deployments' && (
        <DeploymentsPanel
          loading={loading}
          deployments={deployments}
          loadError={loadErrors.deployments}
          onSelect={setSelected}
          onJumpToApprovals={() => setTab('approvals')}
          pendingApprovalCount={pendingApprovals.length}
        />
      )}
      {tab === 'proxies' && (
        <ProxiesPanel
          proxies={proxies}
          loadError={loadErrors.proxies}
          tenantId={currentTenantId}
          onCreate={() => setShowProxyForm(true)}
          onChanged={refresh}
        />
      )}
      {tab === 'windows' && (
        <WindowsPanel
          windows={windows}
          loadError={loadErrors.windows}
          tenantId={currentTenantId}
          onCreate={() => setShowWindowForm(true)}
          onChanged={refresh}
        />
      )}
      {tab === 'approvals' && (
        <ApprovalQueue
          approvals={pendingApprovals}
          loading={loading}
          loadError={loadErrors.approvals}
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
      {showDeployForm && currentTenantId && (
        <DeployForm
          tenantId={currentTenantId}
          onClose={() => setShowDeployForm(false)}
          onSubmitted={(awaitingCount) => {
            setShowDeployForm(false);
            refresh();
            if (awaitingCount > 0) {
              setTab('approvals');
            }
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
      className={`shrink-0 px-3 py-2 text-sm font-medium border-b-2 -mb-px ${
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
  loadError,
  onSelect,
  onJumpToApprovals,
  pendingApprovalCount,
}: {
  loading: boolean;
  deployments: PatchDeployment[];
  loadError?: string;
  onSelect: (d: PatchDeployment) => void;
  onJumpToApprovals: () => void;
  pendingApprovalCount: number;
}): JSX.Element {
  if (loading && deployments.length === 0) return <Skeleton className="h-32 w-full" />;
  if (loadError && deployments.length === 0) {
    return (
      <EmptyState
        title="Patch deployments unavailable"
        description={loadError}
      />
    );
  }
  if (!loading && deployments.length === 0) {
    return (
      <EmptyState
        title="No deployments yet"
        description="Click Deploy patches… to pick a node subset and dispatch apt-get / dnf / winget upgrade. Each node passes through the 4 safety gates."
      />
    );
  }
  return (
    <div className="space-y-3">
      {pendingApprovalCount > 0 && (
        <div className="flex items-center justify-between rounded border border-warning/40 bg-warning/10 p-3 text-sm">
          <span className="flex items-center gap-2">
            <Hourglass className="h-4 w-4" />
            {pendingApprovalCount} patch deployment{pendingApprovalCount === 1 ? '' : 's'} awaiting approval.
          </span>
          <Button variant="outline" size="sm" onClick={onJumpToApprovals}>
            Review queue
          </Button>
        </div>
      )}
      <div className="overflow-x-auto rounded border border-border">
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
    </div>
  );
}

function ProxiesPanel({
  proxies,
  loadError,
  tenantId,
  onCreate,
  onChanged,
}: {
  proxies: SquidProxy[];
  loadError?: string;
  tenantId: string;
  onCreate: () => void;
  onChanged: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [actionState, setActionState] = useState<Record<string, InlineActionState>>({});
  const [pendingRemoval, setPendingRemoval] = useState<PendingProxyRemoval | null>(null);
  void tenantId;

  const remove = (p: SquidProxy) => {
    setPendingRemoval({ proxy: p });
  };

  const confirmRemoval = async () => {
    if (!pendingRemoval) return;
    const proxy = pendingRemoval.proxy;
    const key = proxyActionKey(proxy);
    setActionState((state) => ({ ...state, [key]: { busy: true } }));
    try {
      await client.removeSquidProxy(proxy.ID);
      setActionState((state) => {
        const next = { ...state };
        delete next[key];
        return next;
      });
      setPendingRemoval(null);
      toast.success('Proxy removal queued');
      onChanged();
    } catch (err) {
      const message = `Failed to remove proxy ${proxy.Host}:${proxy.Port}: ${errorMessage(err, 'remove failed')}`;
      setActionState((state) => ({ ...state, [key]: { busy: false, message, tone: 'critical' } }));
      setPendingRemoval((current) =>
        current?.proxy.ID === proxy.ID ? { ...current, error: message } : current,
      );
      toast.error(message);
    }
  };

  const pendingBusy = pendingRemoval ? Boolean(actionState[proxyActionKey(pendingRemoval.proxy)]?.busy) : false;

  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <Button size="sm" onClick={onCreate}>
          <Plus className="mr-2 h-4 w-4" /> Install proxy
        </Button>
      </div>
      {loadError && proxies.length === 0 ? (
        <EmptyState
          title="Managed proxies unavailable"
          description={loadError}
        />
      ) : proxies.length === 0 ? (
        <EmptyState
          title="No managed proxies"
          description="Install a Squid proxy on a designated bastion to relay package-manager traffic for proxy-mode patch deploys."
        />
      ) : (
        <div className="overflow-x-auto rounded border border-border">
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
                    <div className="flex flex-col items-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        loading={actionState[proxyActionKey(p)]?.busy}
                        onClick={() => remove(p)}
                        disabled={p.Status === 'removing' || p.Status === 'removed'}
                        aria-label={`Remove patch proxy ${p.Host}:${p.Port}`}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                      {actionState[proxyActionKey(p)]?.message ? (
                        <p className={`max-w-[18rem] text-right text-xs ${toneText(actionState[proxyActionKey(p)]?.tone)}`}>
                          {actionState[proxyActionKey(p)]?.message}
                        </p>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ConfirmModal
        open={Boolean(pendingRemoval)}
        title={pendingRemoval ? `Remove patch proxy ${pendingRemoval.proxy.Host}:${pendingRemoval.proxy.Port}?` : 'Remove patch proxy?'}
        body="Proxy-mode patch deployments that still reference this proxy will no longer be able to use it. Existing deployment and audit history remain available."
        confirmLabel="Remove proxy"
        cancelLabel="Cancel"
        confirmDisabled={pendingBusy}
        cancelDisabled={pendingBusy}
        variant="danger"
        onConfirm={() => void confirmRemoval()}
        onCancel={() => setPendingRemoval(null)}
      >
        {pendingRemoval?.error ? (
          <Alert variant="critical" title="Proxy removal failed">
            {pendingRemoval.error}
          </Alert>
        ) : null}
      </ConfirmModal>
    </div>
  );
}

function WindowsPanel({
  windows,
  loadError,
  tenantId,
  onCreate,
  onChanged,
}: {
  windows: MaintenanceWindow[];
  loadError?: string;
  tenantId: string;
  onCreate: () => void;
  onChanged: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [busy, setBusy] = useState<string | null>(null);
  const [actionState, setActionState] = useState<Record<string, InlineActionState>>({});
  const [pendingForceClose, setPendingForceClose] = useState<PendingForceClose | null>(null);
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

  const forceClose = (w: MaintenanceWindow) => {
    setPendingForceClose({ window: w });
  };

  const confirmForceClose = async () => {
    if (!pendingForceClose) return;
    const window = pendingForceClose.window;
    const key = windowActionKey(window);
    setActionState((state) => ({ ...state, [key]: { busy: true } }));
    setBusy(window.ID);
    try {
      await client.forceCloseMaintenanceWindow(window.ID);
      setActionState((state) => {
        const next = { ...state };
        delete next[key];
        return next;
      });
      setPendingForceClose(null);
      toast.success('Window force-closed');
      onChanged();
    } catch (err) {
      const message = `Failed to force-close window ${window.Name}: ${errorMessage(err, 'force close failed')}`;
      setActionState((state) => ({ ...state, [key]: { busy: false, message, tone: 'critical' } }));
      setPendingForceClose((current) =>
        current?.window.ID === window.ID ? { ...current, error: message } : current,
      );
      toast.error(message);
    } finally {
      setBusy(null);
    }
  };

  const pendingBusy = pendingForceClose
    ? Boolean(actionState[windowActionKey(pendingForceClose.window)]?.busy)
    : false;

  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <Button size="sm" onClick={onCreate}>
          <Plus className="mr-2 h-4 w-4" /> Schedule window
        </Button>
      </div>
      {loadError && windows.length === 0 ? (
        <EmptyState
          title="Maintenance windows unavailable"
          description={loadError}
        />
      ) : windows.length === 0 ? (
        <EmptyState
          title="No maintenance windows"
          description="Schedule a window to open allow-repo firewall rules during a defined timespan for airgapped or proxy-mode patch deploys."
        />
      ) : (
        <div className="overflow-x-auto rounded border border-border">
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
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => open(w)}
                        disabled={busy === w.ID || actionState[windowActionKey(w)]?.busy}
                        aria-label={`Open maintenance window ${w.Name}`}
                      >
                        <ShieldCheck className="h-4 w-4 mr-1" /> Open
                      </Button>
                    )}
                    {(w.Status === 'open' || w.Status === 'closing') && (
                      <>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => close(w)}
                          disabled={busy === w.ID || actionState[windowActionKey(w)]?.busy}
                          aria-label={`Close maintenance window ${w.Name}`}
                        >
                          Close
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          loading={actionState[windowActionKey(w)]?.busy}
                          onClick={() => forceClose(w)}
                          disabled={busy === w.ID}
                          aria-label={`Force-close maintenance window ${w.Name}`}
                        >
                          Force
                        </Button>
                      </>
                    )}
                    {actionState[windowActionKey(w)]?.message ? (
                      <p className={`mt-1 max-w-[18rem] text-right text-xs ${toneText(actionState[windowActionKey(w)]?.tone)}`}>
                        {actionState[windowActionKey(w)]?.message}
                      </p>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ConfirmModal
        open={Boolean(pendingForceClose)}
        title={pendingForceClose ? `Force-close maintenance window ${pendingForceClose.window.Name}?` : 'Force-close maintenance window?'}
        body="This stamps the window as force-closed immediately and bypasses normal firewall teardown. Use it only when the normal close path cannot complete."
        confirmLabel="Force-close window"
        cancelLabel="Cancel"
        confirmDisabled={pendingBusy}
        cancelDisabled={pendingBusy}
        variant="danger"
        onConfirm={() => void confirmForceClose()}
        onCancel={() => setPendingForceClose(null)}
      >
        {pendingForceClose?.error ? (
          <Alert variant="critical" title="Window force-close failed">
            {pendingForceClose.error}
          </Alert>
        ) : null}
      </ConfirmModal>
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
        <div className="overflow-x-auto">
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
        </div>
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

// DeployForm replaces the old fleet-wide prompt with a per-node
// selector. Operator picks the subset, and on submit we POST node_ids to the
// existing /api/v1/patch/deployments endpoint. When the response includes
// awaiting_approval entries (PR #65), we surface them inline and the parent
// component flips to the Approvals tab.
function DeployForm({
  tenantId,
  onClose,
  onSubmitted,
}: {
  tenantId: string;
  onClose: () => void;
  onSubmitted: (awaitingCount: number) => void;
}): JSX.Element {
  const client = useApiClient();
  const [nodes, setNodes] = useState<NodeSummary[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState('');
  const [mode, setMode] = useState<'auto' | 'direct' | 'proxy' | 'airgapped'>('auto');
  const [reason, setReason] = useState('');
  const [loadingNodes, setLoadingNodes] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoadingNodes(true);
      setLoadError(null);
      try {
        const resp = await client.listNodes({ tenantId, limit: 500 });
        if (cancelled) return;
        setNodes(resp.data ?? []);
      } catch (err) {
        if (cancelled) return;
        setLoadError(err instanceof Error ? err.message : 'load failed');
      } finally {
        if (!cancelled) setLoadingNodes(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [client, tenantId]);

  const filteredNodes = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return nodes;
    return nodes.filter(
      (n) =>
        n.hostname.toLowerCase().includes(needle) ||
        n.id.toLowerCase().includes(needle) ||
        (n.public_ip ?? '').toLowerCase().includes(needle),
    );
  }, [nodes, filter]);

  const toggle = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectAllVisible = () => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      filteredNodes.forEach((n) => next.add(n.id));
      return next;
    });
  };

  const clearVisible = () => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      filteredNodes.forEach((n) => next.delete(n.id));
      return next;
    });
  };

  const submit = async () => {
    const ids = Array.from(selectedIds);
    if (ids.length === 0) {
      toast.error('Select at least one node');
      return;
    }
    setBusy(true);
    try {
      const resp = await client.createPatchDeployment({
        tenant_id: tenantId,
        node_ids: ids,
        mode,
        reason: reason.trim() || undefined,
      });
      const awaitingCount = resp.awaiting_approval?.length ?? 0;
      const blocked = resp.gate_blocked?.length ?? 0;
      const failedCount = resp.failed?.length ?? 0;
      let msg = `Patch deployment dispatched to ${resp.node_count} node${resp.node_count === 1 ? '' : 's'}`;
      if (awaitingCount > 0) msg += `; ${awaitingCount} awaiting approval`;
      if (blocked > 0) msg += `; ${blocked} blocked by safety gate`;
      if (failedCount > 0) msg += `; ${failedCount} failed to dispatch`;
      if (awaitingCount > 0) {
        toast.warning(msg);
      } else {
        toast.success(msg);
      }
      onSubmitted(awaitingCount);
    } catch (err) {
      toast.error(`Deploy failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(false);
    }
  };

  const visibleSelectedCount = filteredNodes.filter((n) => selectedIds.has(n.id)).length;

  return (
    <aside className="fixed right-0 top-0 z-40 flex h-full w-full max-w-2xl flex-col border-l border-border bg-elevated shadow-2xl">
      <div className="flex items-start justify-between border-b border-border p-6">
        <div>
          <h3 className="text-lg font-semibold">Deploy patches</h3>
          <p className="mt-1 text-xs text-text-secondary">
            Pick the nodes to receive this deployment. Each selected node passes through the 4-gate safety pipeline
            (opt-out / change window / circuit breaker / approval).
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>

      <div className="space-y-3 border-b border-border p-6">
        <div className="grid grid-cols-2 gap-3">
          <label className="block text-sm">
            <span className="text-text-secondary">Mode</span>
            <select
              value={mode}
              onChange={(e) => setMode(e.target.value as typeof mode)}
              className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
            >
              <option value="auto">auto (per-node config)</option>
              <option value="direct">direct</option>
              <option value="proxy">proxy</option>
              <option value="airgapped">airgapped</option>
            </select>
          </label>
          <label className="block text-sm">
            <span className="text-text-secondary">Reason (optional)</span>
            <input
              type="text"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
              placeholder="e.g. CVE-2026-XXXX hotfix"
            />
          </label>
        </div>
        <div className="flex items-center gap-2">
          <input
            type="search"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="filter by hostname / id / ip…"
            className="flex-1 rounded border border-border bg-surface-1 px-2 py-1 text-sm"
          />
          <Button variant="outline" size="sm" onClick={selectAllVisible} disabled={filteredNodes.length === 0}>
            Select all
          </Button>
          <Button variant="outline" size="sm" onClick={clearVisible} disabled={filteredNodes.length === 0}>
            Clear
          </Button>
        </div>
        <p className="text-xs text-text-secondary">
          {selectedIds.size} of {nodes.length} nodes selected
          {filter && ` · ${visibleSelectedCount} of ${filteredNodes.length} visible`}
        </p>
      </div>

      <div className="flex-1 overflow-y-auto p-6">
        {loadError && (
          <div className="mb-3 rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{loadError}</div>
        )}
        {loadingNodes && nodes.length === 0 ? (
          <Skeleton className="h-40 w-full" />
        ) : filteredNodes.length === 0 ? (
          <EmptyState title="No matching nodes" description={filter ? 'Adjust the filter.' : 'No enrolled nodes in this tenant.'} />
        ) : (
          <div className="overflow-x-auto rounded border border-border">
            <table className="w-full text-sm">
              <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
                <tr>
                  <th className="px-3 py-2 w-8"></th>
                  <th className="px-3 py-2">Hostname</th>
                  <th className="px-3 py-2">OS</th>
                  <th className="px-3 py-2">State</th>
                  <th className="px-3 py-2">Last seen</th>
                </tr>
              </thead>
              <tbody>
                {filteredNodes.map((n) => {
                  const checked = selectedIds.has(n.id);
                  return (
                    <tr
                      key={n.id}
                      className={`border-t border-border cursor-pointer hover:bg-hover ${checked ? 'bg-surface-2' : ''}`}
                      onClick={() => toggle(n.id)}
                    >
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          aria-label={`Select node ${n.hostname}`}
                          checked={checked}
                          onChange={() => toggle(n.id)}
                          onClick={(e) => e.stopPropagation()}
                        />
                      </td>
                      <td className="px-3 py-2">
                        <div className="font-medium">{n.hostname}</div>
                        <div className="font-mono text-[10px] text-text-secondary">{n.id.slice(0, 8)}</div>
                      </td>
                      <td className="px-3 py-2 text-xs text-text-secondary">{n.os ?? '—'}</td>
                      <td className="px-3 py-2 text-xs">{n.state}</td>
                      <td className="px-3 py-2 text-xs text-text-secondary">
                        {n.last_seen_at ? new Date(n.last_seen_at).toLocaleString() : '—'}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="flex items-center justify-end gap-2 border-t border-border p-4">
        <Button variant="outline" size="sm" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button size="sm" onClick={submit} disabled={busy || selectedIds.size === 0}>
          {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <ShieldAlert className="mr-2 h-4 w-4" />}
          Deploy to {selectedIds.size} node{selectedIds.size === 1 ? '' : 's'}
        </Button>
      </div>
    </aside>
  );
}

// ApprovalQueue lists pending patch_approvals rows for the current tenant
// (PR #65) and exposes Approve / Deny controls. Approve re-runs the
// dispatch; deny lets the operator drop a parked deployment.
function ApprovalQueue({
  approvals,
  loading,
  loadError,
  onChanged,
}: {
  approvals: PatchApproval[];
  loading: boolean;
  loadError?: string;
  onChanged: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [busy, setBusy] = useState<string | null>(null);
  const [actionState, setActionState] = useState<Record<string, InlineActionState>>({});
  const [pendingDeny, setPendingDeny] = useState<PendingApprovalDeny | null>(null);

  const approve = async (a: PatchApproval) => {
    setBusy(a.id);
    try {
      await client.approvePatchApproval(a.id);
      toast.success('Approved — deployment re-dispatched');
      onChanged();
    } catch (err) {
      toast.error(`Approve failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(null);
    }
  };

  const deny = (a: PatchApproval) => {
    setPendingDeny({ approval: a });
  };

  const confirmDeny = async () => {
    if (!pendingDeny) return;
    const approval = pendingDeny.approval;
    const key = approvalActionKey(approval);
    setActionState((state) => ({ ...state, [key]: { busy: true } }));
    setBusy(approval.id);
    try {
      await client.denyPatchApproval(approval.id);
      setActionState((state) => {
        const next = { ...state };
        delete next[key];
        return next;
      });
      setPendingDeny(null);
      toast.success('Denied');
      onChanged();
    } catch (err) {
      const message = `Failed to deny patch approval for node ${approval.node_id.slice(0, 8)}: ${errorMessage(err, 'deny failed')}`;
      setActionState((state) => ({ ...state, [key]: { busy: false, message, tone: 'critical' } }));
      setPendingDeny((current) =>
        current?.approval.id === approval.id ? { ...current, error: message } : current,
      );
      toast.error(message);
    } finally {
      setBusy(null);
    }
  };

  const pendingBusy = pendingDeny ? Boolean(actionState[approvalActionKey(pendingDeny.approval)]?.busy) : false;

  if (loading && approvals.length === 0) return <Skeleton className="h-32 w-full" />;
  if (loadError && approvals.length === 0) {
    return (
      <EmptyState
        title="Patch approvals unavailable"
        description={loadError}
      />
    );
  }
  if (approvals.length === 0) {
    return (
      <EmptyState
        title="No pending approvals"
        description="When a tenant has patch_requires_approval=true, parked deployments show up here for an operator to approve or deny."
      />
    );
  }

  return (
    <>
      <div className="overflow-x-auto rounded border border-border">
      <table className="w-full text-sm">
        <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
          <tr>
            <th className="px-3 py-2">Requested</th>
            <th className="px-3 py-2">Deployment</th>
            <th className="px-3 py-2">Node</th>
            <th className="px-3 py-2">Mode</th>
            <th className="px-3 py-2">Expires</th>
            <th className="px-3 py-2 text-right"></th>
          </tr>
        </thead>
        <tbody>
          {approvals.map((a) => {
            const expired = a.expires_at && new Date(a.expires_at).getTime() < Date.now();
            const denyState = actionState[approvalActionKey(a)];
            return (
              <tr key={a.id} className="border-t border-border align-top">
                <td className="px-3 py-2 text-xs text-text-secondary">{new Date(a.created_at).toLocaleString()}</td>
                <td className="px-3 py-2 font-mono text-xs">{a.deployment_id.slice(0, 8)}</td>
                <td className="px-3 py-2 font-mono text-xs">{a.node_id.slice(0, 8)}</td>
                <td className="px-3 py-2 text-xs">{a.mode}</td>
                <td className="px-3 py-2 text-xs text-text-secondary">
                  {a.expires_at ? new Date(a.expires_at).toLocaleString() : '—'}
                  {expired && <span className="ml-1 text-destructive">(expired)</span>}
                </td>
                <td className="px-3 py-2 text-right whitespace-nowrap">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => approve(a)}
                    disabled={busy === a.id || denyState?.busy || !!expired}
                    aria-label={`Approve patch deployment for node ${a.node_id}`}
                  >
                    <Check className="h-4 w-4 mr-1" /> Approve
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    loading={denyState?.busy}
                    onClick={() => deny(a)}
                    disabled={busy === a.id}
                    aria-label={`Deny patch deployment for node ${a.node_id}`}
                  >
                    <X className="h-4 w-4 mr-1" /> Deny
                  </Button>
                  {denyState?.message ? (
                    <p className={`mt-1 max-w-[18rem] text-right text-xs ${toneText(denyState.tone)}`}>
                      {denyState.message}
                    </p>
                  ) : null}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      </div>
      <ConfirmModal
        open={Boolean(pendingDeny)}
        title={
          pendingDeny
            ? `Deny patch approval for node ${pendingDeny.approval.node_id.slice(0, 8)}?`
            : 'Deny patch approval?'
        }
        body="The parked deployment for this node will not be dispatched. Existing deployment and approval history remain available."
        confirmLabel="Deny approval"
        cancelLabel="Cancel"
        confirmDisabled={pendingBusy}
        cancelDisabled={pendingBusy}
        variant="danger"
        onConfirm={() => void confirmDeny()}
        onCancel={() => setPendingDeny(null)}
      >
        {pendingDeny?.error ? (
          <Alert variant="critical" title="Patch approval denial failed">
            {pendingDeny.error}
          </Alert>
        ) : null}
      </ConfirmModal>
    </>
  );
}

function errorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message.trim()) return err.message;
  if (typeof err === 'string' && err.trim()) return err;
  return fallback;
}

function tabLabel(label: string, count: number, error?: string): string {
  return error ? `${label} (!)`: `${label} (${count})`;
}

function loadErrorSummary(errors: LoadErrors): string {
  return (Object.entries(errors) as Array<[LoadErrorKey, string]>)
    .map(([key, value]) => `${loadErrorName(key)}: ${value}`)
    .join(' ');
}

function loadErrorName(key: LoadErrorKey): string {
  switch (key) {
    case 'deployments':
      return 'Deployments';
    case 'proxies':
      return 'Proxies';
    case 'windows':
      return 'Windows';
    case 'approvals':
      return 'Approvals';
  }
}

function proxyActionKey(proxy: SquidProxy): string {
  return `proxy:${proxy.ID}`;
}

function windowActionKey(window: MaintenanceWindow): string {
  return `window:${window.ID}`;
}

function approvalActionKey(approval: PatchApproval): string {
  return `approval:${approval.id}`;
}

function toneText(tone?: StateTone): string {
  switch (tone) {
    case 'critical':
      return 'text-state-critical';
    case 'warning':
      return 'text-state-warning';
    case 'healthy':
      return 'text-state-healthy';
    default:
      return 'text-text-muted';
  }
}

export default PatchManagement;
