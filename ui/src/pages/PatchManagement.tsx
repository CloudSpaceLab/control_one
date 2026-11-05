import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, RefreshCw, ShieldAlert, ShieldCheck, Clock, Server, Plus, Check, X, Hourglass } from 'lucide-react';
import { ConfirmModal } from '@/components/ConfirmModal';
import { Button } from '../components/ui/button';
import { Skeleton } from '../components/ui/skeleton';
import { Alert, SectionHeader, EmptyState, StatusTag, KpiTile, type StateTone } from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { useAuth } from '../providers/AuthProvider';
import { AllTenantPatchSummary } from '../features/patch-management/AllTenantPatchSummary';
import { PatchAssessmentNotice } from '../features/patch-management/PatchAssessmentNotice';
import { toast } from 'sonner';
import { agentPublishedOfflinePlan, offlinePlanPreparation, parseOfflinePatchContent, requireCurrentOfflinePlan } from '../lib/offlinePatchContent';
import {OnlinePlanReview} from '../features/patch-management/OnlinePlanReview';
import {OnlinePlanPreparation} from '../features/patch-management/OnlinePlanPreparation';
import {currentOnlinePlan,hasOnlinePlanner,requireCurrentOnlinePlan,onlinePlanForReview} from '../lib/onlinePatchPlan';
import { OfflineContentReview } from '../features/patch-management/OfflineContentReview';
import { OfflinePlanPreparation } from '../features/patch-management/OfflinePlanPreparation';
import {ProxyManagementDialog} from '../features/patch-management/ProxyManagementDialog';
import {PatchReviewPanel} from '../features/patch-management/PatchReviewPanel';
import {PatchOrganizationPanel} from '../features/patch-management/PatchOrganizationPanel';
import {TenantRolePanel} from '../features/patch-management/TenantRolePanel';
import {PatchOperationsPanel} from '../features/patch-management/PatchOperationsPanel';
import type {
  PatchDeployment,
  PatchDeploymentSummary,
  NodePatchState,
  NodePatchConfig,
  MaintenanceWindow,
  SquidProxy,
  NodeSummary,
  PatchApproval,
  OfflinePatchContent,
  ExactOnlinePlan,
} from '../lib/api';

// PatchManagement separates measured fleet posture from bounded recent
// deployment activity, while keeping proxy, maintenance-window and approval
// operations tenant-scoped.
type Tab = 'deployments' | 'proxies' | 'windows' | 'approvals' | 'reviews';
type LoadErrorKey = 'posture' | 'deployments' | 'proxies' | 'windows' | 'approvals';
type LoadErrors = Partial<Record<LoadErrorKey, string>>;

interface InlineActionState {
  busy?: boolean;
  message?: string;
  tone?: StateTone;
}

type PendingForceClose = { window: MaintenanceWindow; error?: string };
type PendingApprovalDeny = { approval: PatchApproval; error?: string };

