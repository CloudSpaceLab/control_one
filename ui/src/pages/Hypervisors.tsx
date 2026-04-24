import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
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

interface CredentialFormState {
  tenant_id: string;
  provider: HypervisorProvider;
  name: string;
  configText: string;
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
  configText: '{\n  "username": "",\n  "password": ""\n}',
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
    try {
      config = JSON.parse(credentialForm.configText) as Record<string, unknown>;
    } catch {
      showToast('Invalid credential config: must be valid JSON', 'error');
      return;
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

  return (
    <section className="page">
      <header>
        <h2>Hypervisors &amp; Provider Credentials</h2>
        <p>Register the virtualization hosts and cloud accounts Control One provisions against. Multiple hosts per tenant across datacenters are supported.</p>
      </header>

      <div className="card" style={{ marginTop: '1rem' }}>
        <h3>Add hypervisor host</h3>
        <form onSubmit={handleCreateHost} style={{ display: 'grid', gap: '0.75rem', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' }}>
          <label>
            Tenant
            <select
              value={hostForm.tenant_id}
              onChange={(e) => setHostForm((f) => ({ ...f, tenant_id: e.target.value }))}
              required
            >
              <option value="">Select tenant…</option>
              {tenants?.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Provider
            <select
              value={hostForm.provider}
              onChange={(e) => setHostForm((f) => ({ ...f, provider: e.target.value as HypervisorProvider, credential_id: '' }))}
            >
              {PROVIDERS.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
          <label>
            Name
            <input
              type="text"
              value={hostForm.name}
              onChange={(e) => setHostForm((f) => ({ ...f, name: e.target.value }))}
              placeholder="lon-kvm-01"
              required
            />
          </label>
          <label>
            Endpoint URL
            <input
              type="text"
              value={hostForm.endpoint_url}
              onChange={(e) => setHostForm((f) => ({ ...f, endpoint_url: e.target.value }))}
              placeholder={hostForm.provider === 'libvirt' ? 'qemu+ssh://root@kvm-01/system' : 'https://vcenter.lon'}
              required
            />
          </label>
          <label>
            Datacenter (optional)
            <input
              type="text"
              value={hostForm.datacenter}
              onChange={(e) => setHostForm((f) => ({ ...f, datacenter: e.target.value }))}
              placeholder="lon-dc-1"
            />
          </label>
          <label>
            Credential
            <select
              value={hostForm.credential_id}
              onChange={(e) => setHostForm((f) => ({ ...f, credential_id: e.target.value }))}
            >
              <option value="">No credential (env-based)</option>
              {(credentialsByProvider[hostForm.provider] || []).map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </label>
          <label style={{ gridColumn: '1 / -1' }}>
            Labels (JSON, optional)
            <textarea
              rows={3}
              value={hostForm.labelsText}
              onChange={(e) => setHostForm((f) => ({ ...f, labelsText: e.target.value }))}
              placeholder='{"tier":"prod","region":"eu-west"}'
            />
          </label>
          <div style={{ gridColumn: '1 / -1', display: 'flex', gap: '0.5rem' }}>
            <button type="submit" disabled={submittingHost}>
              {submittingHost ? 'Saving…' : 'Add host'}
            </button>
            <button type="button" onClick={() => setHostForm(HOST_FORM_DEFAULT)} disabled={submittingHost}>
              Reset
            </button>
          </div>
        </form>
      </div>

      <div className="card" style={{ marginTop: '1rem' }}>
        <h3>Registered hosts</h3>
        {loading ? (
          <p>Loading…</p>
        ) : hosts.length === 0 ? (
          <p>No hypervisor hosts configured yet.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Provider</th>
                <th>Endpoint</th>
                <th>Datacenter</th>
                <th>Health</th>
                <th>Last verified</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {hosts.map((host) => (
                <tr key={host.id}>
                  <td>{host.name}</td>
                  <td>{host.provider}</td>
                  <td><code>{host.endpoint_url}</code></td>
                  <td>{host.datacenter ?? '—'}</td>
                  <td>
                    <span className={`badge status-${host.health_status}`}>{host.health_status}</span>
                    {host.health_message ? <div style={{ fontSize: '0.8em', opacity: 0.7 }}>{host.health_message}</div> : null}
                  </td>
                  <td>{formatDate(host.last_verified_at)}</td>
                  <td style={{ display: 'flex', gap: '0.5rem' }}>
                    <button type="button" onClick={() => handleVerify(host)} disabled={verifying === host.id}>
                      {verifying === host.id ? 'Verifying…' : 'Verify'}
                    </button>
                    <button type="button" onClick={() => handleDeleteHost(host)} className="danger">
                      Remove
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="card" style={{ marginTop: '1rem' }}>
        <h3>Add provider credential</h3>
        <p>
          Credentials are encrypted at rest with AES-256-GCM using the key in <code>secrets.encryption_key</code>. Never shown again after save — rotate by posting a
          new config.
        </p>
        <form onSubmit={handleCreateCredential} style={{ display: 'grid', gap: '0.75rem', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' }}>
          <label>
            Tenant
            <select
              value={credentialForm.tenant_id}
              onChange={(e) => setCredentialForm((f) => ({ ...f, tenant_id: e.target.value }))}
              required
            >
              <option value="">Select tenant…</option>
              {tenants?.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Provider
            <select
              value={credentialForm.provider}
              onChange={(e) => setCredentialForm((f) => ({ ...f, provider: e.target.value as HypervisorProvider }))}
            >
              {PROVIDERS.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
          <label>
            Name
            <input
              type="text"
              value={credentialForm.name}
              onChange={(e) => setCredentialForm((f) => ({ ...f, name: e.target.value }))}
              placeholder="kvm-root"
              required
            />
          </label>
          <label style={{ gridColumn: '1 / -1' }}>
            Config (JSON)
            <textarea
              rows={6}
              value={credentialForm.configText}
              onChange={(e) => setCredentialForm((f) => ({ ...f, configText: e.target.value }))}
              required
            />
          </label>
          <div style={{ gridColumn: '1 / -1', display: 'flex', gap: '0.5rem' }}>
            <button type="submit" disabled={submittingCred}>
              {submittingCred ? 'Saving…' : 'Save credential'}
            </button>
            <button type="button" onClick={() => setCredentialForm(CREDENTIAL_FORM_DEFAULT)} disabled={submittingCred}>
              Reset
            </button>
          </div>
        </form>
      </div>

      <div className="card" style={{ marginTop: '1rem' }}>
        <h3>Stored credentials</h3>
        {credentials.length === 0 ? (
          <p>No provider credentials configured yet.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Provider</th>
                <th>Created</th>
                <th>Rotated</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {credentials.map((cred) => (
                <tr key={cred.id}>
                  <td>{cred.name}</td>
                  <td>{cred.provider}</td>
                  <td>{formatDate(cred.created_at)}</td>
                  <td>{formatDate(cred.rotated_at)}</td>
                  <td>
                    <button type="button" onClick={() => handleDeleteCredential(cred)} className="danger">
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
