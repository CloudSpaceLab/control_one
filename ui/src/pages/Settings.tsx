import { FormEvent, useEffect, useState } from 'react';
import QRCode from 'qrcode';
import { useWebhooks } from '../hooks/useWebhooks';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { useTenant } from '../providers/TenantProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { Webhook, CreateWebhookPayload, UpdateWebhookPayload, MFAFactor } from '../lib/api';
import { ConfirmModal } from '../components/ConfirmModal';
import { Panel, SectionHeader, StatusTag, EmptyState, SelectField, KpiTile } from '../components/kit';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../components/ui/tabs';
import { AISettingsTab } from '../components/settings/AISettingsTab';
import { KeyRound, Shield, Trash2 } from 'lucide-react';

function formatDate(value?: string): string {
  if (!value) {
    return '-';
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

const newWebhookForm = (): CreateWebhookPayload => ({
  name: '',
  url: '',
  events: [],
  enabled: true,
  verify_ssl: true,
  timeout_seconds: 30,
  retry_count: 3,
});

function parseWebhookHeaders(value: string): Record<string, unknown> | undefined {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Custom headers must be a JSON object');
  }
  return parsed as Record<string, unknown>;
}

function webhookHasHeaders(webhook?: Webhook | null): boolean {
  return Boolean(webhook?.headers_configured) || Object.keys(webhook?.headers ?? {}).length > 0;
}

function objectRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function base64URLToArrayBuffer(value: string): ArrayBuffer {
  let base64 = value.replace(/-/g, '+').replace(/_/g, '/');
  const padding = base64.length % 4;
  if (padding > 0) {
    base64 += '='.repeat(4 - padding);
  }
  const binary = window.atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes.buffer;
}

function arrayBufferToBase64URL(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
  }
  return window.btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function prepareWebAuthnCreationOptions(options: unknown): PublicKeyCredentialCreationOptions {
  const wrapper = objectRecord(options);
  const rawPublicKey = objectRecord(wrapper?.publicKey) ?? wrapper;
  if (!rawPublicKey) {
    throw new Error('WebAuthn enrollment response did not include publicKey options');
  }
  const publicKey: Record<string, unknown> = { ...rawPublicKey };
  if (typeof publicKey.challenge !== 'string') {
    throw new Error('WebAuthn enrollment challenge was missing');
  }
  publicKey.challenge = base64URLToArrayBuffer(publicKey.challenge);

  const user = objectRecord(publicKey.user);
  if (user) {
    const userID = user.id;
    publicKey.user = {
      ...user,
      id: typeof userID === 'string' ? base64URLToArrayBuffer(userID) : userID,
    };
  }

  if (Array.isArray(publicKey.excludeCredentials)) {
    publicKey.excludeCredentials = publicKey.excludeCredentials.map((item) => {
      const descriptor = objectRecord(item);
      if (!descriptor || typeof descriptor.id !== 'string') {
        return item;
      }
      return {
        ...descriptor,
        id: base64URLToArrayBuffer(descriptor.id),
      };
    });
  }

  return publicKey as unknown as PublicKeyCredentialCreationOptions;
}

function isPublicKeyCredential(credential: Credential | null): credential is PublicKeyCredential {
  return Boolean(credential && credential.type === 'public-key' && 'rawId' in credential && 'response' in credential);
}

function serializeWebAuthnAttestation(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAttestationResponse;
  if (!(credential.rawId instanceof ArrayBuffer) || !(response.clientDataJSON instanceof ArrayBuffer) || !(response.attestationObject instanceof ArrayBuffer)) {
    throw new Error('Browser returned an incomplete WebAuthn attestation');
  }
  const serializedResponse: Record<string, unknown> = {
    clientDataJSON: arrayBufferToBase64URL(response.clientDataJSON),
    attestationObject: arrayBufferToBase64URL(response.attestationObject),
  };
  const transports = response.getTransports?.();
  if (transports && transports.length > 0) {
    serializedResponse.transports = transports;
  }
  return {
    id: credential.id,
    rawId: arrayBufferToBase64URL(credential.rawId),
    type: credential.type,
    response: serializedResponse,
    clientExtensionResults: credential.getClientExtensionResults?.() ?? {},
  };
}

export function Settings(): JSX.Element {
  const api = useApiClient();
  const [activeTab, setActiveTab] = useState<'webhooks' | 'security' | 'system' | 'integrations' | 'trust-center' | 'ai'>('webhooks');

  // MFA state
  const [mfaFactors, setMfaFactors] = useState<MFAFactor[]>([]);
  const [mfaLoading, setMfaLoading] = useState(false);
  const [mfaReloadToken, setMfaReloadToken] = useState(0);
  const [deleteMfaId, setDeleteMfaId] = useState<string | null>(null);
  const [totpEnrollStep, setTotpEnrollStep] = useState<'idle' | 'scanning' | 'verifying'>('idle');
  const [totpEnrollData, setTotpEnrollData] = useState<{ factor_id: string; secret: string; provisioning_uri: string } | null>(null);
  const [totpQrDataUrl, setTotpQrDataUrl] = useState<string | null>(null);
  const [totpCode, setTotpCode] = useState('');
  const [totpLabel, setTotpLabel] = useState('Authenticator app');
  const [webauthnEnrollStep, setWebauthnEnrollStep] = useState<'idle' | 'enrolling'>('idle');
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const [recoveryCodesLoading, setRecoveryCodesLoading] = useState(false);

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

  const handleBeginTOTPEnroll = async () => {
    try {
      const data = await api.beginTOTPEnroll();
      let qrDataUrl: string | null = null;
      try {
        qrDataUrl = await QRCode.toDataURL(data.provisioning_uri, {
          errorCorrectionLevel: 'M',
          margin: 1,
          width: 200,
        });
      } catch (qrError) {
        console.error('Failed to render local TOTP QR code', qrError);
        showToast('Could not render the QR code locally. Enter the secret manually.', 'error');
      }
      setTotpEnrollData(data);
      setTotpQrDataUrl(qrDataUrl);
      setTotpEnrollStep('scanning');
    } catch (err) {
      console.error('Failed to begin TOTP enrollment', err);
      showToast('Could not start TOTP enrollment.', 'error');
    }
  };

  const handleFinishTOTPEnroll = async (e: FormEvent) => {
    e.preventDefault();
    if (!totpEnrollData || !totpCode) return;
    try {
      await api.finishTOTPEnroll(totpEnrollData.factor_id, totpCode, totpLabel);
      setTotpEnrollStep('idle');
      setTotpEnrollData(null);
      setTotpQrDataUrl(null);
      setTotpCode('');
      setMfaReloadToken((n) => n + 1);
      showToast('TOTP factor enrolled successfully', 'success');
    } catch (err) {
      console.error('Failed to finish TOTP enrollment', err);
      showToast('Could not verify the TOTP code.', 'error');
    }
  };

  const handleCancelTOTPEnroll = () => {
    setTotpEnrollStep('idle');
    setTotpEnrollData(null);
    setTotpQrDataUrl(null);
    setTotpCode('');
  };

  const handleBeginWebAuthnEnroll = async () => {
    try {
      if (!window.PublicKeyCredential || !navigator.credentials?.create) {
        showToast('This browser does not support security key enrollment.', 'error');
        return;
      }
      setWebauthnEnrollStep('enrolling');
      const data = await api.beginWebAuthnEnroll();
      const credential = await navigator.credentials.create({
        publicKey: prepareWebAuthnCreationOptions(data.options),
      });
      if (!isPublicKeyCredential(credential)) {
        throw new Error('No security key credential was created');
      }
      await api.finishWebAuthnEnroll(data.challenge_id, 'Security key', serializeWebAuthnAttestation(credential));
      setMfaReloadToken((n) => n + 1);
      showToast('Security key enrolled successfully', 'success');
      setWebauthnEnrollStep('idle');
    } catch (err) {
      console.error('Failed to begin WebAuthn enrollment', err);
      showToast(err instanceof Error ? err.message : 'Could not enroll the security key.', 'error');
      setWebauthnEnrollStep('idle');
    }
  };

  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [isCreatingWebhook, setIsCreatingWebhook] = useState(false);
  const [editingWebhook, setEditingWebhook] = useState<Webhook | null>(null);
  const [webhookSecret, setWebhookSecret] = useState('');
  const [clearWebhookSecret, setClearWebhookSecret] = useState(false);
  const [webhookHeadersText, setWebhookHeadersText] = useState('');
  const [clearWebhookHeaders, setClearWebhookHeaders] = useState(false);

  const { currentTenantId } = useTenant();
  const effectiveTenant = selectedTenant ?? currentTenantId ?? undefined;
  const { data: tenants } = useTenants();
  const {
    data: webhooks,
    loading: webhooksLoading,
    error: webhooksError,
    reload: reloadWebhooks,
  } = useWebhooks({
    tenant_id: effectiveTenant,
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
  const hasRecoveryFactor = mfaFactors.some((factor) => factor.type === 'recovery');

  const handleGenerateRecoveryCodes = async () => {
    try {
      setRecoveryCodesLoading(true);
      const data = await api.generateMFARecoveryCodes(10);
      setRecoveryCodes(data.codes);
      setMfaReloadToken((n) => n + 1);
      showToast(
        hasRecoveryFactor ? 'Backup codes regenerated. Store them now.' : 'Backup codes generated. Store them now.',
        'success',
      );
    } catch (err) {
      console.error('Failed to generate recovery codes', err);
      showToast('Could not generate backup codes. Confirm MFA encryption is configured.', 'error');
    } finally {
      setRecoveryCodesLoading(false);
    }
  };

  const [webhookForm, setWebhookForm] = useState<CreateWebhookPayload>(newWebhookForm);

  const resetWebhookSensitiveForm = () => {
    setWebhookSecret('');
    setClearWebhookSecret(false);
    setWebhookHeadersText('');
    setClearWebhookHeaders(false);
  };

  const handleCreateWebhook = () => {
    setIsCreatingWebhook(true);
    setEditingWebhook(null);
    setWebhookForm(newWebhookForm());
    resetWebhookSensitiveForm();
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
    resetWebhookSensitiveForm();
    resetFeedback();
  };

  const handleCancelEdit = () => {
    setIsCreatingWebhook(false);
    setEditingWebhook(null);
    resetWebhookSensitiveForm();
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
    if (!effectiveTenant) {
      showError('Tenant is required');
      return;
    }

    setSaving(true);
    resetFeedback();

    try {
      let parsedHeaders: Record<string, unknown> | undefined;
      try {
        parsedHeaders = parseWebhookHeaders(webhookHeadersText);
      } catch (err) {
        showError(err instanceof Error ? err.message : 'Custom headers must be valid JSON');
        return;
      }

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
        const trimmedSecret = webhookSecret.trim();
        if (clearWebhookSecret) {
          payload.secret = '';
        } else if (trimmedSecret) {
          payload.secret = trimmedSecret;
        }
        if (clearWebhookHeaders) {
          payload.headers = {};
        } else if (parsedHeaders) {
          payload.headers = parsedHeaders;
        }
        await api.updateWebhook(editingWebhook.id, payload);
        showSuccess('Webhook updated successfully');
      } else {
        const payload: CreateWebhookPayload = {
          ...webhookForm,
          tenant_id: effectiveTenant,
        };
        const trimmedSecret = webhookSecret.trim();
        if (trimmedSecret) {
          payload.secret = trimmedSecret;
        }
        if (parsedHeaders) {
          payload.headers = parsedHeaders;
        }
        await api.createWebhook(payload);
        showSuccess('Webhook created successfully');
      }
      setIsCreatingWebhook(false);
      setEditingWebhook(null);
      resetWebhookSensitiveForm();
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
  const trustCenterTenant = tenants.find((tenant) => tenant.id === effectiveTenant);
  const trustCenterTenantName = trustCenterTenant?.name ?? tenants[0]?.name ?? 'default';
  const trustCenterHref = `/trust/${encodeURIComponent(trustCenterTenantName)}`;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="SETTINGS"
        title="Settings"
        description="Webhook endpoints and platform integrations."
      />

      <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as 'webhooks' | 'security' | 'system' | 'integrations' | 'trust-center' | 'ai')}>
        <TabsList className="grid h-auto w-full grid-cols-2 gap-1 overflow-visible sm:inline-flex sm:w-auto sm:grid-cols-none">
          <TabsTrigger className="w-full sm:w-auto" value="webhooks">Webhooks</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="security">Security</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="system">System health</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="integrations">Integrations</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="trust-center">Trust Center</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="ai">AI</TabsTrigger>
        </TabsList>

        <TabsContent value="webhooks" className="mt-4 flex flex-col gap-4">
          {/* Tenant filter */}
          <div className="flex items-center gap-3">
            <SelectField
              id="tenant-filter"
              label="Filter by tenant"
              value={effectiveTenant ?? ''}
              onChange={(e) => setSelectedTenant(e.target.value || undefined)}
              wrapperClassName="w-64"
            >
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
                  <div className="flex flex-col gap-1.5 sm:col-span-2">
                    <Label htmlFor="wh-secret">Signing secret</Label>
                    <Input
                      id="wh-secret"
                      type="password"
                      autoComplete="off"
                      value={webhookSecret}
                      disabled={clearWebhookSecret}
                      onChange={(e) => setWebhookSecret(e.target.value)}
                      placeholder={editingWebhook?.secret_configured ? 'Configured; leave blank to keep' : 'Optional HMAC secret'}
                    />
                    {editingWebhook?.secret_configured && (
                      <label className="inline-flex items-center gap-2 text-xs text-text-muted cursor-pointer">
                        <input
                          type="checkbox"
                          className="h-3.5 w-3.5 rounded border-border-subtle accent-brand-500 cursor-pointer"
                          checked={clearWebhookSecret}
                          onChange={(e) => {
                            setClearWebhookSecret(e.target.checked);
                            if (e.target.checked) {
                              setWebhookSecret('');
                            }
                          }}
                        />
                        Clear configured signing secret
                      </label>
                    )}
                  </div>
                  <div className="flex flex-col gap-1.5 sm:col-span-2">
                    <Label htmlFor="wh-headers">Custom headers JSON</Label>
                    <textarea
                      id="wh-headers"
                      className="min-h-24 rounded-lg border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground outline-none transition focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 disabled:cursor-not-allowed disabled:opacity-60"
                      value={webhookHeadersText}
                      disabled={clearWebhookHeaders}
                      onChange={(e) => setWebhookHeadersText(e.target.value)}
                      placeholder={webhookHasHeaders(editingWebhook) ? '{"Authorization":"Bearer ..."} leave blank to keep' : '{"X-Team":"secops"}'}
                    />
                    {editingWebhook && webhookHasHeaders(editingWebhook) && (
                      <label className="inline-flex items-center gap-2 text-xs text-text-muted cursor-pointer">
                        <input
                          type="checkbox"
                          className="h-3.5 w-3.5 rounded border-border-subtle accent-brand-500 cursor-pointer"
                          checked={clearWebhookHeaders}
                          onChange={(e) => {
                            setClearWebhookHeaders(e.target.checked);
                            if (e.target.checked) {
                              setWebhookHeadersText('');
                            }
                          }}
                        />
                        Clear configured custom headers
                      </label>
                    )}
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
            <p className="text-sm text-text-muted">Loading webhooks...</p>
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
                      {wh.secret_configured && (
                        <StatusTag tone="info">Signed</StatusTag>
                      )}
                      {webhookHasHeaders(wh) && (
                        <StatusTag tone="info">Custom headers</StatusTag>
                      )}
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
            eyebrow="MFA / WEBAUTHN"
            title="Enrolled factors"
            actions={
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={handleBeginTOTPEnroll}
                  disabled={totpEnrollStep !== 'idle' || mfaLoading}
                >
                  Add TOTP
                </Button>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={handleBeginWebAuthnEnroll}
                  disabled={webauthnEnrollStep !== 'idle' || mfaLoading}
                >
                  {webauthnEnrollStep === 'enrolling' ? 'Adding...' : 'Add Security Key'}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setMfaReloadToken((n) => n + 1)}
                  disabled={mfaLoading}
                >
                  Refresh
                </Button>
              </div>
            }
          >
            {totpEnrollStep === 'scanning' && totpEnrollData && (
              <div className="p-4 rounded-lg border border-border-subtle bg-surface mb-4">
                <h3 className="text-sm font-medium text-foreground mb-2">Scan QR Code</h3>
                <div className="flex flex-col gap-3">
                  {totpQrDataUrl && (
                    <img src={totpQrDataUrl} alt="TOTP QR Code" className="w-48 h-48 mx-auto" />
                  )}
                  <p className="text-xs text-text-muted text-center">Or enter this secret: <code className="bg-surface-2 px-1 rounded">{totpEnrollData.secret}</code></p>
                  <form onSubmit={handleFinishTOTPEnroll} className="flex flex-col gap-2">
                    <Label htmlFor="totp-code">Verification Code</Label>
                    <Input
                      id="totp-code"
                      type="text"
                      placeholder="6-digit code"
                      value={totpCode}
                      onChange={(e) => setTotpCode(e.target.value)}
                      maxLength={6}
                      pattern="[0-9]{6}"
                    />
                    <Label htmlFor="totp-label">Label</Label>
                    <Input
                      id="totp-label"
                      type="text"
                      value={totpLabel}
                      onChange={(e) => setTotpLabel(e.target.value)}
                      placeholder="Authenticator app"
                    />
                    <div className="flex gap-2">
                      <Button type="submit" variant="primary" size="sm">
                        Verify & Enable
                      </Button>
                      <Button type="button" variant="ghost" size="sm" onClick={handleCancelTOTPEnroll}>
                        Cancel
                      </Button>
                    </div>
                  </form>
                </div>
              </div>
            )}

            {mfaLoading ? (
              <p className="text-sm text-text-muted">Loading factors...</p>
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
                        <p className="text-xs text-text-muted capitalize">{f.type} / enrolled {new Date(f.created_at).toLocaleDateString()}</p>
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

          <Panel
            padding="md"
            eyebrow="RECOVERY CODES"
            title="Backup Codes"
            actions={
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={handleGenerateRecoveryCodes}
                disabled={recoveryCodesLoading}
              >
                {recoveryCodesLoading ? 'Generating...' : hasRecoveryFactor ? 'Regenerate Codes' : 'Generate Codes'}
              </Button>
            }
          >
            <p className="text-sm text-text-secondary mb-4">
              Generate one-time backup codes for MFA step-up if your authenticator is unavailable.
            </p>
            {recoveryCodes && (
              <div className="rounded-lg border border-border-subtle bg-surface p-4">
                <p className="mb-2 text-xs text-state-critical">Save these codes now. They will not be shown again.</p>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {recoveryCodes.map((code) => (
                    <code key={code} className="rounded bg-surface-2 px-2 py-1 font-mono text-sm">
                      {code}
                    </code>
                  ))}
                </div>
              </div>
            )}
          </Panel>
        </TabsContent>

        <TabsContent value="system" className="mt-4 flex flex-col gap-4">
          <Panel
            padding="md"
            eyebrow="SYSTEM / WORKER POOL"
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
              <p className="text-sm text-text-muted">Loading worker pool status...</p>
            ) : (
              <EmptyState title="Worker status unavailable" description="The worker pool did not respond." />
            )}
          </Panel>
        </TabsContent>

        <TabsContent value="integrations" className="mt-4">
          <EmptyState
            title="No native integrations configured"
            description="Alert delivery and ticketing integrations are not connected for this tenant."
          />
        </TabsContent>

        <TabsContent value="trust-center" className="mt-4 flex flex-col gap-4">
          <Panel
            padding="md"
            eyebrow="TRUST CENTER / ADMIN"
            title="Public Trust Center Management"
          >
            <p className="text-sm text-text-secondary mb-4">
              Manage the public-facing compliance transparency portal for your tenants.
            </p>
            <div className="flex flex-col gap-3">
              <p className="text-sm text-text-secondary">
                The Trust Center displays subprocessors, certifications, security FAQ, and incident history to the public.
                Access the public portal at <code className="text-brand-600">/trust/:tenant-name</code>.
              </p>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-2">
                <Button variant="secondary" asChild>
                  <a href={trustCenterHref} target="_blank" rel="noopener noreferrer">
                    View Public Trust Center
                  </a>
                </Button>
                <Button variant="secondary" onClick={() => { window.location.href = '/console/settings'; }}>
                  Manage via API
                </Button>
              </div>
            </div>
          </Panel>

          <Panel
            padding="md"
            eyebrow="SUBPROCESSORS"
            title="Third-Party Service Providers"
          >
            <p className="text-sm text-text-secondary mb-4">
              Add and manage subprocessors that may process customer data.
            </p>
            <EmptyState
              title="Manage via API"
              description="Use the Trust Center API endpoints to manage subprocessors, certifications, FAQ, and incidents."
            />
          </Panel>

          <Panel
            padding="md"
            eyebrow="CERTIFICATIONS"
            title="Compliance Certifications"
          >
            <p className="text-sm text-text-secondary mb-4">
              Add and manage security and compliance certifications.
            </p>
            <EmptyState
              title="Manage via API"
              description="Use the Trust Center API endpoints to manage certifications."
            />
          </Panel>

          <Panel
            padding="md"
            eyebrow="SECURITY FAQ"
            title="Frequently Asked Questions"
          >
            <p className="text-sm text-text-secondary mb-4">
              Add and manage security and privacy FAQ items.
            </p>
            <EmptyState
              title="Manage via API"
              description="Use the Trust Center API endpoints to manage FAQ items."
            />
          </Panel>

          <Panel
            padding="md"
            eyebrow="INCIDENTS"
            title="Security Incident History"
          >
            <p className="text-sm text-text-secondary mb-4">
              Publish security incidents and their resolution status.
            </p>
            <EmptyState
              title="Manage via API"
              description="Use the Trust Center API endpoints to manage incident reports."
            />
          </Panel>
        </TabsContent>

        <TabsContent value="ai" className="mt-4">
          <AISettingsTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}
