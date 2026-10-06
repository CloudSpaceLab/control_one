import { useEffect, useState, type FormEvent } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { Alert } from './kit';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import type { NetworkConnectionReceipt, NetworkCredentialConfig, NetworkTarget } from '../lib/api';

const types = ['router', 'switch', 'firewall', 'load_balancer', 'waf', 'vpn_gateway', 'wireless_controller', 'access_point', 'ids_ips', 'network_appliance'];
const selectClass = 'h-10 w-full rounded-md border border-border-subtle bg-surface px-3 text-sm';
const failure = (error: unknown) => error instanceof Error ? error.message : 'Unable to complete network onboarding.';

export function NetworkDeviceWizard({ onSaved, onCancel, onBusyChange }: { onSaved: (target: NetworkTarget) => void; onCancel: () => void; onBusyChange?: (busy: boolean) => void }): JSX.Element {
  const api = useApiClient();
  const { tenants, currentTenantId } = useTenant();
  const [tenant, setTenant] = useState(currentTenantId ?? '');
  const [name, setName] = useState('');
  const [address, setAddress] = useState('');
  const [site, setSite] = useState('');
  const [group, setGroup] = useState('');
  const [protocol, setProtocol] = useState<'snmpv3' | 'ssh'>('snmpv3');
  const [port, setPort] = useState('161');
  const [receipt, setReceipt] = useState<NetworkConnectionReceipt | null>(null);
  const [credentialId, setCredentialId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [type, setType] = useState('network_appliance');
  const [reviewed, setReviewed] = useState(false);
  const [selectedSource, setSelectedSource] = useState(false);
  const [review, setReview] = useState(false);
  const [sshAuth, setSshAuth] = useState('password');
  const [saveAttempted, setSaveAttempted] = useState(false);
  useEffect(() => { onBusyChange?.(busy); }, [busy, onBusyChange]);
  useEffect(() => () => { onBusyChange?.(false); }, [onBusyChange]);

  async function test(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const values = new FormData(form);
    const value = (key: string) => String(values.get(key) ?? '');
    setBusy(true); setError(null); setReceipt(null); setReviewed(false); setSaveAttempted(false);
    try {
      let reference = credentialId;
      if (!reference) {
        const config: NetworkCredentialConfig = protocol === 'snmpv3'
          ? { username: value('username'), auth_protocol: value('auth_protocol'), auth_secret: value('auth_secret'), priv_protocol: 'AES', priv_secret: value('priv_secret') }
          : { username: value('username'), host_key_fingerprint: value('host_key_fingerprint'), ...(sshAuth === 'password' ? { password: value('password') } : { private_key: value('private_key'), passphrase: value('passphrase') }) };
        // getRandomValues also works on the existing HTTP LAN dev console.
        const suffix = Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('');
        const saved = await api.createNetworkCredential({ tenant_id: tenant, protocol, name: `${name.slice(0,50)} ${suffix}`, config });
        reference = saved.id; setCredentialId(reference);
        // Clear secret input fields once encrypted storage acknowledges receipt.
        for (const key of ['auth_secret', 'priv_secret', 'password', 'private_key', 'passphrase']) {
          const field = form.elements.namedItem(key);
          if (field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement) field.value = '';
        }
      }
      const result = await api.testNetworkConnection({ tenant_id: tenant, credential_id: reference, address, port: Number(port) });
      setReceipt(result);
      if (result.state === 'authenticated') {
        setType(result.result.suggested_type); setSelectedSource(false); setReview(true);
      }
    } catch (err) { setError(failure(err)); }
    finally { setBusy(false); }
  }

  async function save() {
    if (!receipt || !reviewed || busy) return;
    setBusy(true); setError(null); setSaveAttempted(true);
    try {
      const target = await api.saveNetworkOnboarding({ test_id: receipt.id, display_name: name, type, site, group, telemetry_sources: selectedSource ? [protocol === 'snmpv3' ? 'snmp_identity' : 'ssh_identity'] : [] });
      onSaved(target);
    } catch (err) { setError(failure(err)); }
    finally { setBusy(false); }
  }

  if (review && receipt) return <div className="flex flex-col gap-4">
    <h3 className="font-semibold">2. Review identity and verification</h3>
    <Alert variant="success" title="Authenticated">{receipt.result.message}</Alert>
    <dl className="grid grid-cols-2 gap-3 text-sm">{[['Vendor', receipt.result.vendor || 'Not detected'], ['Model', receipt.result.model || 'Not detected'], ['Platform', receipt.result.platform || 'Not detected'], ['Classification confidence', `${receipt.result.confidence}%`]].map(([label, value]) => <div key={label}><dt className="text-text-secondary">{label}</dt><dd>{value}</dd></div>)}</dl>
    {receipt.result.confidence < 80 && <Alert variant="warning" title="Classification needs review">Select the correct device type. An operator override preserves the protocol evidence and its original confidence.</Alert>}
    <div><Label htmlFor="review-type">Device type</Label><select id="review-type" className={selectClass} value={type} disabled={busy || saveAttempted} onChange={(event) => { setType(event.target.value); setReviewed(false); }}>{types.map((key) => <option key={key} value={key}>{key.replaceAll('_', ' ')}</option>)}</select></div>
    <ul className="list-disc pl-5 text-sm">{receipt.result.evidence.map((evidence) => <li key={evidence}>{evidence}</li>)}</ul>
    <p className="text-sm">Required privileges: {receipt.result.required_privileges}</p>
    <p className="text-sm">Detected capabilities: {receipt.result.capabilities.map((capability) => capability.replaceAll('_', ' ')).join(', ') || 'None detected'}.</p>
    <h3 className="font-semibold">Telemetry sources</h3>
    <label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={selectedSource} disabled={busy || saveAttempted} onChange={(event) => setSelectedSource(event.target.checked)} />Keep {protocol === 'snmpv3' ? 'SNMP identity' : 'SSH identity'} source selection</label>
    <p className="text-xs text-text-secondary">This test verified a one-time identity read. Source selection is saved for configuration; recurring polling is not enabled by this step. Syslog and flow are not configured.</p>
    <label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={reviewed} disabled={busy} onChange={(event) => setReviewed(event.target.checked)} />I reviewed the classification and connection result.</label>
    {error && <Alert variant="critical" title="Unable to save">{error}</Alert>}
    <div className="flex justify-end gap-2"><Button variant="secondary" disabled={busy} onClick={() => { setReview(false); setReceipt(null); }}>Back to connection</Button><Button disabled={busy || !reviewed} onClick={save}>{busy ? 'Saving…' : 'Save verified device'}</Button></div>
  </div>;

  return <form onSubmit={test} className="flex flex-col gap-4">
    <h3 className="font-semibold">1. Connection and credential</h3>
    <fieldset disabled={busy} className="flex flex-col gap-4">
      <div><Label htmlFor="wizard-tenant">Tenant</Label><select id="wizard-tenant" className={selectClass} required value={tenant} onChange={(event) => { setTenant(event.target.value); setCredentialId(null); setReceipt(null); }}><option value="">Choose tenant</option>{tenants.filter((item) => !currentTenantId || item.id === currentTenantId).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></div>
      <div><Label htmlFor="wizard-name">Device name</Label><Input id="wizard-name" required maxLength={255} value={name} onChange={(event) => setName(event.target.value)} /></div>
      <div className="grid gap-3 sm:grid-cols-2"><div><Label htmlFor="wizard-address">Management address</Label><Input id="wizard-address" required maxLength={253} value={address} placeholder="IP address or DNS name" onChange={(event) => { setAddress(event.target.value); setReceipt(null); }} /></div><div><Label htmlFor="wizard-port">Port</Label><Input id="wizard-port" type="number" min={1} max={65535} required value={port} onChange={(event) => { setPort(event.target.value); setReceipt(null); }} /></div></div>
      <div className="grid gap-3 sm:grid-cols-2"><div><Label htmlFor="wizard-site">Site (optional)</Label><Input id="wizard-site" maxLength={255} value={site} onChange={(event) => setSite(event.target.value)} /></div><div><Label htmlFor="wizard-group">Group (optional)</Label><Input id="wizard-group" maxLength={255} value={group} onChange={(event) => setGroup(event.target.value)} /></div></div>
      <div><Label htmlFor="wizard-protocol">Protocol</Label><select id="wizard-protocol" className={selectClass} value={protocol} onChange={(event) => { const next = event.target.value as 'snmpv3' | 'ssh'; setProtocol(next); setPort(next === 'ssh' ? '22' : '161'); setCredentialId(null); setReceipt(null); }}><option value="snmpv3">SNMPv3 (authPriv)</option><option value="ssh">SSH (read-only)</option><option disabled>NETCONF / RESTCONF / vendor API (snapshot sources; not for identity test)</option></select><p className="text-xs text-text-secondary">This test verifies device identity over SNMPv3 or SSH. Configure snapshot sources separately under Network telemetry sources on the saved device.</p></div>
      {credentialId ? <Alert variant="info" title="Encrypted credential saved">Tests use the saved secret reference. <Button type="button" variant="ghost" onClick={() => { setCredentialId(null); setReceipt(null); }}>Use another credential</Button></Alert> : <>
        <div><Label htmlFor="wizard-username">Read-only username</Label><Input id="wizard-username" name="username" autoComplete="off" required maxLength={128} /></div>
        {protocol === 'snmpv3' ? <>
          <p className="text-sm text-text-secondary">Requires read-only sysDescr/sysObjectID access. Privacy uses AES.</p>
          <div><Label htmlFor="wizard-auth-protocol">Authentication algorithm</Label><select id="wizard-auth-protocol" name="auth_protocol" className={selectClass} defaultValue="SHA256"><option value="SHA256">SHA256</option><option value="SHA">SHA (compatibility)</option></select></div>
          <div><Label htmlFor="wizard-auth-secret">Authentication secret</Label><Input id="wizard-auth-secret" name="auth_secret" type="password" autoComplete="new-password" required minLength={8} maxLength={255} /></div>
          <div><Label htmlFor="wizard-priv-secret">Privacy secret</Label><Input id="wizard-priv-secret" name="priv_secret" type="password" autoComplete="new-password" required minLength={8} maxLength={255} /></div>
        </> : <>
          <p className="text-sm text-text-secondary">Requires read-only show version permission. No enable/config privileges.</p>
          <div><Label htmlFor="wizard-host-key">Trusted SSH host-key fingerprint</Label><Input id="wizard-host-key" name="host_key_fingerprint" required placeholder="SHA256:…" /><p className="text-xs text-text-secondary">Use the fingerprint verified through your device administrator.</p></div>
          <div><Label htmlFor="wizard-ssh-auth">SSH authentication</Label><select id="wizard-ssh-auth" className={selectClass} value={sshAuth} onChange={(event) => setSshAuth(event.target.value)}><option value="password">Password</option><option value="key">Private key</option></select></div>
          {sshAuth === 'password' ? <div><Label htmlFor="wizard-password">SSH password</Label><Input id="wizard-password" name="password" type="password" autoComplete="new-password" required /></div> : <><div><Label htmlFor="wizard-private-key">SSH private key</Label><textarea id="wizard-private-key" name="private_key" className={`${selectClass} h-24 font-mono`} required maxLength={16384} /></div><div><Label htmlFor="wizard-passphrase">Key passphrase (optional)</Label><Input id="wizard-passphrase" name="passphrase" type="password" autoComplete="new-password" /></div></>}
        </>}
      </>}
    </fieldset>
    {receipt && <Alert variant="warning" title={receipt.state.replaceAll('_', ' ')}>{receipt.result.message}</Alert>}
    {error && <Alert variant="critical" title="Connection test failed">{error}</Alert>}
    <div className="flex justify-end gap-2"><Button type="button" variant="secondary" disabled={busy} onClick={onCancel}>Cancel</Button><Button type="submit" disabled={busy}>{busy ? 'Testing…' : 'Test connection'}</Button></div>
  </form>;
}
