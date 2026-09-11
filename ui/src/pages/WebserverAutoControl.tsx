import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowRight, RefreshCw, RotateCcw, ShieldAlert, ShieldCheck, Server, Settings2 } from 'lucide-react';
import { ConfirmModal } from '@/components/ConfirmModal';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { Alert, EmptyState, KpiTile, Panel, SectionHeader, StatusTag, type StateTone } from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import type { WebserverConfigActionHistory, WebserverConfigReceipt, WebserverInstance } from '../lib/api';

type WebserverActionMode = 'plan' | 'capture' | 'enforce' | 'rollback';

interface InlineActionState {
  busy?: boolean;
  message?: string;
  tone?: StateTone;
}

interface PendingWebserverAction {
  mode: Exclude<WebserverActionMode, 'plan'>;
  instance: WebserverInstance;
  error?: string;
}

export function WebserverAutoControl(): JSX.Element {
  const api = useApiClient();
  const { currentTenantId, currentTenant } = useTenant();
  const [instances, setInstances] = useState<WebserverInstance[]>([]);
  const [selectedId, setSelectedId] = useState('');
  const [actions, setActions] = useState<WebserverConfigActionHistory[]>([]);
  const [receipts, setReceipts] = useState<WebserverConfigReceipt[]>([]);
  const [loading, setLoading] = useState(true);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [actionState, setActionState] = useState<Record<string, InlineActionState>>({});
  const [pendingAction, setPendingAction] = useState<PendingWebserverAction | null>(null);

  const selected = useMemo(
    () => instances.find((instance) => instance.ID === selectedId) ?? instances[0] ?? null,
    [instances, selectedId],
  );

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const response = await api.listWebserverInstances({ tenantId: currentTenantId, limit: 200 });
      const rows = response.data ?? [];
      setInstances(rows);
      setSelectedId((current) => current || rows[0]?.ID || '');
      setError(null);
    } catch (err) {
      setInstances([]);
      setSelectedId('');
      setError(errorMessage(err, 'Webserver inventory unavailable'));
    } finally {
      setLoading(false);
    }
  }, [api, currentTenantId]);

  const refreshHistory = useCallback(async (instance: WebserverInstance | null) => {
    if (!currentTenantId || !instance?.ID) {
      setActions([]);
      setReceipts([]);
      return;
    }
    setHistoryLoading(true);
    try {
      const [actionResp, receiptResp] = await Promise.all([
        api.listWebserverConfigActions({ tenantId: currentTenantId, instanceId: instance.ID, limit: 20 }),
        api.listWebserverConfigReceipts({ tenantId: currentTenantId, instanceId: instance.ID, limit: 20 }),
      ]);
      setActions(actionResp.data ?? []);
      setReceipts(receiptResp.data ?? []);
      setHistoryError(null);
    } catch (err) {
      setActions([]);
      setReceipts([]);
      setHistoryError(errorMessage(err, 'History unavailable'));
    } finally {
      setHistoryLoading(false);
    }
  }, [api, currentTenantId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    void refreshHistory(selected);
  }, [refreshHistory, selected]);

  const totals = useMemo(() => {
    const captureReady = instances.filter((instance) => webserverCaptureReady(instance)).length;
    const enforceReady = instances.filter((instance) => webserverEnforceReady(instance)).length;
    const gaps = instances.length - Math.min(captureReady, enforceReady);
    return { captureReady, enforceReady, gaps };
  }, [instances]);

  const inventoryUnavailable = Boolean(error) && !loading;
  const historyUnavailable = Boolean(historyError) && !historyLoading;

  const runAction = async (instance: WebserverInstance, mode: WebserverActionMode) => {
    if (!currentTenantId) return;
    const key = webserverActionKey(instance, mode);
    const label = webserverTargetLabel(instance);
    const title = actionTitle(mode);
    setStatus(`${title} queued for ${label}...`);
    setActionState((prev) => ({
      ...prev,
      [key]: { busy: true, message: `${title} queued for ${label}...`, tone: 'warning' },
    }));
    const payload = {
      tenant_id: currentTenantId,
      node_id: instance.NodeID,
      policy: {
        mode,
        requested_from: 'webserver_auto_control',
        approval_required: mode !== 'plan',
      },
    };
    try {
      const response =
        mode === 'plan'
          ? await api.planWebserverConfig(instance.ID, payload)
          : mode === 'rollback'
            ? await api.rollbackWebserverConfig(instance.ID, payload)
            : await api.applyWebserverConfig(instance.ID, payload);
      const message = `${title} ${response.status || 'queued'} for ${label} (${response.job_id.slice(0, 8)})`;
      setStatus(message);
      setActionState((prev) => ({
        ...prev,
        [key]: { busy: false, message, tone: statusTone(response.status || 'queued') },
      }));
      setPendingAction(null);
      await refreshHistory(instance);
    } catch (err) {
      const message = `${title} failed for ${label}: ${errorMessage(err, 'Action failed')}`;
      setStatus(message);
      setActionState((prev) => ({
        ...prev,
        [key]: { busy: false, message, tone: 'critical' },
      }));
      setPendingAction((current) => (
        current && current.instance.ID === instance.ID && current.mode === mode ? { ...current, error: message } : current
      ));
    }
  };

  const queueAction = (mode: WebserverActionMode) => {
    if (!selected) return;
    if (mode === 'plan') {
      void runAction(selected, mode);
      return;
    }
    setPendingAction({ mode, instance: selected });
  };

  const confirmPendingAction = () => {
    if (!pendingAction) return;
    void runAction(pendingAction.instance, pendingAction.mode);
  };

  const cancelPendingAction = () => setPendingAction(null);

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant to view webserver auto-control." />;
  }

  const pendingKey = pendingAction ? webserverActionKey(pendingAction.instance, pendingAction.mode) : '';
  const pendingBusy = pendingKey ? Boolean(actionState[pendingKey]?.busy) : false;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="WEBSERVER AUTO-CONTROL"
        title="Capture and enforcement"
        description={
          inventoryUnavailable
            ? `${currentTenant?.name ? `${currentTenant.name}: ` : ''}webserver inventory unavailable.`
            : `${currentTenant?.name ? `${currentTenant.name}: ` : ''}${instances.length} detected, ${totals.captureReady} capture ready, ${totals.enforceReady} enforcement ready.`
        }
        actions={
          <Button type="button" variant="outline" size="sm" onClick={() => void refresh()} loading={loading}>
            <RefreshCw />
            Refresh
          </Button>
        }
      />

      {error ? (
        <Alert
          variant="critical"
          title="Webserver inventory unavailable"
          actions={
            <Button type="button" variant="outline" size="sm" onClick={() => void refresh()} disabled={loading}>
              Retry
            </Button>
          }
        >
          {error}
        </Alert>
      ) : null}

      <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
        <KpiTile label="Detected" value={inventoryUnavailable ? 'N/A' : instances.length} loading={loading} icon={<Server />} />
        <KpiTile label="Capture ready" value={inventoryUnavailable ? 'N/A' : `${totals.captureReady}/${instances.length}`} tone={totals.captureReady === instances.length ? 'healthy' : 'warning'} loading={loading} icon={<ShieldCheck />} />
        <KpiTile label="Enforcement ready" value={inventoryUnavailable ? 'N/A' : `${totals.enforceReady}/${instances.length}`} tone={totals.enforceReady === instances.length ? 'healthy' : 'warning'} loading={loading} icon={<ShieldAlert />} />
        <KpiTile label="Config gaps" value={inventoryUnavailable ? 'N/A' : totals.gaps} tone={totals.gaps > 0 || inventoryUnavailable ? 'warning' : 'healthy'} loading={loading} icon={<Settings2 />} />
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(320px,420px)_1fr]">
        <Panel eyebrow="INVENTORY" title="Detected webservers" toneAccent={totals.gaps > 0 ? 'warning' : 'healthy'}>
          {loading ? (
            <Skeleton className="h-80 rounded-lg" />
          ) : inventoryUnavailable ? (
            <EmptyState icon={<Server />} title="Webserver inventory unavailable" description="Detected webservers could not be loaded for the selected tenant." />
          ) : instances.length === 0 ? (
            <EmptyState icon={<Server />} title="No webserver inventory" description="No nginx, Apache, lighttpd, Tomcat, or edge proxy instances reported." />
          ) : (
            <div className="flex max-h-[32rem] flex-col gap-2 overflow-y-auto pr-1">
              {instances.map((instance) => (
                <button
                  key={instance.ID}
                  type="button"
                  onClick={() => setSelectedId(instance.ID)}
                  className={`rounded-lg border p-3 text-left transition ${
                    selected?.ID === instance.ID ? 'border-brand-500 bg-brand-500/10' : 'border-border-subtle bg-surface hover:bg-hover'
                  }`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium text-foreground">{instance.Kind} {instance.ServiceName || 'default'}</p>
                      <p className="truncate font-mono text-xs text-text-muted">{instance.ConfigPath || instance.AccessLogPath || instance.NodeID}</p>
                    </div>
                    <StatusTag tone={webserverCaptureReady(instance) && webserverEnforceReady(instance) ? 'healthy' : 'warning'}>
                      {webserverCaptureReady(instance) && webserverEnforceReady(instance) ? 'ready' : 'gap'}
                    </StatusTag>
                  </div>
                  <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-text-secondary">
                    <span>{instance.Version || 'version unknown'}</span>
                    <span>{instance.VHosts?.length ?? 0} vhosts</span>
                    <span>{formatDateTime(instance.ObservedAt)}</span>
                  </div>
                </button>
              ))}
            </div>
          )}
        </Panel>

        <div className="flex flex-col gap-4">
          <Panel eyebrow="SELECTED INSTANCE" title={selected ? `${selected.Kind} ${selected.ServiceName || 'default'}` : 'No instance selected'} toneAccent={selected && (!webserverCaptureReady(selected) || !webserverEnforceReady(selected)) ? 'warning' : 'brand'}>
            {selected ? (
              <div className="flex flex-col gap-4">
                <div className="grid gap-4 lg:grid-cols-[1fr_18rem]">
                  <div className="grid grid-cols-1 gap-2 text-sm md:grid-cols-2">
                    <Fact label="Config" value={selected.ConfigPath || 'unknown'} mono />
                    <Fact label="Access log" value={selected.AccessLogPath || 'not reported'} mono />
                    <Fact label="Error log" value={selected.ErrorLogPath || 'not reported'} mono />
                    <Fact label="Node" value={selected.NodeID} mono />
                    <Fact label="Purpose" value={compactList(instanceServerPurposes(selected), 3) || purposeFromKind(selected.Kind)} />
                    <Fact label="App roots" value={String(selected.VHosts?.length ?? 0)} tone={(selected.VHosts?.length ?? 0) > 0 ? 'info' : 'unknown'} />
                    <Fact label="Capture" value={webserverCaptureReady(selected) ? 'ready' : 'gap'} tone={webserverCaptureReady(selected) ? 'healthy' : 'warning'} />
                    <Fact label="Enforcement" value={webserverEnforceReady(selected) ? 'ready' : 'gap'} tone={webserverEnforceReady(selected) ? 'healthy' : 'warning'} />
                    <Fact label="Response headers" value={capabilityBool(selected.Capabilities, 'response_header_capture') ? 'captured' : 'not reported'} tone={capabilityBool(selected.Capabilities, 'response_header_capture') ? 'healthy' : 'unknown'} />
                    <Fact label="Drift" value={capabilityBool(selected.Capabilities, 'drift_detected') ? 'detected' : 'not reported'} tone={capabilityBool(selected.Capabilities, 'drift_detected') ? 'warning' : 'unknown'} />
                  </div>
                  <div className="flex flex-col gap-2">
                    {(['plan', 'capture', 'enforce', 'rollback'] as const).map((mode) => {
                      const key = webserverActionKey(selected, mode);
                      const label = `${actionTitle(mode)} for ${webserverTargetLabel(selected)}`;
                      return (
                        <Button
                          key={mode}
                          type="button"
                          variant={mode === 'plan' ? 'outline' : 'ghost'}
                          size="sm"
                          onClick={() => queueAction(mode)}
                          loading={actionState[key]?.busy}
                          disabled={actionState[key]?.busy}
                          aria-label={label}
                        >
                          {mode === 'rollback' ? <RotateCcw /> : null}
                          {actionTitle(mode)}
                        </Button>
                      );
                    })}
                  </div>
                </div>
                <ApplicationContext instance={selected} />
              </div>
            ) : (
              <EmptyState title="No instance selected" description="Select a webserver instance from inventory." />
            )}
            {status ? <p className="mt-3 text-sm text-text-secondary">{status}</p> : null}
            {selected ? (
              <div className="mt-3 flex flex-col gap-2">
                {(['plan', 'capture', 'enforce', 'rollback'] as const).map((mode) => {
                  const state = actionState[webserverActionKey(selected, mode)];
                  return state?.message ? (
                    <p key={mode} className={`text-sm ${toneText(state.tone ?? 'unknown')}`}>
                      {actionTitle(mode)}: {state.message}
                    </p>
                  ) : null;
                })}
              </div>
            ) : null}
          </Panel>

          {historyError ? (
            <Alert
              variant="warning"
              title="Webserver action history unavailable"
              actions={
                <Button type="button" variant="outline" size="sm" onClick={() => void refreshHistory(selected)} disabled={historyLoading}>
                  Retry
                </Button>
              }
            >
              {historyError}
            </Alert>
          ) : null}

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <Panel eyebrow="ACTIONS" title="Config action history" toneAccent={actions.some((action) => action.status === 'failed') ? 'critical' : 'brand'}>
              {historyLoading ? <Skeleton className="h-56 rounded-lg" /> : <ActionHistory rows={actions} unavailable={historyUnavailable} />}
            </Panel>
            <Panel eyebrow="RECEIPTS" title="Validation and rollback receipts" toneAccent={receipts.some((receipt) => receipt.validation_status === 'failed' || receipt.reload_status === 'failed') ? 'critical' : 'brand'}>
              {historyLoading ? <Skeleton className="h-56 rounded-lg" /> : <ReceiptHistory rows={receipts} unavailable={historyUnavailable} />}
            </Panel>
          </div>
        </div>
      </div>

      <Button asChild variant="ghost" size="sm" className="w-fit">
        <Link to="/control-room">
          Control Room
          <ArrowRight />
        </Link>
      </Button>

      <ConfirmModal
        open={Boolean(pendingAction)}
        title={`${pendingAction ? actionTitle(pendingAction.mode) : 'Apply'} webserver control?`}
        body={
          pendingAction
            ? `${actionTitle(pendingAction.mode)} for ${webserverTargetLabel(pendingAction.instance)}. This queues an agent-managed webserver configuration change and keeps rollback evidence attached.`
            : undefined
        }
        confirmLabel={pendingAction ? actionTitle(pendingAction.mode) : 'Confirm'}
        cancelLabel="Cancel"
        confirmDisabled={pendingBusy}
        cancelDisabled={pendingBusy}
        variant={pendingAction?.mode === 'rollback' ? 'danger' : 'default'}
        onConfirm={confirmPendingAction}
        onCancel={cancelPendingAction}
      >
        {pendingAction?.error ? (
          <Alert variant="critical" title="Webserver action failed">
            {pendingAction.error}
          </Alert>
        ) : null}
      </ConfirmModal>
    </div>
  );
}

