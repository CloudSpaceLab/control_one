import { FormEvent, useEffect, useState } from 'react';
import { useWebhooks } from '../hooks/useWebhooks';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { Webhook, CreateWebhookPayload, UpdateWebhookPayload, MFAFactor } from '../lib/api';
import { ConfirmModal } from '../components/ConfirmModal';
import { Panel, SectionHeader, StatusTag, EmptyState, SelectField, KpiTile } from '../components/kit';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../components/ui/tabs';
import { KeyRound, Shield, Trash2 } from 'lucide-react';

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

const AVAILABLE_EVENTS = [
  'job.created',
  'job.succeeded',
  'job.failed',
  'compliance.scan.completed',
  'compliance.violation.detected',
  'node.registered',
  'node.updated',
  'policy.created',
  'policy.updated',
  'tenant.created',
  'tenant.updated',
];

export function Settings(): JSX.Element {
  const api = useApiClient();
  const [activeTab, setActiveTab] = useState<'webhooks' | 'security' | 'system' | 'integrations'>('webhooks');

  // MFA state
  const [mfaFactors, setMfaFactors] = useState<MFAFactor[]>([]);
  const [mfaLoading, setMfaLoading] = useState(false);
  const [mfaReloadToken, setMfaReloadToken] = useState(0);
  const [deleteMfaId, setDeleteMfaId] = useState<string | null>(null);

  // Worker pool
  const { status: workerStatus, loading: workerLoading, refresh: refreshWorker } = useWorkerStatus({ pollIntervalMs: 0 });

  useEffect(() => {
    let cancelled = false;
    setMfaLoading(true);
    api
      .listMFAFactors()
      .then((r) => { if (!cancelled) setMfaFactors(r.factors ?? []); })
      .catch(() => { if (!cancelled) setMfaFactors([]); })
      .finally(() => { if (!cancelled) setMfaLoading(false); });
    return () => { cancelled = true; };
  }, [api, mfaReloadToken]);

  const handleDeleteMFA = async () => {
    if (!deleteMfaId) return;
    await api.deleteMFAFactor(deleteMfaId);
    setDeleteMfaId(null);
    setMfaReloadToken((n) => n + 1);
  };
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [isCreatingWebhook, setIsCreatingWebhook] = useState(false);
  const [editingWebhook, setEditingWebhook] = useState<Webhook | null>(null);

  const { data: tenants } = useTenants();
  const {
    data: webhooks,
    loading: webhooksLoading,
    error: webhooksError,
    reload: reloadWebhooks,
  } = useWebhooks({
    tenant_id: selectedTenant,
    limit: 100,
  });

  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [saving, setSaving] = useState(false);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  const [webhookForm, setWebhookForm] = useState<CreateWebhookPayload>({
    name: '',
    url: '',
    events: [],
    enabled: true,
    verify_ssl: true,
    timeout_seconds: 30,
    retry_count: 3,
  });

  const handleCreateWebhook = () => {
    setIsCreatingWebhook(true);
    setEditingWebhook(null);
    setWebhookForm({
      name: '',
      url: '',
      events: [],
      enabled: true,
      verify_ssl: true,
      timeout_seconds: 30,
      retry_count: 3,
    });
    resetFeedback();
  };

  const handleEditWebhook = (webhook: Webhook) => {
    setEditingWebhook(webhook);
    setIsCreatingWebhook(false);
    setWebhookForm({
      name: webhook.name,
      url: webhook.url,
      events: [...webhook.events],
      enabled: webhook.enabled,
      verify_ssl: webhook.verify_ssl,
      timeout_seconds: webhook.timeout_seconds,
      retry_count: webhook.retry_count,
    });
    resetFeedback();
  };

  const handleCancelEdit = () => {
    setIsCreatingWebhook(false);
    setEditingWebhook(null);
    resetFeedback();
  };

  const handleEventToggle = (event: string) => {
    setWebhookForm((prev) => {
      const events = prev.events || [];
      if (events.includes(event)) {
        return { ...prev, events: events.filter((e) => e !== event) };
      }
      return { ...prev, events: [...events, event] };
    });
  };

  const handleSaveWebhook = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!webhookForm.name || !webhookForm.url || !webhookForm.events || webhookForm.events.length === 0) {
      showError('Name, URL, and at least one event are required');
      return;
    }

    setSaving(true);
    resetFeedback();

    try {
      if (editingWebhook) {
        const payload: UpdateWebhookPayload = {
          name: webhookForm.name,
          url: webhookForm.url,
          events: webhookForm.events,
          enabled: webhookForm.enabled,
          verify_ssl: webhookForm.verify_ssl,
          timeout_seconds: webhookForm.timeout_seconds,
          retry_count: webhookForm.retry_count,
        };
        await api.updateWebhook(editingWebhook.id, payload);
        showSuccess('Webhook updated successfully');
      } else {
        const payload: CreateWebhookPayload = {
          ...webhookForm,
          tenant_id: selectedTenant,
        };
        await api.createWebhook(payload);
        showSuccess('Webhook created successfully');
      }
      setIsCreatingWebhook(false);
      setEditingWebhook(null);
      reloadWebhooks();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to save webhook';
      showError(message);
      showToast(message, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteWebhook = async (webhookId: string) => {
    try {
      await api.deleteWebhook(webhookId);
      showToast('Webhook deleted successfully', 'success');
      reloadWebhooks();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to delete webhook';
      showToast(message, 'error');
    }
  };

  const handleTestWebhook = async (webhookId: string) => {
    try {
      const result = await api.testWebhook(webhookId, {
        event_type: 'test',
        payload: { message: 'Test webhook delivery' },
      });
      if (result.success) {
        showToast('Webhook test successful', 'success');
      } else {
        showToast(`Webhook test failed: ${result.error || 'Unknown error'}`, 'error');
      }
    } catch (error: unknown) {
      showToast(error instanceof Error ? error.message : 'Failed to test webhook', 'error');
    }
  };

  const showForm = isCreatingWebhook || editingWebhook !== null;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="SETTINGS"
        title="Settings"
        description="Webhook endpoints and platform integrations."
      />

      <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as 'webhooks' | 'security' | 'system' | 'integrations')}>
        <TabsList>
          <TabsTrigger value="webhooks">Webhooks</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="system">System health</TabsTrigger>
          <TabsTrigger value="integrations">Integrations</TabsTrigger>
        </TabsList>

        <TabsContent value="webhooks" className="mt-4 flex flex-col gap-4">
          {/* Tenant filter */}
          <div className="flex items-center gap-3">
            <SelectField
              id="tenant-filter"
              label="Filter by tenant"
              value={selectedTenant ?? ''}
              onChange={(e) => setSelectedTenant(e.target.value || undefined)}
              wrapperClassName="w-64"
            >
              <option value="">All tenants</option>
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>{t.name}</option>
              ))}
            </SelectField>
          </div>

          {webhooksError && (
            <p className="text-sm text-state-critical">Error loading webhooks: {webhooksError}</p>
          )}

          {/* New/Edit webhook form */}
          {showForm && (
            <Panel
              padding="md"
              eyebrow={editingWebhook ? 'EDIT WEBHOOK' : 'NEW WEBHOOK'}
              title={editingWebhook ? 'Edit endpoint' : 'Add endpoint'}
              toneAccent="brand"
            >
              <form onSubmit={handleSaveWebhook} className="flex flex-col gap-4">
                {formError && (
                  <p className="text-sm text-state-critical">{formError}</p>
                )}
                {formSuccess && (
                  <p className="text-sm text-state-healthy">{formSuccess}</p>
                )}

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="wh-name">Name</Label>
                    <Input
                      id="wh-name"
                      value={webhookForm.name}
                      onChange={(e) => setWebhookForm((f) => ({ ...f, name: e.target.value }))}
                      placeholder="e.g. PagerDuty alerts"
                      required
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="wh-url">URL</Label>
                    <Input
                      id="wh-url"
                      type="url"
                      value={webhookForm.url}
                      onChange={(e) => setWebhookForm((f) => ({ ...f, url: e.target.value }))}
                      placeholder="https://hooks.example.com/..."
                      required
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="wh-timeout">Timeout (seconds)</Label>
                    <Input
                      id="wh-timeout"
                      type="number"
                      min={1}
                      max={60}
                      value={webhookForm.timeout_seconds}
                      onChange={(e) =>
                        setWebhookForm((f) => ({ ...f, timeout_seconds: Number(e.target.value) }))
                      }
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="wh-retries">Retry count</Label>
                    <Input
                      id="wh-retries"
                      type="number"
                      min={0}
                      max={5}
                      value={webhookForm.retry_count}
                      onChange={(e) =>
                        setWebhookForm((f) => ({ ...f, retry_count: Number(e.target.value) }))
                      }
                    />
                  </div>
                </div>

                {/* Events grid */}
                <div>
                  <p className="mb-2 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                    Events
                  </p>
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {AVAILABLE_EVENTS.map((ev) => (
                      <label
                        key={ev}
                        className="inline-flex items-center gap-2 text-sm text-foreground cursor-pointer"
                      >
                        <input
                          type="checkbox"
                          className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                          checked={webhookForm.events.includes(ev)}
                          onChange={() => handleEventToggle(ev)}
                        />
                        <code className="font-mono text-xs">{ev}</code>
                      </label>
                    ))}
                  </div>
                </div>

                {/* Toggle options */}
                <div className="flex flex-wrap gap-4">
                  <label className="inline-flex items-center gap-2 text-sm text-foreground cursor-pointer">
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                      checked={webhookForm.enabled}
                      onChange={(e) =>
                        setWebhookForm((f) => ({ ...f, enabled: e.target.checked }))
                      }
                    />
                    Enabled
                  </label>
                  <label className="inline-flex items-center gap-2 text-sm text-foreground cursor-pointer">
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                      checked={webhookForm.verify_ssl}
                      onChange={(e) =>
                        setWebhookForm((f) => ({ ...f, verify_ssl: e.target.checked }))
                      }
                    />
                    Verify SSL
                  </label>
                </div>

                <div className="flex items-center justify-end gap-2 pt-2">
                  <Button type="button" variant="ghost" onClick={handleCancelEdit}>
                    Cancel
                  </Button>
                  <Button type="submit" variant="primary" loading={saving}>
                    {editingWebhook ? 'Save changes' : 'Create webhook'}
                  </Button>
                </div>
              </form>
            </Panel>
          )}

          {/* Add button when not editing */}
          {!showForm && (
            <div className="flex justify-end">
              <Button variant="primary" onClick={() => handleCreateWebhook()}>
                New webhook
              </Button>
            </div>
          )}

          {/* Webhook list */}
          {webhooksLoading && !webhooks.length ? (
            <p className="text-sm text-text-muted">Loading webhooks…</p>
          ) : webhooks.length === 0 ? (
            <EmptyState
              title="No webhooks configured"
              description="Add an endpoint to receive events from Control One."
            />
          ) : (
            <div className="flex flex-col gap-3">
              {webhooks.map((wh) => (
                <Panel
                  key={wh.id}
                  padding="md"
                  title={<span className="font-semibold text-foreground">{wh.name}</span>}
                  actions={
                    <div className="flex items-center gap-2">
                      <StatusTag tone={wh.enabled ? 'healthy' : 'unknown'}>
                        {wh.enabled ? 'Enabled' : 'Disabled'}
                      </StatusTag>
                      <Button variant="ghost" size="sm" onClick={() => handleTestWebhook(wh.id)}>
                        Test
                      </Button>
                      <Button variant="secondary" size="sm" onClick={() => handleEditWebhook(wh)}>
                        Edit
                      </Button>
                      <Button variant="danger" size="sm" onClick={() => setConfirmDeleteId(wh.id)}>
                        Delete
                      </Button>
                    </div>
                  }
                >
                  <div className="flex flex-col gap-2">
                    <code className="font-mono text-xs text-text-secondary break-all">{wh.url}</code>
                    <div className="flex flex-wrap gap-1">
                      {wh.events.map((ev) => (
                        <StatusTag key={ev} tone="info">
                          <code className="font-mono text-[0.65rem]">{ev}</code>
                        </StatusTag>
                      ))}
                    </div>
                    <div className="flex gap-4 text-xs text-text-muted">
                      {wh.last_triggered_at && (
                        <span>Last triggered: {formatDate(wh.last_triggered_at)}</span>
                      )}
                      {(wh.failure_count ?? 0) > 0 && (
                        <span className="text-state-warning">{wh.failure_count} failures</span>
                      )}
                    </div>
                  </div>
                </Panel>
              ))}
            </div>
          )}

          {confirmDeleteId && (
            <ConfirmModal
              open={confirmDeleteId !== null}
              title="Delete webhook?"
              body="This will permanently remove the webhook. Deliveries in-flight may still complete."
              variant="danger"
              confirmLabel="Delete"
              onConfirm={() => {
                handleDeleteWebhook(confirmDeleteId);
                setConfirmDeleteId(null);
              }}
              onCancel={() => setConfirmDeleteId(null)}
            />
          )}
        </TabsContent>

        <TabsContent value="security" className="mt-4 flex flex-col gap-4">
          <Panel
            padding="md"
            eyebrow="MFA · WEBAUTHN"
            title="Enrolled factors"
            actions={
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => setMfaReloadToken((n) => n + 1)}
                disabled={mfaLoading}
              >
                Refresh
              </Button>
            }
          >
            {mfaLoading ? (
              <p className="text-sm text-text-muted">Loading factors…</p>
            ) : mfaFactors.length === 0 ? (
              <EmptyState
                icon={<Shield />}
                title="No MFA factors enrolled"
                description="Enroll a hardware key or TOTP app to strengthen account security."
              />
            ) : (
              <ul className="flex flex-col divide-y divide-border-subtle">
                {mfaFactors.map((f) => (
                  <li key={f.id} className="flex items-center justify-between gap-4 py-2.5">
                    <div className="flex items-center gap-2.5">
                      <KeyRound className="h-4 w-4 text-text-muted" />
                      <div>
                        <p className="text-sm font-medium text-foreground">{f.name}</p>
                        <p className="text-xs text-text-muted capitalize">{f.type} · enrolled {new Date(f.created_at).toLocaleDateString()}</p>
                      </div>
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => setDeleteMfaId(f.id)}
                    >
                      <Trash2 className="h-3.5 w-3.5 text-state-critical" />
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </Panel>

          <ConfirmModal
            open={deleteMfaId !== null}
            title="Revoke MFA factor?"
            body="This factor will be removed immediately. You may be locked out if it is your only factor."
            confirmLabel="Revoke"
            variant="danger"
            onConfirm={handleDeleteMFA}
            onCancel={() => setDeleteMfaId(null)}
          />
        </TabsContent>

        <TabsContent value="system" className="mt-4 flex flex-col gap-4">
          <Panel
            padding="md"
            eyebrow="SYSTEM · WORKER POOL"
            title="Worker pool"
            actions={
              <Button type="button" variant="secondary" size="sm" onClick={refreshWorker} disabled={workerLoading}>
                Refresh
              </Button>
            }
          >
            {workerStatus ? (
              <div className="flex flex-col gap-4">
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                  <KpiTile label="Active jobs" value={workerStatus.active} tone="brand" />
                  <KpiTile label="Queue depth" value={workerStatus.queue_depth} tone={workerStatus.queue_depth > 50 ? 'warning' : 'healthy'} />
                  <KpiTile label="Backend" value={workerStatus.backend} />
                  <KpiTile label="Status" value={workerStatus.started ? 'Running' : 'Stopped'} tone={workerStatus.started ? 'healthy' : 'critical'} />
                </div>
                <div className="flex items-center gap-2">
                  <StatusTag
                    tone={workerStatus.started ? (workerStatus.active > 0 ? 'healthy' : 'unknown') : 'critical'}
                  >
                    {workerStatus.started ? (workerStatus.active > 0 ? 'Processing' : 'Idle') : 'Stopped'}
                  </StatusTag>
                  {workerStatus.last_error && (
                    <span className="text-xs text-state-critical">Last error: {workerStatus.last_error}</span>
                  )}
                </div>
              </div>
            ) : workerLoading ? (
              <p className="text-sm text-text-muted">Loading worker pool status…</p>
            ) : (
              <EmptyState title="Worker status unavailable" description="The worker pool did not respond." />
            )}
          </Panel>
        </TabsContent>

        <TabsContent value="integrations" className="mt-4">
          <EmptyState
            title="Integrations coming soon"
            description="Native integrations with Slack, PagerDuty, OpsGenie, Jira, and SIEM platforms are on the roadmap."
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}