export function PatchManagement(): JSX.Element {
  const client = useApiClient();
  const { profile } = useAuth();
  const canRequestPatches = (profile?.roles ?? []).some(role => role === 'admin' || role === 'operator');
  const { currentTenantId } = useTenant();
  const [tab, setTab] = useState<Tab>(()=>new URLSearchParams(window.location.search).get('view')==='reviews'?'reviews':'deployments');
  const [posture, setPosture] = useState<PatchDeploymentSummary | null>(null);
  const [deployments, setDeployments] = useState<PatchDeployment[]>([]);
  const [proxies, setProxies] = useState<SquidProxy[]>([]);
  const [windows, setWindows] = useState<MaintenanceWindow[]>([]);
  const [pendingApprovals, setPendingApprovals] = useState<PatchApproval[]>([]);
  const [pendingApprovalTotal, setPendingApprovalTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [loadErrors, setLoadErrors] = useState<LoadErrors>({});
  const [selected, setSelected] = useState<PatchDeployment | null>(null);
  const [showProxyForm, setShowProxyForm] = useState(false);
  const [showWindowForm, setShowWindowForm] = useState(false);
  const [showDeployForm, setShowDeployForm] = useState(false);

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setPosture(null);
      setDeployments([]);
      setProxies([]);
      setWindows([]);
      setPendingApprovals([]);
      setPendingApprovalTotal(0);
      setLoadErrors({});
      return;
    }
    setLoading(true);
    setLoadErrors({});
    const nextErrors: LoadErrors = {};
    const [summary, deps, proxyList, windowList, approvals] = await Promise.allSettled([
      client.getPatchSummary(currentTenantId),
      client.listPatchDeployments({ tenantId: currentTenantId, limit: 50 }),
      client.listSquidProxies(currentTenantId),
      client.listMaintenanceWindows(currentTenantId),
      canRequestPatches
        ? client.listPatchApprovals({ status: 'pending', tenantId: currentTenantId, limit: 100 })
        : Promise.resolve({ data: [] as PatchApproval[], pagination: { total: 0 } }),
    ]);

    if (summary.status === 'fulfilled') {
      setPosture(summary.value);
    } else {
      setPosture(null);
      nextErrors.posture = errorMessage(summary.reason, 'Patch posture could not be loaded.');
    }

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
      const rows = approvals.value.data ?? [];
      setPendingApprovals(rows);
      setPendingApprovalTotal(approvals.value.pagination?.total ?? rows.length);
    } else {
      setPendingApprovals([]);
      setPendingApprovalTotal(0);
      nextErrors.approvals = errorMessage(approvals.reason, 'Patch approvals could not be loaded.');
    }

    setLoadErrors(nextErrors);
    setLoading(false);
  }, [client, currentTenantId, canRequestPatches]);

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
    return <AllTenantPatchSummary />;
  }

  return (
    <div className="space-y-6">
      <SectionHeader
        title="Patch posture"
        description="Package coverage, known patch risk and deployment state."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button asChild variant="outline" size="sm">
              <a href="/console/control-room/patch-posture">View management overview</a>
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} loading={loading}>
	              <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
	              {loading ? 'Refreshing…' : 'Refresh'}
            </Button>
            {canRequestPatches && <Button size="sm" onClick={() => setShowDeployForm(true)}>
              <ShieldAlert className="mr-2 h-4 w-4" />
              Deploy patches…
            </Button>}
          </div>
        }
      />

      <PatchAssessmentNotice assessment={loadErrors.posture ? undefined : posture?.assessment} />

      <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
        <KpiTile
          label="Fresh package inventory"
          value={loadErrors.posture || !posture ? 'N/A' : `${posture.fresh_inventory_nodes} / ${posture.active_nodes}`}
          hint={
            posture && !loadErrors.posture
              ? `${posture.inventory_nodes} inventoried · ${Math.max(0, posture.inventory_nodes - posture.fresh_inventory_nodes)} stale`
              : undefined
          }
          tone={
            !posture || loadErrors.posture || posture.active_nodes === 0
              ? 'unknown'
              : posture.fresh_inventory_nodes === posture.active_nodes
                ? 'healthy'
                : 'warning'
          }
        />
        <KpiTile
          label="Known critical findings"
          value={loadErrors.posture || !posture ? 'N/A' : String(posture.known_critical_findings)}
          hint={posture && !loadErrors.posture ? `${posture.known_kev_findings} KEV · ${posture.known_patchable_findings} with known fix` : undefined}
          tone={posture && !loadErrors.posture && posture.known_critical_findings > 0 ? 'critical' : 'unknown'}
        />
        <KpiTile
          label="Known affected nodes"
          value={loadErrors.posture || !posture ? 'N/A' : String(posture.known_affected_nodes)}
          hint={posture && !loadErrors.posture ? `${posture.known_active_findings} unresolved findings` : undefined}
          tone={posture && !loadErrors.posture && posture.known_affected_nodes > 0 ? 'warning' : 'unknown'}
        />
        <KpiTile
          label="Pending approvals"
          value={loadErrors.posture || !posture ? 'N/A' : String(posture.pending_approvals)}
          hint={posture && !loadErrors.posture && posture.expired_approvals > 0 ? `${posture.expired_approvals} expired` : undefined}
          tone={posture && !loadErrors.posture && posture.pending_approvals > 0 ? 'warning' : 'healthy'}
        />
      </div>

      <div className="space-y-2">
        <p className="text-xs font-medium uppercase tracking-wider text-text-secondary">
          Recent deployment activity · latest 50
        </p>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
          <KpiTile label="Deployments" value={loadErrors.deployments ? 'N/A' : String(totals.total)} size="sm" />
          <KpiTile label="In flight" value={loadErrors.deployments ? 'N/A' : String(totals.inFlight)} tone={totals.inFlight > 0 ? 'warning' : 'unknown'} size="sm" />
          <KpiTile label="Completed" value={loadErrors.deployments ? 'N/A' : String(totals.completed)} tone="healthy" size="sm" />
          <KpiTile label="Failed / partial" value={loadErrors.deployments ? 'N/A' : String(totals.failed)} tone={totals.failed > 0 ? 'critical' : 'healthy'} size="sm" />
        </div>
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
        {canRequestPatches && <TabButton
          active={tab === 'approvals'}
          onClick={() => setTab('approvals')}
          label={tabLabel('Approvals', pendingApprovalTotal, loadErrors.approvals)}
        />}
        {currentTenantId&&<TabButton active={tab==='reviews'} onClick={()=>setTab('reviews')} label="Review policy & ownership"/>}
      </div>

      {hasLoadErrors ? (
        <Alert
          variant={loadErrors.posture || loadErrors.deployments ? 'critical' : 'warning'}
          title={
            loadErrors.posture
              ? 'Patch posture unavailable'
              : loadErrors.deployments
                ? 'Patch deployment data unavailable'
                : 'Patch management data partially unavailable'
          }
          actions={
            <Button type="button" variant="secondary" size="sm" onClick={() => void refresh()} disabled={loading}>
              Retry
            </Button>
          }
        >
          {loadErrorSummary(loadErrors)}
        </Alert>
      ) : null}

      {tab==='reviews'&&currentTenantId&&<><TenantRolePanel tenantId={currentTenantId}/><PatchOrganizationPanel tenantId={currentTenantId}/><PatchReviewPanel tenantId={currentTenantId}/><PatchOperationsPanel tenantId={currentTenantId}/></>}
      {tab === 'deployments' && (
        <DeploymentsPanel
          loading={loading}
          deployments={deployments}
          loadError={loadErrors.deployments}
          onSelect={setSelected}
          onJumpToApprovals={() => setTab('approvals')}
          pendingApprovalCount={posture && !loadErrors.posture ? posture.pending_approvals : pendingApprovalTotal}
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
      {tab === 'approvals' && canRequestPatches && (
        <ApprovalQueue
          approvals={pendingApprovals}
          totalCount={pendingApprovalTotal}
          loading={loading}
          loadError={loadErrors.approvals}
          onChanged={refresh}
        />
      )}
      {tab === 'approvals' && !canRequestPatches && <p>Installation approvals require an authorized tenant operator or administrator. Organizational policy review is available under Review policy & ownership.</p>}

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

function deploymentModeLabel(deployment: PatchDeployment): string {
  if (deployment.Summary?.requested_mode !== 'auto') return deployment.Mode;
  const waves = deployment.Summary.waves;
  if (!Array.isArray(waves)) return 'auto (per node)';
  const modes = new Set<string>();
  for (const wave of waves) {
    if (!wave || !Array.isArray(wave.nodes)) return 'auto (per node)';
    for (const node of wave.nodes) {
      if (!node || !['direct', 'proxy', 'airgapped'].includes(node.mode)) return 'auto (per node)';
      modes.add(node.mode);
    }
  }
  return modes.size === 1 ? `auto (${[...modes][0]})` : 'auto (per node)';
}

function DeploymentPackageScope({ deployment }: { deployment: PatchDeployment }): JSX.Element {
  const summary = deployment.Summary;
  const validNames = (value: unknown): value is string[] | null =>
    value === null || (Array.isArray(value) && value.every((name) => typeof name === 'string' && name.trim().length > 0));
  if (!summary || !Object.prototype.hasOwnProperty.call(summary, 'package_allowlist') ||
      !validNames(summary.package_allowlist) ||
      (summary.package_denylist !== undefined && !validNames(summary.package_denylist))) {
    return <span className="text-text-secondary">Package scope unavailable</span>;
  }
  const allowed = summary.package_allowlist ?? [];
  const denied = summary.package_denylist ?? [];
  return <div className="text-xs">
    <div>{allowed.length ? allowed.join(', ') : 'All eligible packages'}</div>
    {!!denied.length && <div>Excluded: {denied.join(', ')}</div>}
    <div className="text-text-secondary">Required dependencies may also change.</div>
  </div>;
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
        description="Deploy patches to a selected node group."
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
              <th className="px-3 py-2">Package scope</th>
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
                <td className="px-3 py-2"><DeploymentPackageScope deployment={d} /></td>
                <td className="px-3 py-2">{deploymentModeLabel(d)}</td>
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
  const [managedProxy, setManagedProxy] = useState<SquidProxy | null>(null);
  useEffect(() => setManagedProxy(null), [tenantId]);

  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <Button size="sm" onClick={onCreate}>
          <Plus className="mr-2 h-4 w-4" /> Register proxy
        </Button>
      </div>
      <p className="text-sm text-text-secondary">
        Recorded status is not a live connectivity check. Confirm repository access from the target before rollout;
        a queued configuration change has not yet been applied or verified.
      </p>
      <Alert variant="warning" title="Managed proxy requirements">Registration saves connection details. Management requires a bound enrolled host, a current supported agent and reviewed client/repository authorization. Recorded health applies to the last completed verification.</Alert>
      {loadError && proxies.length === 0 ? (
        <EmptyState
          title="Managed proxies unavailable"
          description={loadError}
        />
      ) : proxies.length === 0 ? (
        <EmptyState
          title="No registered proxies"
          description="Register an existing approved proxy for proxy-mode patching. Registration does not install software or verify connectivity."
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
                <th className="px-3 py-2">Last recorded validation</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {proxies.map((p) => (
                <tr key={p.ID} className="border-t border-border align-top">
                  <td className="px-3 py-2 font-mono text-xs">{p.Host}</td>
                  <td className="px-3 py-2">{p.Port}</td>
                  <td className="px-3 py-2">
                    <StatusTag tone={p.Evidence?.state === 'verification_recorded' ? 'info' : 'unknown'}>{proxyEvidenceLabel(p)}</StatusTag>
                    <p className="mt-1 max-w-xs text-xs text-text-secondary">{p.Evidence?.message ?? 'Lifecycle evidence unavailable; saved status does not establish progress or health.'}</p>
                    {p.Evidence?.job_id && <p className="mt-1 font-mono text-xs">Job {p.Evidence.job_id.slice(0, 8)}</p>}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">
                    {p.Whitelist.length === 0 ? '—' : `${p.Whitelist.length} hosts`}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">
                    {p.LastValidatedAt ? new Date(p.LastValidatedAt).toLocaleString() : '—'}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <div className="flex flex-col items-end gap-1">
                      <Button variant="outline" size="sm" onClick={() => setManagedProxy(p)} aria-label={`Manage patch proxy ${p.Host}:${p.Port}`}>Manage</Button>

                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {managedProxy && managedProxy.TenantID === tenantId && <ProxyManagementDialog key={`${tenantId}:${managedProxy.ID}`} proxy={managedProxy} tenantId={tenantId} onClose={() => setManagedProxy(null)} onChanged={onChanged} />}
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
      toast.success('Calendar window opening queued');
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
      toast.success('Window scheduling authorization revoked; running installations and network closure are not confirmed');
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
      <p className="text-sm text-text-secondary">
        Calendar windows control when a new installation may start. Network-controlled windows are unavailable until
        target enforcement is qualified. Closing a window does not confirm firewall closure or stop an installation already running.
      </p>
      {loadError && windows.length === 0 ? (
        <EmptyState
          title="Maintenance windows unavailable"
          description={loadError}
        />
      ) : windows.length === 0 ? (
        <EmptyState
          title="No maintenance windows"
          description="Create a maintenance window for scheduled patch access."
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
        body="This revokes scheduling authorization for new patch requests. It does not confirm network closure or stop an installation already running."
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
      toast.success('Proxy registered; installation and connectivity are not yet verified');
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
        <h3 className="text-lg font-semibold">Register patch proxy</h3>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      <div className="space-y-3">
        <p className="text-sm text-text-secondary">Register an existing approved proxy. This saves its connection details; it does not install or reconfigure Squid, grant remote-client access, or verify repository connectivity.</p>
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
            Register proxy
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
  const [allowRepos, setAllowRepos] = useState('');
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
          <span className="text-text-secondary">Allow repos (network enforcement unavailable)</span>
          <textarea
            value={allowRepos}
            onChange={(e) => setAllowRepos(e.target.value)}
            rows={5}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm font-mono text-xs"
          />
        </label>
        <p className="text-xs text-text-secondary">
          Leave repositories empty for a calendar-only window. Windows that change network access are blocked
          until target enforcement is qualified. This schedule does not open firewall access.
        </p>
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

function proxyEvidenceLabel(p: SquidProxy): string {
  const labels: Record<string,string> = {legacy_unverified:'Legacy / unverified',registered:'Registered / unverified',pending:'Awaiting target result',outcome_unknown:'Outcome unknown',failed_or_unknown:'Failed / unknown',removed_verified:'Removal verified',verification_recorded:'Verification recorded',unavailable:'Evidence unavailable',unverified:'Unverified'};
  return labels[p.Evidence?.state ?? ''] ?? 'Evidence unavailable';
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

export function DeploymentNodeDetail({
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
  const [resultRevision, setResultRevision] = useState(0);
  const approvedOfflinePlan = useMemo(() => {
    const value = deployment.Summary?.offline_content;
    if (!value) return null;
    try { return parseOfflinePatchContent(JSON.stringify(value)); } catch { return null; }
  }, [deployment.Summary]);

  useEffect(() => {
    let cancel = false;
    (async () => {
      setLoading(true);
      setError(null);
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
  }, [deployment.ID, client, resultRevision]);

  return (
    <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-2xl overflow-y-auto border-l border-border bg-surface p-6 shadow-2xl">
      <div className="mb-4 flex items-start justify-between">
        <div>
          <p className="text-xs uppercase tracking-wider text-text-secondary">Deployment</p>
          <h3 className="font-mono text-lg">{deployment.ID.slice(0, 8)}</h3>
          <p className="text-xs text-text-secondary">
            {deploymentModeLabel(deployment)} · {new Date(deployment.RequestedAt).toLocaleString()}
          </p>
        </div>
        <div className="flex gap-2"><Button variant="ghost" size="sm" disabled={loading} onClick={() => setResultRevision(value => value + 1)}>Refresh result</Button>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
        </div>
      </div>
      {loading && <Skeleton className="h-32 w-full" />}
      <section className="mb-4 rounded border border-border p-3" aria-label="Requested package scope">
        <p className="mb-1 text-xs font-medium">Requested package scope</p>
        <DeploymentPackageScope deployment={deployment} />
        <p className="mt-2 text-xs text-text-secondary">PKGS is the actual number upgraded on each node. It can be 0 when the requested packages are already current.</p>
      </section>
      {onlinePlanForReview(deployment.Summary?.online_plan)&&<details className="mb-4"><summary className="mb-2 cursor-pointer text-sm">Approved exact online plan</summary><OnlinePlanReview plan={onlinePlanForReview(deployment.Summary?.online_plan)!}/></details>}
      {approvedOfflinePlan && <details className="mb-4"><summary className="mb-2 cursor-pointer text-sm">Approved offline content plan</summary><p className="mb-2 text-xs text-text-secondary">Plan source: {deployment.Summary?.published_offline_plan_sha256 ? 'Agent-published plan' : 'Manual JSON import'}</p><OfflineContentReview content={approvedOfflinePlan} /></details>}
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
              <th className="px-2 py-1">Completed at</th>
              <th className="px-2 py-1"></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.ID} className="border-t border-border align-top">
                <td className="px-2 py-1 font-mono text-xs">{r.NodeID.slice(0, 8)}</td>
                <td className="px-2 py-1">
                  <StatusTag tone={nodeStatusTone(r.Status)}>{r.Status}</StatusTag>
                  <p className="mt-1 text-xs text-text-secondary">Verification: {r.Verification?.current_state.replaceAll('_', ' ') ?? 'unavailable'}</p>
                  <p className="text-xs text-text-secondary">Promotion: {r.Verification?.promotion_eligible ? 'eligible' : 'blocked'}</p>
                </td>
                <td className="px-2 py-1 text-xs">{r.PackagesUpgraded ?? '—'}</td>
                <td className="px-2 py-1 text-xs text-text-secondary">
                  {r.Error ? <span className="text-destructive">{r.Error}</span> : null}
                  {r.LogTail ? (
                    <details><summary className="cursor-pointer">Execution log</summary><pre className="whitespace-pre-wrap text-[10px] leading-tight text-text-secondary">{r.LogTail}</pre></details>
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

export function NodeConfigEditor({
  node,
  onClose,
}: {
  node: NodePatchState;
  onClose: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [cfg, setCfg] = useState<NodePatchConfig | null>(null);
  const [busy, setBusy] = useState(false);
  const [mode, setMode] = useState<NodePatchConfig['Mode']>('direct');
  const [proxyId, setProxyId] = useState('');
  const [proxies, setProxies] = useState<SquidProxy[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    let cancel = false;
    (async () => {
      try {
        const [data, available] = await Promise.all([
          client.getNodePatchConfig(node.NodeID), client.listSquidProxies(node.TenantID),
        ]);
        if (!cancel) {
          setCfg(data); setMode(data.Mode); setProxyId(data.ProxyID ?? '');
          setProxies(available.proxies.filter((proxy) => proxy.Status !== 'removed'));
        }
      } catch (error) {
        if (!cancel) setLoadError(error instanceof Error ? error.message : 'Could not load patch settings');
      }
    })();
    return () => {
      cancel = true;
    };
  }, [client, node.NodeID, node.TenantID]);

  const save = async () => {
    if (!cfg || loadError || (mode === 'proxy' && !proxyId)) return;
    setBusy(true);
    try {
      const updated = await client.upsertNodePatchConfig({
        node_id: node.NodeID, mode, proxy_id: proxyId || undefined, window_id: cfg.WindowID,
      });
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
            disabled={busy || !cfg || !!loadError}
            aria-pressed={mode === m}
            onClick={() => setMode(m)}
            className={`w-full rounded border p-3 text-left text-sm ${
              mode === m ? 'border-primary bg-surface-2' : 'border-border bg-surface-1'
            }`}
          >
            <div className="font-medium">{m}</div>
            <div className="mt-1 text-xs text-text-secondary">
              {m === 'direct' && 'Run apt/dnf/winget upgrade against the upstream repos directly.'}
              {m === 'proxy' && 'Route package-manager traffic through a managed Squid proxy.'}
              {m === 'airgapped' && 'Use a signed local content plan on a compatible Ubuntu/APT agent. Exact versions and explicit approval are required.'}
            </div>
          </button>
        ))}
        <label className="block pt-3 text-sm" htmlFor={`patch-proxy-${node.NodeID}`}>
          Repository proxy
        </label>
        <select id={`patch-proxy-${node.NodeID}`} className="w-full rounded border border-border bg-surface-1 p-2 text-sm"
          disabled={busy || !cfg || !!loadError} value={proxyId} onChange={(event) => setProxyId(event.target.value)}>
          <option value="">No proxy selected</option>
          {proxies.map((proxy) => <option key={proxy.ID} value={proxy.ID}>{proxy.Host}:{proxy.Port}</option>)}
        </select>
        {mode === 'proxy' && !proxyId && <p className="text-sm text-text-secondary">Select a repository proxy before saving proxy mode.</p>}
        {loadError && <p role="alert" className="text-sm text-red-400">Could not load patch settings: {loadError}</p>}
        <Button disabled={busy || !cfg || !!loadError || (mode === 'proxy' && !proxyId)} onClick={save}>
          {busy ? 'Saving…' : 'Save patch settings'}
        </Button>
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
  const [packageAllowlist, setPackageAllowlist] = useState('');
  const [offlineContent, setOfflineContent] = useState<OfflinePatchContent | null>(null);
 const [onlinePlan,setOnlinePlan]=useState<ExactOnlinePlan|null>(null);
 const [planNow,setPlanNow]=useState(Date.now);
 useEffect(()=>{const timer=setInterval(()=>setPlanNow(Date.now()),1000);return()=>clearInterval(timer);},[]);
  const [offlineError, setOfflineError] = useState<string | null>(null);
  const [offlineSource, setOfflineSource] = useState<'agent' | 'manual'>('agent');
  const [publishedNodeId, setPublishedNodeId] = useState('');
  const selectedPreparation = offlinePlanPreparation(nodes.find(node => node.id === publishedNodeId), offlineContent);
  const [planRefresh, setPlanRefresh] = useState(0);
  const preparationFinished = useCallback(() => {
    setOfflineContent(null);setOnlinePlan(null);setPublishedNodeId('');setPackageAllowlist('');setPlanRefresh(value=>value+1);
  }, []);
  const [postPatchRescan, setPostPatchRescan] = useState(true);
  const [loadingNodes, setLoadingNodes] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoadingNodes(true);
      setLoadError(null);
      try {
        const activeNodes: NodeSummary[] = [];
        let offset = 0;
        for (;;) {
          const resp = await client.listNodes({ tenantId, limit: 500, offset });
          if (cancelled) return;
          activeNodes.push(...(resp.data ?? []).filter((node) => node.state === 'active'));
          const nextOffset = resp.pagination?.nextOffset ?? null;
          if (nextOffset === null || nextOffset <= offset) break;
          offset = nextOffset;
        }
        setNodes(activeNodes);
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
  }, [client, tenantId, planRefresh]);

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
 setOnlinePlan(null);
    if (mode === 'airgapped' && offlineContent) {
      try { requireCurrentOfflinePlan(offlineContent); } catch (error) { setOfflineError((error as Error).message); return; }
    }
    if (mode === 'airgapped' && offlineSource === 'agent') {
      setPublishedNodeId(''); setOfflineContent(null); setPackageAllowlist('');
    }
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectAllVisible = () => {
 setOnlinePlan(null);
    setSelectedIds((prev) => {
      const next = new Set(prev);
      filteredNodes.forEach((n) => next.add(n.id));
      return next;
    });
  };

  const clearVisible = () => {
 setOnlinePlan(null);
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
    if(onlinePlan){
      try{requireCurrentOnlinePlan(onlinePlan);const node=nodes.find(n=>n.id===onlinePlan.node_id);const current=node&&currentOnlinePlan(node,onlinePlan.mode);if(ids.length!==1||ids[0]!==onlinePlan.node_id||mode!==onlinePlan.mode||!current||JSON.stringify(current)!==JSON.stringify(onlinePlan))throw new Error('Selected online plan no longer matches the current target, mode or agent report. Refresh and select again.');}
      catch(error){toast.error((error as Error).message);return;}
    }else if(mode!=='airgapped'&&nodes.some(n=>ids.includes(n.id)&&hasOnlinePlanner(n))){toast.error('Prepare and select an exact direct or proxy plan for this target before deployment.');return;}
    if (mode === 'airgapped' && (!offlineContent || ids.length !== 1)) {
      const message = 'Load an offline content plan and select exactly one target node.';
      setOfflineError(message);
      toast.error(message);
      return;
    }
    if (mode === 'airgapped' && offlineContent) {
      try { requireCurrentOfflinePlan(offlineContent); } catch (error) { setOfflineError((error as Error).message); return; }
    }
    if (mode === 'airgapped' && offlineSource === 'agent') {
      const node = nodes.find(n => n.id === publishedNodeId);
      const current = node && agentPublishedOfflinePlan(node);
      if (!current || ids[0] !== publishedNodeId || JSON.stringify(current) !== JSON.stringify(offlineContent)) {
        setOfflineError('The agent-published plan is no longer current for this target. Refresh plans and select it again.');
        return;
      }
    }
    setBusy(true);
    try {
      const resp = await client.createPatchDeployment({
        tenant_id: tenantId,
        node_ids: ids,
        mode,
        offline_content: mode === 'airgapped' ? offlineContent ?? undefined : undefined,
 online_plan: mode!=='airgapped'?onlinePlan??undefined:undefined,
        published_offline_plan_sha256: mode === 'airgapped' && offlineSource === 'agent' ? offlineContent?.manifest_sha256 : undefined,
        reason: reason.trim() || undefined,
        package_allowlist: [...new Set(packageAllowlist.split(/[\s,]+/).filter(Boolean))],
        post_patch_rescan: !!onlinePlan || mode === 'airgapped' || postPatchRescan,
      });
      const awaitingCount = resp.awaiting_approval?.length ?? 0;
      const blocked = resp.gate_blocked?.length ?? 0;
      const failedCount = resp.failed?.length ?? 0;
      let msg = `Patch request evaluated for ${ids.length} selected node${ids.length === 1 ? '' : 's'}`;
      if (awaitingCount > 0) msg += `; ${awaitingCount} awaiting approval`;
      if (blocked > 0) msg += `; ${blocked} blocked by safety gate`;
      if (failedCount > 0) msg += `; ${failedCount} failed to dispatch`;
      if (blocked > 0) msg += `: ${resp.gate_blocked?.slice(0, 3).map(row => row.reason).join('; ')}`;
      if (awaitingCount > 0 || blocked > 0 || failedCount > 0) {
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
    <aside className="fixed right-0 top-0 z-40 flex h-full w-full max-w-2xl flex-col border-l border-border bg-surface shadow-2xl">
      <div className="flex shrink-0 items-start justify-between border-b border-border p-6">
        <div>
          <h3 className="text-lg font-semibold">Deploy patches</h3>
          <p className="mt-1 text-xs text-text-secondary">
            Select active nodes for this deployment. Safety gates run before dispatch.
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="space-y-3 border-b border-border p-6">
        <div className="grid grid-cols-2 gap-3">
          <label className="block text-sm">
            <span className="text-text-secondary">Mode</span>
            <select
              value={mode}
              onChange={(e) => {setMode(e.target.value as typeof mode);setOnlinePlan(null);}}
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
        <label className="block text-sm">
          <span className="text-text-secondary">Package allowlist (optional)</span>
          <input
            type="text"
            value={packageAllowlist}
            disabled={mode === 'airgapped'||!!onlinePlan}
            onChange={(e) => setPackageAllowlist(e.target.value)}
            className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm"
            placeholder="e.g. perl-base"
          />
          <span className="mt-1 block text-xs text-text-secondary">
            Separate package names with commas or spaces. Empty means all eligible packages.
            Required dependencies may also change. Package selection is currently supported on Linux only.
          </span>
        </label>
        {(mode==='direct'||mode==='proxy')&&nodes.some(n=>hasOnlinePlanner(n,planNow))&&<OnlinePlanPreparation key={mode} nodes={nodes} mode={mode} onPrepared={preparationFinished} onSelected={plan=>{setOnlinePlan(plan);setPackageAllowlist(plan?.requested.join(', ')??'');if(plan){setSelectedIds(new Set([plan.node_id]));setPostPatchRescan(true);}}}/>}
        {onlinePlan&&<OnlinePlanReview plan={onlinePlan}/>}
        {mode==='auto'&&nodes.some(n=>selectedIds.has(n.id)&&hasOnlinePlanner(n,planNow))&&<p role="alert" className="text-xs text-warning">This target requires an exact online plan. Choose direct or proxy, prepare its plan and select it before deployment.</p>}
        {mode === 'airgapped' && <div className="space-y-2">
          <p className="text-xs text-text-secondary">Ubuntu 24.04 amd64 with a compatible agent only. The target must have signed content staged locally. Select its agent-published plan, or import a public plan for controlled offline transfer. Operator approval is always required.</p>
          <label className="block text-sm">Plan source
            <select aria-label="Plan source" value={offlineSource} className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm" onChange={event => {
              setOfflineSource(event.target.value as 'agent' | 'manual'); setPublishedNodeId(''); setOfflineContent(null); setOfflineError(null); setPackageAllowlist('');
            }}>
              <option value="agent">Agent-published plan</option>
              <option value="manual">Manual JSON import</option>
            </select>
          </label>
          {offlineSource === 'agent' ? <>
            <OfflinePlanPreparation nodes={nodes} onPrepared={preparationFinished}/>
            <label className="block text-sm">Available offline plan
              <select aria-label="Available offline plan" value={publishedNodeId} disabled={loadingNodes} className="mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm" onChange={event => {
                const node = nodes.find(n => n.id === event.target.value);
                const plan = node && agentPublishedOfflinePlan(node);
                setPublishedNodeId(plan ? node!.id : ''); setOfflineContent(plan ?? null); setOfflineError(null);
                setPackageAllowlist(plan?.requested.map(pkg => pkg.name).join(', ') ?? '');
                setSelectedIds(new Set(plan ? [node!.id] : []));
              }}>
                <option value="">Select a machine and its staged plan</option>
                {nodes.map(node => {
                  const plan = agentPublishedOfflinePlan(node);
                  const preparation = offlinePlanPreparation(node, plan);
                  return plan ? <option key={node.id} value={node.id}>{node.hostname} ({node.id.slice(0, 8)}) — {plan.requested.map(pkg => `${pkg.name} ${pkg.version}`).join(', ')}{preparation ? ` | Request ${preparation.jobId.slice(0,8)} | Completed ${new Date(preparation.completedAt).toLocaleString()}` : ' | Preparation details unavailable'}</option> : null;
                })}
              </select>
            </label>
            {publishedNodeId && <p className="text-xs text-text-secondary">{selectedPreparation ? <>Preparation request: <code>{selectedPreparation.jobId}</code>. Completed: {new Date(selectedPreparation.completedAt).toLocaleString()}. </> : 'Preparation details unavailable for this plan. Prepare a fresh plan to record its request and completion time. '}One latest validated plan is shown per machine; preparing the same content updates its preparation details without adding a duplicate option.</p>}
            <Button variant="outline" size="sm" disabled={loadingNodes} onClick={() => {
              setOfflineContent(null); setPublishedNodeId(''); setPackageAllowlist(''); setOfflineError(null); setPlanRefresh(value => value + 1);
            }}>Refresh plans</Button>
            <p className="text-xs text-text-secondary">The latest validated plan from each machine is shown for five minutes after its agent report. If none is available, stage or restage signed content on the target and wait for its next heartbeat. Selecting a plan selects its one target machine.</p>
          </> : <label className="block text-sm">Offline content-plan file
            <input type="file" accept=".json,application/json" className="mt-1 block w-full" onChange={async event => {
              const file = event.target.files?.[0];
              setOfflineContent(null); setOfflineError(null); setPackageAllowlist('');
              if (!file) return;
              try {
                if (file.size > 131072) throw new Error('The offline content plan is too large.');
                const plan = parseOfflinePatchContent(await file.text());
                setOfflineContent(plan); setPackageAllowlist(plan.requested.map(pkg => pkg.name).join(', '));
              } catch (error) { setOfflineError(errorMessage(error, 'The offline content plan could not be read.')); }
            }} />
          </label>}
          {offlineError && <p role="alert" className="text-xs text-destructive">{offlineError}</p>}
          {offlineContent && <OfflineContentReview content={offlineContent} />}
        </div>}
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={mode === 'airgapped' || postPatchRescan} disabled={mode === 'airgapped'||!!onlinePlan} onChange={(e) => setPostPatchRescan(e.target.checked)} />
          Refresh package inventory after patching
        </label>
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
          {selectedIds.size} of {nodes.length} active nodes selected
          {filter && ` · ${visibleSelectedCount} of ${filteredNodes.length} visible`}
        </p>
      </div>

      <div className="p-6">
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

      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border p-4">
        <Button variant="outline" size="sm" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button size="sm" onClick={submit} disabled={busy || loadingNodes || !!loadError || selectedIds.size === 0 || (mode!=='airgapped'&&!onlinePlan&&nodes.some(n=>selectedIds.has(n.id)&&hasOnlinePlanner(n,planNow))) || (!!onlinePlan&&Date.parse(onlinePlan.review.expires_at)<=planNow)}>
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
  totalCount,
  loading,
  loadError,
  onChanged,
}: {
  approvals: PatchApproval[];
  totalCount: number;
  loading: boolean;
  loadError?: string;
  onChanged: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [approvalNow,setApprovalNow]=useState(Date.now);
  useEffect(()=>{const timer=setInterval(()=>setApprovalNow(Date.now()),1000);return ()=>clearInterval(timer);},[]);
  const approvalDeadline=(a:PatchApproval)=>Math.min(Date.parse(a.expires_at)||0,a.offline_content?.format===3?Date.parse(a.offline_content.review?.expires_at??'')||0:Infinity,a.online_plan?Date.parse(a.online_plan.review.expires_at)||0:Infinity);
  const [busy, setBusy] = useState<string | null>(null);
  const [actionState, setActionState] = useState<Record<string, InlineActionState>>({});
  const [pendingDeny, setPendingDeny] = useState<PendingApprovalDeny | null>(null);

  const approve = async (a: PatchApproval) => {
    try {
      if (approvalDeadline(a)<=Date.now()) throw new Error('Approval or exact plan expired; prepare and request approval again.');
      if (a.offline_content) requireCurrentOfflinePlan(a.offline_content);
 if(a.online_plan)requireCurrentOnlinePlan(a.online_plan);
    } catch(error) {toast.error((error as Error).message);return;}
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
        title="No approvals waiting"
        description="No patch approvals waiting."
      />
    );
  }

  return (
    <>
      {totalCount > approvals.length ? (
        <p className="mb-3 text-xs text-text-secondary">
          Showing {approvals.length} of {totalCount} pending approvals.
        </p>
      ) : null}
      <div className="overflow-x-auto rounded border border-border">
      <table className="w-full text-sm">
        <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
          <tr>
            <th className="px-3 py-2">Requested</th>
            <th className="px-3 py-2">Deployment</th>
            <th className="px-3 py-2">Node</th>
            <th className="px-3 py-2">Mode</th>
            <th className="px-3 py-2">Expires</th>
            <th className="px-3 py-2">Package scope</th>
            <th className="px-3 py-2 text-right"></th>
          </tr>
        </thead>
        <tbody>
          {approvals.map((a) => {
            const deadline=approvalDeadline(a);
            const expired=deadline<=approvalNow;
            const denyState = actionState[approvalActionKey(a)];
            return (
              <Fragment key={a.id}><tr className="border-t border-border align-top">
                <td className="px-3 py-2 text-xs text-text-secondary">{new Date(a.created_at).toLocaleString()}</td>
                <td className="px-3 py-2 font-mono text-xs">{a.deployment_id.slice(0, 8)}</td>
                <td className="px-3 py-2 font-mono text-xs">{a.node_id.slice(0, 8)}</td>
                <td className="px-3 py-2 text-xs">{a.mode}</td>
                <td className="px-3 py-2 text-xs text-text-secondary">
                  {a.expires_at ? new Date(a.expires_at).toLocaleString() : '—'}
                  {expired && <span className="ml-1 text-destructive">(expired)</span>}
                </td>
                <td className="px-3 py-2 text-xs">
                  {a.package_scope_available
                    ? <><div>{a.package_allowlist?.length ? a.package_allowlist.join(', ') : 'All eligible packages'}</div>
                        {!!a.package_denylist?.length && <div>Excluded: {a.package_denylist.join(', ')}</div>}
                        {a.online_plan?<div className="text-text-secondary">Exact acquired online plan shown below.</div>:a.offline_content ? <div className="text-text-secondary">Exact signed local plan shown below.</div> : <div className="text-text-secondary">Required dependencies may also change.</div>}</>
                    : <span className="text-text-secondary">Package scope unavailable</span>}
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
              {a.online_plan&&<tr><td colSpan={7} className="px-3 pb-4"><OnlinePlanReview plan={a.online_plan}/></td></tr>}
              {a.offline_content && <tr><td colSpan={7} className="px-3 pb-4"><p className="mb-2 text-xs text-text-secondary">Plan source: {a.published_offline_plan_sha256 ? 'Agent-published plan' : 'Manual JSON import'}</p><OfflineContentReview content={a.offline_content} /></td></tr>}
              </Fragment>
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
    case 'posture':
      return 'Posture';
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