function Fact({ label, value, mono, tone }: { label: string; value: string; mono?: boolean; tone?: StateTone }) {
  return (
    <div className="rounded-lg border border-border-subtle bg-surface p-3">
      <p className="text-xs text-text-muted">{label}</p>
      <p className={`${mono ? 'font-mono text-xs' : 'text-sm'} mt-1 break-all font-medium ${tone ? toneText(tone) : 'text-foreground'}`}>{value}</p>
    </div>
  );
}

function ApplicationContext({ instance }: { instance: WebserverInstance }) {
  const roots = applicationRootRows(instance);
  const backends = instanceBackends(instance);
  if (roots.length === 0 && backends.length === 0) {
    return (
      <EmptyState
        title="No application roots reported"
        description="Control One has not traced an app root from this webserver config yet."
      />
    );
  }
  return (
    <div className="grid gap-3 xl:grid-cols-2">
      <div className="overflow-x-auto rounded-lg border border-border-subtle">
        <table className="w-full text-sm">
          <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
            <tr>
              <th className="px-3 py-2">App/vhost</th>
              <th className="px-3 py-2">Root</th>
              <th className="px-3 py-2">Skill</th>
            </tr>
          </thead>
          <tbody>
            {roots.length === 0 ? (
              <tr>
                <td className="px-3 py-3 text-text-secondary" colSpan={3}>No app roots reported.</td>
              </tr>
            ) : roots.map((root) => (
              <tr key={`${root.name}:${root.path}`} className="border-t border-border-subtle">
                <td className="px-3 py-2">
                  <p className="font-medium text-foreground">{root.name}</p>
                  <p className="text-xs text-text-muted">
                    {root.type}{root.confidence ? ` - ${root.confidence}% confidence` : ''}
                  </p>
                </td>
                <td className="max-w-[20rem] px-3 py-2 text-xs text-text-secondary">
                  <p className="truncate font-mono">{root.path || 'not reported'}</p>
                  <p className="truncate">{compactList(root.evidence, 2) || 'evidence not reported'}</p>
                </td>
                <td className="px-3 py-2">
                  <StatusTag tone={root.status === 'skill_required' ? 'warning' : root.status ? 'healthy' : 'unknown'}>
                    {root.skill || root.status || 'unknown'}
                  </StatusTag>
                  {root.remediationSkill && <p className="mt-1 text-xs text-text-muted">{root.remediationSkill}</p>}
                  {root.catalogVersion && <p className="mt-1 text-xs text-text-muted">{root.catalogVersion}</p>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="overflow-x-auto rounded-lg border border-border-subtle">
        <table className="w-full text-sm">
          <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
            <tr>
              <th className="px-3 py-2">Backend</th>
              <th className="px-3 py-2">Servers</th>
            </tr>
          </thead>
          <tbody>
            {backends.length === 0 ? (
              <tr>
                <td className="px-3 py-3 text-text-secondary" colSpan={2}>No load-balancer backends reported.</td>
              </tr>
            ) : backends.map((backend) => (
              <tr key={backend.name} className="border-t border-border-subtle">
                <td className="px-3 py-2 font-medium text-foreground">{backend.name}</td>
                <td className="px-3 py-2 text-text-secondary">{compactList(backend.servers, 4) || 'not reported'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ActionHistory({ rows, unavailable }: { rows: WebserverConfigActionHistory[]; unavailable?: boolean }) {
  if (unavailable) return <EmptyState title="Config action history unavailable" description="Action history could not be loaded for this webserver instance." />;
  if (rows.length === 0) return <EmptyState title="No config actions" description="No plan, apply, enforcement, or rollback actions recorded for this instance." />;
  return (
    <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle">
      {rows.map((row) => (
        <div key={row.id} className="p-3">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-foreground">{actionTitleFromJob(row.action)}</p>
              <p className="mt-1 text-xs text-text-secondary">{formatDateTime(row.updated_at)}</p>
            </div>
            <StatusTag tone={statusTone(row.status)}>{row.status}</StatusTag>
          </div>
          {row.error_message ? <p className="mt-2 text-xs text-state-critical">{row.error_message}</p> : null}
          {actionDetail(row) ? <p className="mt-2 text-xs text-text-secondary">{actionDetail(row)}</p> : null}
        </div>
      ))}
    </div>
  );
}

function ReceiptHistory({ rows, unavailable }: { rows: WebserverConfigReceipt[]; unavailable?: boolean }) {
  if (unavailable) return <EmptyState title="Receipts unavailable" description="Validation and rollback receipts could not be loaded for this webserver instance." />;
  if (rows.length === 0) return <EmptyState title="No receipts" description="No validation, reload, or rollback receipts recorded for this instance." />;
  return (
    <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle">
      {rows.map((row) => (
        <div key={row.id} className="p-3">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-foreground">{actionTitleFromJob(row.action)}</p>
              <p className="mt-1 text-xs text-text-secondary">{formatDateTime(row.created_at)}</p>
            </div>
            <StatusTag tone={receiptTone(row)}>{row.validation_status || row.reload_status || 'receipt'}</StatusTag>
          </div>
          <div className="mt-2 grid grid-cols-2 gap-2 text-xs text-text-secondary">
            <span>validation {row.validation_status || 'unknown'}</span>
            <span>reload {row.reload_status || 'unknown'}</span>
            <span className="col-span-2 truncate">rollback {row.rollback_ref || 'not reported'}</span>
            <span className="col-span-2 truncate">drift {receiptDrift(row)}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

function webserverCaptureReady(instance: WebserverInstance): boolean {
  return capabilityAnyBool(instance.Capabilities, 'capture_supported', 'capture') || Boolean(instance.AccessLogPath);
}

function webserverEnforceReady(instance: WebserverInstance): boolean {
  return capabilityAnyBool(instance.Capabilities, 'enforce_supported', 'enforce', 'blocklist_supported');
}

function capabilityBool(caps: Record<string, unknown> | undefined, key: string): boolean {
  const value = caps?.[key];
  return value === true || value === 'true' || value === '1';
}

function capabilityAnyBool(caps: Record<string, unknown> | undefined, ...keys: string[]): boolean {
  return keys.some((key) => capabilityBool(caps, key));
}

interface ApplicationRootRow {
  name: string;
  type: string;
  path: string;
  status: string;
  skill: string;
  remediationSkill: string;
  confidence: string;
  catalogVersion: string;
  evidence: string[];
}

interface BackendRow {
  name: string;
  servers: string[];
}

function applicationRootRows(instance: WebserverInstance): ApplicationRootRow[] {
  return (instance.VHosts ?? []).map((vhost, index) => ({
    name: stringField(vhost, 'name', 'vhost', 'server_name') || `app ${index + 1}`,
    type: stringField(vhost, 'application_name', 'application_type', 'app_type') || 'unknown',
    path: stringField(vhost, 'document_root', 'path', 'root'),
    status: stringField(vhost, 'coverage_state', 'log_skill_status'),
    skill: stringField(vhost, 'parser_profile_id', 'suggested_skill'),
    remediationSkill: stringField(vhost, 'remediation_skill_id'),
    confidence: stringField(vhost, 'confidence'),
    catalogVersion: stringField(vhost, 'catalog_version'),
    evidence: listField(vhost, 'evidence'),
  }));
}

function instanceServerPurposes(instance: WebserverInstance): string[] {
  return listField(instance.Capabilities, 'server_purposes');
}

function instanceBackends(instance: WebserverInstance): BackendRow[] {
  const raw = instance.Capabilities?.['load_balancer_backends'];
  if (!Array.isArray(raw)) return [];
  return raw
    .map((item): BackendRow | null => {
      if (!item || typeof item !== 'object') return null;
      const row = item as Record<string, unknown>;
      const name = stringField(row, 'backend', 'name');
      if (!name) return null;
      return { name, servers: listField(row, 'servers') };
    })
    .filter((row): row is BackendRow => row !== null);
}

function stringField(row: Record<string, unknown> | undefined, ...keys: string[]): string {
  if (!row) return '';
  for (const key of keys) {
    const value = row[key];
    if (typeof value === 'string') return value.trim();
    if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  }
  return '';
}

function listField(row: Record<string, unknown> | undefined, key: string): string[] {
  const value = row?.[key];
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean);
  if (typeof value === 'string') return value.split(',').map((item) => item.trim()).filter(Boolean);
  return [];
}

function purposeFromKind(kind: string): string {
  const normalized = kind.toLowerCase();
  if (normalized === 'haproxy') return 'load_balancer';
  if (normalized === 'tomcat') return 'app_node';
  return 'web_server';
}

function compactList(values: string[], limit: number): string {
  return values.filter(Boolean).slice(0, limit).join(', ');
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

function webserverTargetLabel(instance: WebserverInstance): string {
  return `${instance.Kind} ${instance.ServiceName || instance.ID}`;
}

function webserverActionKey(instance: WebserverInstance, mode: WebserverActionMode): string {
  return `${instance.ID}:${mode}`;
}

function actionTitle(mode: 'plan' | 'capture' | 'enforce' | 'rollback'): string {
  switch (mode) {
    case 'plan':
      return 'Plan';
    case 'capture':
      return 'Apply capture';
    case 'enforce':
      return 'Apply enforcement';
    case 'rollback':
      return 'Rollback';
  }
}

function actionTitleFromJob(action?: string): string {
  return (action || 'webserver action').replace(/^webserver\./, '').replace(/_/g, ' ');
}

function actionDetail(row: WebserverConfigActionHistory): string {
  const parts: string[] = [];
  if (row.policy?.approval_required === true) parts.push('approval required');
  if (row.result?.validation_status) parts.push(`validation ${String(row.result.validation_status)}`);
  if (row.result?.reload_status) parts.push(`reload ${String(row.result.reload_status)}`);
  if (row.result?.drift_detected === true) parts.push('drift detected');
  if (row.result?.drift_detected === false) parts.push('no drift');
  return parts.join(', ');
}

function statusTone(status?: string): StateTone {
  switch ((status || '').toLowerCase()) {
    case 'succeeded':
    case 'success':
    case 'completed':
    case 'active':
      return 'healthy';
    case 'failed':
    case 'error':
      return 'critical';
    case 'pending':
    case 'queued':
    case 'running':
      return 'warning';
    default:
      return 'unknown';
  }
}

function receiptTone(row: WebserverConfigReceipt): StateTone {
  if (row.validation_status === 'failed' || row.reload_status === 'failed') return 'critical';
  if (row.validation_status === 'ok' && row.reload_status === 'ok') return 'healthy';
  return 'unknown';
}

function receiptDrift(row: WebserverConfigReceipt): string {
  if (row.metadata?.drift_detected === true) return 'detected';
  if (row.metadata?.drift_detected === false) return 'not detected';
  if (row.metadata?.drift) return String(row.metadata.drift);
  return 'not reported';
}

function toneText(tone: StateTone): string {
  if (tone === 'critical') return 'text-state-critical';
  if (tone === 'warning') return 'text-state-warning';
  if (tone === 'healthy') return 'text-state-healthy';
  return 'text-foreground';
}

function formatDateTime(value?: string): string {
  if (!value) return 'unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date);
}

export default WebserverAutoControl;
