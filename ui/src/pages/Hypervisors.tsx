import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
import { SectionHeader, Panel, EmptyState, StatusTag, DataTable, SelectField, FileUploadButton } from '../components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import type { ColumnDef } from '@tanstack/react-table';
import { Server } from 'lucide-react';
import type { StateTone } from '../components/kit';
import type {
  CreateHypervisorHostPayload,
  CreateProviderCredentialPayload,
  HypervisorHost,
  HypervisorProvider,
  ProviderCredential,
} from '../lib/api';

const PROVIDERS: HypervisorProvider[] = ['libvirt', 'vmware', 'aws', 'azure'];

function formatDate(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function healthTone(status?: string): StateTone {
  const s = (status ?? '').toLowerCase();
  if (s === 'ok' || s === 'healthy') return 'healthy';
  if (s === 'error' || s === 'unreachable') return 'critical';
  return 'unknown';
}

interface CredentialFormState {
  tenant_id: string;
  provider: HypervisorProvider;
  name: string;
  // configText is the raw fallback for power users; the visual form state
  // below covers the typical case so non-engineers don't need to edit JSON.
  configText: string;
  showRawJSON: boolean;
  fields: Record<string, string>;
}

interface ProviderFieldSpec {
  key: string;
  label: string;
  type: 'text' | 'password' | 'textarea';
  placeholder?: string;
  helper?: string;
  required?: boolean;
}

// PROVIDER_FIELDS describes the visual form per provider. Keys match what the
// Go adapters expect inside the credential JSON. Adding a new provider is a
// matter of adding an entry here and wiring the adapter on the backend.
const PROVIDER_FIELDS: Record<HypervisorProvider, ProviderFieldSpec[]> = {
  libvirt: [
    { key: 'username', label: 'SSH user', type: 'text', placeholder: 'root', required: true,
      helper: 'User on the KVM host the agent connects as.' },
    { key: 'private_key', label: 'SSH private key', type: 'textarea',
      placeholder: '-----BEGIN OPENSSH PRIVATE KEY-----\n…',
      helper: 'PEM-encoded key. Stored encrypted; never displayed again.', required: true },
    { key: 'known_hosts', label: 'Known hosts entry', type: 'textarea',
      placeholder: 'kvm-01.example.com ssh-ed25519 AAAA…',
      helper: 'Optional but recommended — avoids TOFU prompts.' },
  ],
  vmware: [
    { key: 'username', label: 'vCenter user', type: 'text', placeholder: 'admin@vsphere.local', required: true },
    { key: 'password', label: 'vCenter password', type: 'password', required: true },
    { key: 'datacenter', label: 'Default datacenter', type: 'text', placeholder: 'DC-Production' },
    { key: 'folder', label: 'VM folder', type: 'text', placeholder: '/Production/Linux' },
    { key: 'insecure', label: 'Skip TLS verify (insecure)', type: 'text', placeholder: 'false' },
  ],
  aws: [
    { key: 'access_key_id', label: 'AWS access key ID', type: 'text', placeholder: 'AKIA…', required: true },
    { key: 'secret_access_key', label: 'AWS secret access key', type: 'password', required: true },
    { key: 'region', label: 'Default region', type: 'text', placeholder: 'us-east-1', required: true },
    { key: 'role_arn', label: 'Assume-role ARN (optional)', type: 'text',
      placeholder: 'arn:aws:iam::1234:role/control-one' },
  ],
  azure: [
    { key: 'tenant_id', label: 'Azure tenant ID', type: 'text', required: true },
    { key: 'subscription_id', label: 'Subscription ID', type: 'text', required: true },
    { key: 'client_id', label: 'Service principal client ID', type: 'text', required: true },
    { key: 'client_secret', label: 'Service principal secret', type: 'password', required: true },
    { key: 'resource_group', label: 'Default resource group', type: 'text', placeholder: 'rg-prod' },
  ],
};

function defaultFieldsFor(provider: HypervisorProvider): Record<string, string> {
  const out: Record<string, string> = {};
  PROVIDER_FIELDS[provider].forEach((f) => {
    out[f.key] = '';
  });
  return out;
}

function fieldsToConfigText(fields: Record<string, string>): string {
  const trimmed: Record<string, string> = {};
  Object.entries(fields).forEach(([k, v]) => {
    if (v && v.trim() !== '') trimmed[k] = v;
  });
  return JSON.stringify(trimmed, null, 2);
}

interface HostFormState {
  tenant_id: string;
  provider: HypervisorProvider;
  name: string;
  endpoint_url: string;
  credential_id: string;
  datacenter: string;
  labelsText: string;
}

const CREDENTIAL_FORM_DEFAULT: CredentialFormState = {
  tenant_id: '',
  provider: 'libvirt',
  name: '',
  configText: '',
  showRawJSON: false,
  fields: defaultFieldsFor('libvirt'),
};

const HOST_FORM_DEFAULT: HostFormState = {
  tenant_id: '',
  provider: 'libvirt',
  name: '',
  endpoint_url: '',
  credential_id: '',
  datacenter: '',
  labelsText: '',
};

export function Hypervisors(): JSX.Element {
  const api = useApiClient();
  const { data: tenants } = useTenants();
  const { showToast } = useToast();

  const [credentials, setCredentials] = useState<ProviderCredential[]>([]);
  const [hosts, setHosts] = useState<HypervisorHost[]>([]);
  const [loading, setLoading] = useState(true);
  const [verifying, setVerifying] = useState<string | null>(null);
  const [scanning, setScanning] = useState<string | null>(null);
  const [credentialForm, setCredentialForm] = useState<CredentialFormState>(CREDENTIAL_FORM_DEFAULT);
  const [hostForm, setHostForm] = useState<HostFormState>(HOST_FORM_DEFAULT);
  const [submittingCred, setSubmittingCred] = useState(false);
  const [submittingHost, setSubmittingHost] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [credResp, hostResp] = await Promise.all([
        api.listProviderCredentials({ limit: 200 }),
        api.listHypervisorHosts({ limit: 200 }),
      ]);
      setCredentials(credResp.items ?? []);
      setHosts(hostResp.items ?? []);
    } catch (err) {
      showToast(`Failed to load hypervisors: ${String(err)}`, 'error');
    } finally {
      setLoading(false);
    }
  }, [api, showToast]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const credentialsByProvider = useMemo(() => {
    const grouped: Record<HypervisorProvider, ProviderCredential[]> = { libvirt: [], vmware: [], aws: [], azure: [] };
    for (const c of credentials) {
      if (grouped[c.provider]) grouped[c.provider].push(c);
    }
    return grouped;
  }, [credentials]);

  async function handleCreateCredential(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (submittingCred) return;
    let config: Record<string, unknown>;
    if (credentialForm.showRawJSON) {
      try {
        config = JSON.parse(credentialForm.configText) as Record<string, unknown>;
      } catch {
        showToast('Invalid credential config: must be valid JSON', 'error');
        return;
      }
    } else {
      // Visual form path — strip empty fields and require provider-specific
      // required fields before submit so the operator gets a clear error
      // before the round-trip.
      const required = PROVIDER_FIELDS[credentialForm.provider]
        .filter((f) => f.required)
        .filter((f) => !credentialForm.fields[f.key] || credentialForm.fields[f.key].trim() === '');
      if (required.length > 0) {
        showToast(`Required: ${required.map((r) => r.label).join(', ')}`, 'error');
        return;
      }
      config = {};
      Object.entries(credentialForm.fields).forEach(([k, v]) => {
        if (v && v.trim() !== '') (config as Record<string, unknown>)[k] = v;
      });
    }
    if (!credentialForm.tenant_id || !credentialForm.name) {
      showToast('Tenant and name required', 'error');
      return;
    }
    setSubmittingCred(true);
    try {
      const payload: CreateProviderCredentialPayload = {
        tenant_id: credentialForm.tenant_id,
        provider: credentialForm.provider,
        name: credentialForm.name,
        config,
      };
      await api.createProviderCredential(payload);
      showToast(`Credential saved: ${credentialForm.name}`, 'success');
      setCredentialForm(CREDENTIAL_FORM_DEFAULT);
      await refresh();
    } catch (err) {
      showToast(`Failed to save credential: ${String(err)}`, 'error');
    } finally {
      setSubmittingCred(false);
    }
  }

  async function handleCreateHost(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (submittingHost) return;
    if (!hostForm.tenant_id || !hostForm.name || !hostForm.endpoint_url) {
      showToast('Tenant, name, and endpoint are required', 'error');
      return;
    }
    let labels: Record<string, unknown> | undefined;
    if (hostForm.labelsText.trim()) {
      try {
        labels = JSON.parse(hostForm.labelsText) as Record<string, unknown>;
      } catch {
        showToast('Invalid labels JSON: must be a valid object', 'error');
        return;
      }
    }
    setSubmittingHost(true);
    try {
      const payload: CreateHypervisorHostPayload = {
        tenant_id: hostForm.tenant_id,
        provider: hostForm.provider,
        name: hostForm.name,
        endpoint_url: hostForm.endpoint_url,
      };
      if (hostForm.credential_id) payload.credential_id = hostForm.credential_id;
      if (hostForm.datacenter) payload.datacenter = hostForm.datacenter;
      if (labels) payload.labels = labels;
      await api.createHypervisorHost(payload);
      showToast(`Hypervisor host added: ${hostForm.name}`, 'success');
      setHostForm(HOST_FORM_DEFAULT);
      await refresh();
    } catch (err) {
      showToast(`Failed to add host: ${String(err)}`, 'error');
    } finally {
      setSubmittingHost(false);
    }
  }

  async function handleVerify(host: HypervisorHost): Promise<void> {
    setVerifying(host.id);
    try {
      const resp = await api.verifyHypervisorHost(host.id);
      showToast(
        resp.status === 'ok' ? `Host reachable: ${host.endpoint_url}` : `Host unreachable: ${resp.message ?? host.endpoint_url}`,
        resp.status === 'ok' ? 'success' : 'error',
      );
      await refresh();
    } catch (err) {
      showToast(`Verify failed: ${String(err)}`, 'error');
    } finally {
      setVerifying(null);
    }
  }

  async function handleDeleteHost(host: HypervisorHost): Promise<void> {
    if (!window.confirm(`Remove hypervisor host "${host.name}"? Clusters referencing it will be detached.`)) return;
    try {
      await api.deleteHypervisorHost(host.id);
      showToast(`Host removed: ${host.name}`, 'success');
      await refresh();
    } catch (err) {
      showToast(`Failed to remove host: ${String(err)}`, 'error');
    }
  }

  async function handleScan(host: HypervisorHost): Promise<void> {
    setScanning(host.id);
    try {
      const job = await api.createJob({
        type: 'hypervisor.scan',
        tenant_id: host.tenant_id,
        payload: { host_id: host.id, provider: host.provider },
      });
      showToast(`Scan job queued: ${job.id.slice(0, 8)}…`, 'success');
    } catch (err) {
      showToast(`Failed to start scan: ${String(err)}`, 'error');
    } finally {
      setScanning(null);
    }
  }

  async function handleDeleteCredential(cred: ProviderCredential): Promise<void> {
    if (!window.confirm(`Delete credential "${cred.name}"? Hosts referencing it will lose their credential_id.`)) return;
    try {
      await api.deleteProviderCredential(cred.id);
      showToast(`Credential removed: ${cred.name}`, 'success');
      await refresh();
    } catch (err) {
      showToast(`Failed to remove credential: ${String(err)}`, 'error');
    }
  }

  const hostColumns: ColumnDef<HypervisorHost>[] = [
    {
      header: 'Name',
      accessorKey: 'name',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
    },
    {
      header: 'Provider',
      accessorKey: 'provider',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.provider}</span>,
    },
    {
      header: 'Endpoint',
      accessorKey: 'endpoint_url',
      cell: ({ row }) => (
        <code className="font-mono text-xs text-text-secondary">{row.original.endpoint_url}</code>
      ),
    },
    {
      header: 'Datacenter',
      accessorKey: 'datacenter',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.datacenter ?? '—'}</span>,
    },
    {
      header: 'Health',
      id: 'health',
      cell: ({ row }) => (
        <div className="flex flex-col gap-0.5">
          <StatusTag tone={healthTone(row.original.health_status)}>
            {row.original.health_status ?? 'unknown'}
          </StatusTag>
          {row.original.health_message ? (
            <span className="text-xs text-text-muted">{row.original.health_message}</span>
          ) : null}
        </div>
      ),
    },
    {
      header: 'Last verified',
      id: 'last_verified',
      cell: ({ row }) => (
        <span className="text-text-secondary">{formatDate(row.original.last_verified_at)}</span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex items-center gap-1.5">
          <Button
            type="button"
            variant="primary"
            size="sm"
            onClick={() => handleScan(row.original)}
            disabled={scanning === row.original.id || verifying === row.original.id}
          >
            {scanning === row.original.id ? 'Scanning…' : 'Scan'}
          </Button>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={() => handleVerify(row.original)}
            disabled={verifying === row.original.id || scanning === row.original.id}
          >
            {verifying === row.original.id ? 'Verifying…' : 'Verify'}
          </Button>
          <Button
            type="button"
            variant="danger"
            size="sm"
            onClick={() => handleDeleteHost(row.original)}
          >
            Remove
          </Button>
        </div>
      ),
    },
  ];

  const credColumns: ColumnDef<ProviderCredential>[] = [
    {
      header: 'Name',
      accessorKey: 'name',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
    },
    {
      header: 'Provider',
      accessorKey: 'provider',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.provider}</span>,
    },
    {
      header: 'Created',
      accessorKey: 'created_at',
      cell: ({ row }) => (
        <span className="text-text-secondary">{formatDate(row.original.created_at)}</span>
      ),
    },
    {
      header: 'Rotated',
      accessorKey: 'rotated_at',
      cell: ({ row }) => (
        <span className="text-text-secondary">{formatDate(row.original.rotated_at)}</span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          type="button"
          variant="danger"
          size="sm"
          onClick={() => handleDeleteCredential(row.original)}
        >
          Delete
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="INFRASTRUCTURE · HYPERVISORS"
        title="Hypervisors & provider credentials"
        description="Register the virtualization hosts and cloud accounts Control One provisions against. Multiple hosts per tenant across datacenters are supported."
      />

      {/* Forms row */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* LEFT: Add host */}
        <Panel padding="md" eyebrow="ADD HOST" title="Register hypervisor host" toneAccent="brand">
          <form onSubmit={handleCreateHost} className="flex flex-col gap-3">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <SelectField
                id="host-tenant"
                label="Tenant"
                value={hostForm.tenant_id}
                onChange={(e) => setHostForm((f) => ({ ...f, tenant_id: e.target.value }))}
                required
              >
                <option value="">Select tenant…</option>
                {tenants?.map((t) => (
                  <option key={t.id} value={t.id}>{t.name}</option>
                ))}
              </SelectField>
              <SelectField
                id="host-provider"
                label="Provider"
                value={hostForm.provider}
                onChange={(e) => setHostForm((f) => ({ ...f, provider: e.target.value as HypervisorProvider, credential_id: '' }))}
              >
                {PROVIDERS.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </SelectField>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="host-name">Name</Label>
                <Input
                  id="host-name"
                  type="text"
                  value={hostForm.name}
                  onChange={(e) => setHostForm((f) => ({ ...f, name: e.target.value }))}
                  placeholder="lon-kvm-01"
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="host-endpoint">Endpoint URL</Label>
                <Input
                  id="host-endpoint"
                  type="text"
                  value={hostForm.endpoint_url}
                  onChange={(e) => setHostForm((f) => ({ ...f, endpoint_url: e.target.value }))}
                  placeholder={hostForm.provider === 'libvirt' ? 'qemu+ssh://root@kvm-01/system' : 'https://vcenter.lon'}
                  required
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="host-datacenter">Datacenter (optional)</Label>
                <Input
                  id="host-datacenter"
                  type="text"
                  value={hostForm.datacenter}
                  onChange={(e) => setHostForm((f) => ({ ...f, datacenter: e.target.value }))}
                  placeholder="lon-dc-1"
                />
              </div>
              <SelectField
                id="host-credential"
                label="Credential"
                value={hostForm.credential_id}
                onChange={(e) => setHostForm((f) => ({ ...f, credential_id: e.target.value }))}
              >
                <option value="">No credential (env-based)</option>
                {(credentialsByProvider[hostForm.provider] || []).map((c) => (
                  <option key={c.id} value={c.id}>{c.name}</option>
                ))}
              </SelectField>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="host-labels">Labels (JSON, optional)</Label>
              <textarea
                id="host-labels"
                className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                rows={3}
                value={hostForm.labelsText}
                onChange={(e) => setHostForm((f) => ({ ...f, labelsText: e.target.value }))}
                placeholder='{"tier":"prod","region":"eu-west"}'
              />
            </div>

            <div className="flex items-center gap-2 pt-2">
              <Button type="submit" variant="primary" disabled={submittingHost}>
                {submittingHost ? 'Saving…' : 'Add host'}
              </Button>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setHostForm(HOST_FORM_DEFAULT)}
                disabled={submittingHost}
              >
                Reset
              </Button>
            </div>
          </form>
        </Panel>

        {/* RIGHT: Add credential */}
        <Panel padding="md" eyebrow="ADD CREDENTIAL" title="Provider credential" toneAccent="accent">
          <p className="text-xs text-text-muted">
            Credentials are encrypted at rest with AES-256-GCM. Never shown again after save — rotate by posting a new config.
          </p>
          <form onSubmit={handleCreateCredential} className="flex flex-col gap-3">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <SelectField
                id="cred-tenant"
                label="Tenant"
                value={credentialForm.tenant_id}
                onChange={(e) => setCredentialForm((f) => ({ ...f, tenant_id: e.target.value }))}
                required
              >
                <option value="">Select tenant…</option>
                {tenants?.map((t) => (
                  <option key={t.id} value={t.id}>{t.name}</option>
                ))}
              </SelectField>
              <SelectField
                id="cred-provider"
                label="Provider"
                value={credentialForm.provider}
                onChange={(e) => {
                  const next = e.target.value as HypervisorProvider;
                  setCredentialForm((f) => ({
                    ...f,
                    provider: next,
                    fields: defaultFieldsFor(next),
                    configText: '',
                  }));
                }}
              >
                {PROVIDERS.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </SelectField>
              <div className="flex flex-col gap-1.5 sm:col-span-2">
                <Label htmlFor="cred-name">Name</Label>
                <Input
                  id="cred-name"
                  type="text"
                  value={credentialForm.name}
                  onChange={(e) => setCredentialForm((f) => ({ ...f, name: e.target.value }))}
                  placeholder="kvm-root"
                  required
                />
              </div>
            </div>

            {!credentialForm.showRawJSON ? (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {PROVIDER_FIELDS[credentialForm.provider].map((spec) => (
                  spec.type === 'textarea' ? (
                    <div key={spec.key} className="flex flex-col gap-1.5 sm:col-span-2">
                      <div className="flex items-center justify-between">
                        <Label htmlFor={`cred-field-${spec.key}`}>
                          {spec.label}{spec.required ? ' *' : ''}
                        </Label>
                        <FileUploadButton
                          accept=".pem,.key,.pub,.crt,.cer,text/plain"
                          label="Upload file"
                          onContent={(text) =>
                            setCredentialForm((f) => ({ ...f, fields: { ...f.fields, [spec.key]: text.trim() } }))
                          }
                        />
                      </div>
                      <textarea
                        id={`cred-field-${spec.key}`}
                        className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50 resize-y"
                        rows={4}
                        value={credentialForm.fields[spec.key] ?? ''}
                        onChange={(e) =>
                          setCredentialForm((f) => ({ ...f, fields: { ...f.fields, [spec.key]: e.target.value } }))
                        }
                        placeholder={spec.placeholder}
                        autoComplete="off"
                      />
                      {spec.helper ? <p className="text-xs text-text-muted">{spec.helper}</p> : null}
                    </div>
                  ) : (
                    <div key={spec.key} className="flex flex-col gap-1.5">
                      <Label htmlFor={`cred-field-${spec.key}`}>
                        {spec.label}{spec.required ? ' *' : ''}
                      </Label>
                      <Input
                        id={`cred-field-${spec.key}`}
                        type={spec.type}
                        value={credentialForm.fields[spec.key] ?? ''}
                        onChange={(e) =>
                          setCredentialForm((f) => ({ ...f, fields: { ...f.fields, [spec.key]: e.target.value } }))
                        }
                        placeholder={spec.placeholder}
                        required={spec.required}
                        autoComplete={spec.type === 'password' ? 'new-password' : 'off'}
                      />
                      {spec.helper ? <p className="text-xs text-text-muted">{spec.helper}</p> : null}
                    </div>
                  )
                ))}
              </div>
            ) : (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="cred-raw-json">Config (raw JSON)</Label>
                <textarea
                  id="cred-raw-json"
                  className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                  rows={6}
                  value={credentialForm.configText || fieldsToConfigText(credentialForm.fields)}
                  onChange={(e) => setCredentialForm((f) => ({ ...f, configText: e.target.value }))}
                  required
                />
              </div>
            )}

            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() =>
                setCredentialForm((f) => ({
                  ...f,
                  showRawJSON: !f.showRawJSON,
                  configText: f.showRawJSON ? '' : fieldsToConfigText(f.fields),
                }))
              }
            >
              {credentialForm.showRawJSON ? 'Use visual form' : 'Edit raw JSON'}
            </Button>

            <div className="flex items-center gap-2 pt-2">
              <Button type="submit" variant="primary" disabled={submittingCred}>
                {submittingCred ? 'Saving…' : 'Save credential'}
              </Button>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setCredentialForm(CREDENTIAL_FORM_DEFAULT)}
                disabled={submittingCred}
              >
                Reset
              </Button>
            </div>
          </form>
        </Panel>
      </div>

      {/* Hosts table */}
      <Panel padding="md" eyebrow="HOSTS" title="Registered hosts">
        <DataTable
          columns={hostColumns}
          rows={hosts}
          loading={loading}
          rowKey={(row) => row.id}
          empty={
            <EmptyState
              title="No hypervisor hosts"
              description="No hypervisor hosts configured yet."
              icon={<Server />}
            />
          }
        />
      </Panel>

      {/* Credentials table */}
      <Panel padding="md" eyebrow="CREDENTIALS" title="Stored credentials">
        <DataTable
          columns={credColumns}
          rows={credentials}
          rowKey={(row) => row.id}
          empty={
            <EmptyState
              title="No credentials"
              description="No provider credentials configured yet."
            />
          }
        />
      </Panel>
    </div>
  );
}
