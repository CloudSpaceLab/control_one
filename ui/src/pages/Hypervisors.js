import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
const PROVIDERS = ['libvirt', 'vmware', 'aws', 'azure'];
function formatDate(value) {
    if (!value)
        return '—';
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}
// PROVIDER_FIELDS describes the visual form per provider. Keys match what the
// Go adapters expect inside the credential JSON. Adding a new provider is a
// matter of adding an entry here and wiring the adapter on the backend.
const PROVIDER_FIELDS = {
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
function defaultFieldsFor(provider) {
    const out = {};
    PROVIDER_FIELDS[provider].forEach((f) => {
        out[f.key] = '';
    });
    return out;
}
function fieldsToConfigText(fields) {
    const trimmed = {};
    Object.entries(fields).forEach(([k, v]) => {
        if (v && v.trim() !== '')
            trimmed[k] = v;
    });
    return JSON.stringify(trimmed, null, 2);
}
const CREDENTIAL_FORM_DEFAULT = {
    tenant_id: '',
    provider: 'libvirt',
    name: '',
    configText: '',
    showRawJSON: false,
    fields: defaultFieldsFor('libvirt'),
};
const HOST_FORM_DEFAULT = {
    tenant_id: '',
    provider: 'libvirt',
    name: '',
    endpoint_url: '',
    credential_id: '',
    datacenter: '',
    labelsText: '',
};
export function Hypervisors() {
    const api = useApiClient();
    const { data: tenants } = useTenants();
    const { showToast } = useToast();
    const [credentials, setCredentials] = useState([]);
    const [hosts, setHosts] = useState([]);
    const [loading, setLoading] = useState(true);
    const [verifying, setVerifying] = useState(null);
    const [credentialForm, setCredentialForm] = useState(CREDENTIAL_FORM_DEFAULT);
    const [hostForm, setHostForm] = useState(HOST_FORM_DEFAULT);
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
        }
        catch (err) {
            showToast(`Failed to load hypervisors: ${String(err)}`, 'error');
        }
        finally {
            setLoading(false);
        }
    }, [api, showToast]);
    useEffect(() => {
        void refresh();
    }, [refresh]);
    const credentialsByProvider = useMemo(() => {
        const grouped = { libvirt: [], vmware: [], aws: [], azure: [] };
        for (const c of credentials) {
            if (grouped[c.provider])
                grouped[c.provider].push(c);
        }
        return grouped;
    }, [credentials]);
    async function handleCreateCredential(event) {
        event.preventDefault();
        if (submittingCred)
            return;
        let config;
        if (credentialForm.showRawJSON) {
            try {
                config = JSON.parse(credentialForm.configText);
            }
            catch {
                showToast('Invalid credential config: must be valid JSON', 'error');
                return;
            }
        }
        else {
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
                if (v && v.trim() !== '')
                    config[k] = v;
            });
        }
        if (!credentialForm.tenant_id || !credentialForm.name) {
            showToast('Tenant and name required', 'error');
            return;
        }
        setSubmittingCred(true);
        try {
            const payload = {
                tenant_id: credentialForm.tenant_id,
                provider: credentialForm.provider,
                name: credentialForm.name,
                config,
            };
            await api.createProviderCredential(payload);
            showToast(`Credential saved: ${credentialForm.name}`, 'success');
            setCredentialForm(CREDENTIAL_FORM_DEFAULT);
            await refresh();
        }
        catch (err) {
            showToast(`Failed to save credential: ${String(err)}`, 'error');
        }
        finally {
            setSubmittingCred(false);
        }
    }
    async function handleCreateHost(event) {
        event.preventDefault();
        if (submittingHost)
            return;
        if (!hostForm.tenant_id || !hostForm.name || !hostForm.endpoint_url) {
            showToast('Tenant, name, and endpoint are required', 'error');
            return;
        }
        let labels;
        if (hostForm.labelsText.trim()) {
            try {
                labels = JSON.parse(hostForm.labelsText);
            }
            catch {
                showToast('Invalid labels JSON: must be a valid object', 'error');
                return;
            }
        }
        setSubmittingHost(true);
        try {
            const payload = {
                tenant_id: hostForm.tenant_id,
                provider: hostForm.provider,
                name: hostForm.name,
                endpoint_url: hostForm.endpoint_url,
            };
            if (hostForm.credential_id)
                payload.credential_id = hostForm.credential_id;
            if (hostForm.datacenter)
                payload.datacenter = hostForm.datacenter;
            if (labels)
                payload.labels = labels;
            await api.createHypervisorHost(payload);
            showToast(`Hypervisor host added: ${hostForm.name}`, 'success');
            setHostForm(HOST_FORM_DEFAULT);
            await refresh();
        }
        catch (err) {
            showToast(`Failed to add host: ${String(err)}`, 'error');
        }
        finally {
            setSubmittingHost(false);
        }
    }
    async function handleVerify(host) {
        setVerifying(host.id);
        try {
            const resp = await api.verifyHypervisorHost(host.id);
            showToast(resp.status === 'ok' ? `Host reachable: ${host.endpoint_url}` : `Host unreachable: ${resp.message ?? host.endpoint_url}`, resp.status === 'ok' ? 'success' : 'error');
            await refresh();
        }
        catch (err) {
            showToast(`Verify failed: ${String(err)}`, 'error');
        }
        finally {
            setVerifying(null);
        }
    }
    async function handleDeleteHost(host) {
        if (!window.confirm(`Remove hypervisor host "${host.name}"? Clusters referencing it will be detached.`))
            return;
        try {
            await api.deleteHypervisorHost(host.id);
            showToast(`Host removed: ${host.name}`, 'success');
            await refresh();
        }
        catch (err) {
            showToast(`Failed to remove host: ${String(err)}`, 'error');
        }
    }
    async function handleDeleteCredential(cred) {
        if (!window.confirm(`Delete credential "${cred.name}"? Hosts referencing it will lose their credential_id.`))
            return;
        try {
            await api.deleteProviderCredential(cred.id);
            showToast(`Credential removed: ${cred.name}`, 'success');
            await refresh();
        }
        catch (err) {
            showToast(`Failed to remove credential: ${String(err)}`, 'error');
        }
    }
    return (_jsxs("section", { className: "page", children: [_jsxs("header", { children: [_jsx("h2", { children: "Hypervisors & Provider Credentials" }), _jsx("p", { children: "Register the virtualization hosts and cloud accounts Control One provisions against. Multiple hosts per tenant across datacenters are supported." })] }), _jsxs("div", { className: "card", style: { marginTop: '1rem' }, children: [_jsx("h3", { children: "Add hypervisor host" }), _jsxs("form", { onSubmit: handleCreateHost, style: { display: 'grid', gap: '0.75rem', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' }, children: [_jsxs("label", { children: ["Tenant", _jsxs("select", { value: hostForm.tenant_id, onChange: (e) => setHostForm((f) => ({ ...f, tenant_id: e.target.value })), required: true, children: [_jsx("option", { value: "", children: "Select tenant\u2026" }), tenants?.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id)))] })] }), _jsxs("label", { children: ["Provider", _jsx("select", { value: hostForm.provider, onChange: (e) => setHostForm((f) => ({ ...f, provider: e.target.value, credential_id: '' })), children: PROVIDERS.map((p) => (_jsx("option", { value: p, children: p }, p))) })] }), _jsxs("label", { children: ["Name", _jsx("input", { type: "text", value: hostForm.name, onChange: (e) => setHostForm((f) => ({ ...f, name: e.target.value })), placeholder: "lon-kvm-01", required: true })] }), _jsxs("label", { children: ["Endpoint URL", _jsx("input", { type: "text", value: hostForm.endpoint_url, onChange: (e) => setHostForm((f) => ({ ...f, endpoint_url: e.target.value })), placeholder: hostForm.provider === 'libvirt' ? 'qemu+ssh://root@kvm-01/system' : 'https://vcenter.lon', required: true })] }), _jsxs("label", { children: ["Datacenter (optional)", _jsx("input", { type: "text", value: hostForm.datacenter, onChange: (e) => setHostForm((f) => ({ ...f, datacenter: e.target.value })), placeholder: "lon-dc-1" })] }), _jsxs("label", { children: ["Credential", _jsxs("select", { value: hostForm.credential_id, onChange: (e) => setHostForm((f) => ({ ...f, credential_id: e.target.value })), children: [_jsx("option", { value: "", children: "No credential (env-based)" }), (credentialsByProvider[hostForm.provider] || []).map((c) => (_jsx("option", { value: c.id, children: c.name }, c.id)))] })] }), _jsxs("label", { style: { gridColumn: '1 / -1' }, children: ["Labels (JSON, optional)", _jsx("textarea", { rows: 3, value: hostForm.labelsText, onChange: (e) => setHostForm((f) => ({ ...f, labelsText: e.target.value })), placeholder: '{"tier":"prod","region":"eu-west"}' })] }), _jsxs("div", { style: { gridColumn: '1 / -1', display: 'flex', gap: '0.5rem' }, children: [_jsx("button", { type: "submit", disabled: submittingHost, children: submittingHost ? 'Saving…' : 'Add host' }), _jsx("button", { type: "button", onClick: () => setHostForm(HOST_FORM_DEFAULT), disabled: submittingHost, children: "Reset" })] })] })] }), _jsxs("div", { className: "card", style: { marginTop: '1rem' }, children: [_jsx("h3", { children: "Registered hosts" }), loading ? (_jsx("p", { children: "Loading\u2026" })) : hosts.length === 0 ? (_jsx("p", { children: "No hypervisor hosts configured yet." })) : (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Provider" }), _jsx("th", { children: "Endpoint" }), _jsx("th", { children: "Datacenter" }), _jsx("th", { children: "Health" }), _jsx("th", { children: "Last verified" }), _jsx("th", {})] }) }), _jsx("tbody", { children: hosts.map((host) => (_jsxs("tr", { children: [_jsx("td", { children: host.name }), _jsx("td", { children: host.provider }), _jsx("td", { children: _jsx("code", { children: host.endpoint_url }) }), _jsx("td", { children: host.datacenter ?? '—' }), _jsxs("td", { children: [_jsx("span", { className: `badge status-${host.health_status}`, children: host.health_status }), host.health_message ? _jsx("div", { style: { fontSize: '0.8em', opacity: 0.7 }, children: host.health_message }) : null] }), _jsx("td", { children: formatDate(host.last_verified_at) }), _jsxs("td", { style: { display: 'flex', gap: '0.5rem' }, children: [_jsx("button", { type: "button", onClick: () => handleVerify(host), disabled: verifying === host.id, children: verifying === host.id ? 'Verifying…' : 'Verify' }), _jsx("button", { type: "button", onClick: () => handleDeleteHost(host), className: "danger", children: "Remove" })] })] }, host.id))) })] }))] }), _jsxs("div", { className: "card", style: { marginTop: '1rem' }, children: [_jsx("h3", { children: "Add provider credential" }), _jsxs("p", { children: ["Credentials are encrypted at rest with AES-256-GCM using the key in ", _jsx("code", { children: "secrets.encryption_key" }), ". Never shown again after save \u2014 rotate by posting a new config."] }), _jsxs("form", { onSubmit: handleCreateCredential, style: { display: 'grid', gap: '0.75rem', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' }, children: [_jsxs("label", { children: ["Tenant", _jsxs("select", { value: credentialForm.tenant_id, onChange: (e) => setCredentialForm((f) => ({ ...f, tenant_id: e.target.value })), required: true, children: [_jsx("option", { value: "", children: "Select tenant\u2026" }), tenants?.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id)))] })] }), _jsxs("label", { children: ["Provider", _jsx("select", { value: credentialForm.provider, onChange: (e) => {
                                            const next = e.target.value;
                                            setCredentialForm((f) => ({
                                                ...f,
                                                provider: next,
                                                fields: defaultFieldsFor(next),
                                                configText: '',
                                            }));
                                        }, children: PROVIDERS.map((p) => (_jsx("option", { value: p, children: p }, p))) })] }), _jsxs("label", { children: ["Name", _jsx("input", { type: "text", value: credentialForm.name, onChange: (e) => setCredentialForm((f) => ({ ...f, name: e.target.value })), placeholder: "kvm-root", required: true })] }), !credentialForm.showRawJSON ? (_jsx("div", { style: { gridColumn: '1 / -1', display: 'grid', gap: '0.6rem', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }, children: PROVIDER_FIELDS[credentialForm.provider].map((spec) => (_jsxs("label", { style: spec.type === 'textarea' ? { gridColumn: '1 / -1' } : undefined, children: [spec.label, spec.required ? ' *' : '', spec.type === 'textarea' ? (_jsx("textarea", { rows: 4, value: credentialForm.fields[spec.key] ?? '', onChange: (e) => setCredentialForm((f) => ({ ...f, fields: { ...f.fields, [spec.key]: e.target.value } })), placeholder: spec.placeholder, autoComplete: "off" })) : (_jsx("input", { type: spec.type, value: credentialForm.fields[spec.key] ?? '', onChange: (e) => setCredentialForm((f) => ({ ...f, fields: { ...f.fields, [spec.key]: e.target.value } })), placeholder: spec.placeholder, required: spec.required, autoComplete: spec.type === 'password' ? 'new-password' : 'off' })), spec.helper ? _jsx("small", { className: "muted", children: spec.helper }) : null] }, spec.key))) })) : (_jsxs("label", { style: { gridColumn: '1 / -1' }, children: ["Config (raw JSON)", _jsx("textarea", { rows: 6, value: credentialForm.configText || fieldsToConfigText(credentialForm.fields), onChange: (e) => setCredentialForm((f) => ({ ...f, configText: e.target.value })), required: true })] })), _jsx("div", { style: { gridColumn: '1 / -1' }, children: _jsx("button", { type: "button", className: "secondary-button", onClick: () => setCredentialForm((f) => ({
                                        ...f,
                                        showRawJSON: !f.showRawJSON,
                                        configText: f.showRawJSON ? '' : fieldsToConfigText(f.fields),
                                    })), children: credentialForm.showRawJSON ? 'Use visual form' : 'Edit raw JSON' }) }), _jsxs("div", { style: { gridColumn: '1 / -1', display: 'flex', gap: '0.5rem' }, children: [_jsx("button", { type: "submit", disabled: submittingCred, children: submittingCred ? 'Saving…' : 'Save credential' }), _jsx("button", { type: "button", onClick: () => setCredentialForm(CREDENTIAL_FORM_DEFAULT), disabled: submittingCred, children: "Reset" })] })] })] }), _jsxs("div", { className: "card", style: { marginTop: '1rem' }, children: [_jsx("h3", { children: "Stored credentials" }), credentials.length === 0 ? (_jsx("p", { children: "No provider credentials configured yet." })) : (_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Provider" }), _jsx("th", { children: "Created" }), _jsx("th", { children: "Rotated" }), _jsx("th", {})] }) }), _jsx("tbody", { children: credentials.map((cred) => (_jsxs("tr", { children: [_jsx("td", { children: cred.name }), _jsx("td", { children: cred.provider }), _jsx("td", { children: formatDate(cred.created_at) }), _jsx("td", { children: formatDate(cred.rotated_at) }), _jsx("td", { children: _jsx("button", { type: "button", onClick: () => handleDeleteCredential(cred), className: "danger", children: "Delete" }) })] }, cred.id))) })] }))] })] }));
}
