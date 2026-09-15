import { useEffect, useState, type FormEvent } from 'react';
import { Panel } from '@/components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { useAuth } from '@/providers/AuthProvider';
import type { SMTPSettings, UpdateSMTPSettings } from '@/lib/api';

export function SMTPSettingsTab(): JSX.Element {
  const { currentTenantId } = useTenant();
  const { profile } = useAuth();
  const admin = profile?.roles.some(role => role.trim().toLowerCase() === 'admin');
  if (!admin) return <Panel title="Email alerts"><p>Administrator access is required to manage SMTP settings.</p></Panel>;
  if (!currentTenantId) return <Panel title="Email alerts"><p>Select a tenant to configure SMTP.</p></Panel>;
  // Remount on tenant changes so unsaved credentials and pending results cannot cross tenants.
  return <SMTPForm key={currentTenantId} tenantId={currentTenantId} />;
}

function SMTPForm({ tenantId }: { tenantId: string }): JSX.Element {
  const client = useApiClient();
  const [config, setConfig] = useState<SMTPSettings | null>(null);
  const [form, setForm] = useState<UpdateSMTPSettings | null>(null);
  const [password, setPassword] = useState('');
  const [clearPassword, setClearPassword] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true); setError('');
    client.getSMTPSettings(tenantId).then(value => {
      if (cancelled) return;
      setConfig(value);
      const { host, port, tls_mode, auth_enabled, username, sender_name, sender_email, enabled } = value;
      setForm({ host, port, tls_mode, auth_enabled, username, sender_name, sender_email, enabled });
    }).catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Unable to load SMTP settings.'); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [client, tenantId, reload]);

  const change = (patch: Partial<UpdateSMTPSettings>) => { setForm(previous => previous && { ...previous, ...patch }); setSaved(false); };
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!form) return;
    setSaving(true); setError(''); setSaved(false);
    try {
      const value = await client.updateSMTPSettings(tenantId, {
        ...form, ...(!form.auth_enabled && clearPassword ? { password: '' } : form.auth_enabled && password ? { password } : {}),
      });
      setConfig(value); setPassword(''); setClearPassword(false); setSaved(true);
    } catch (err) { setError(err instanceof Error ? err.message : 'Unable to save SMTP settings.'); }
    finally { setSaving(false); }
  };

  return <Panel title="Email alerts" eyebrow="SMTP" className="max-w-3xl">
    <p className="text-sm text-muted-foreground">Configure the outgoing mail server for this tenant. Recipient lists and email delivery will be available in the next phase.</p>
    {error && <p role="alert" className="text-sm text-red-500">{error}</p>}
    {loading ? <p role="status">Loading SMTP settings…</p> : !form || !config ? <Button onClick={() => setReload(value => value + 1)}>Retry</Button> :
      <form onSubmit={save} className="space-y-5">
        {!config.encryption_available && <p role="status" className="text-sm text-amber-500">Credential encryption is unavailable. Ask your administrator to configure the server encryption key before saving a password.</p>}
        <fieldset disabled={saving} className="space-y-5">
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.enabled} onChange={event => change({ enabled: event.target.checked })} />Enable email integration</label>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="smtp-host">SMTP host</Label><Input id="smtp-host" required maxLength={253} placeholder="smtp.example.com" value={form.host} onChange={event => change({ host: event.target.value })} /></div>
            <div className="space-y-2"><Label htmlFor="smtp-port">Port</Label><Input id="smtp-port" type="number" min={1} max={65535} required value={form.port || ''} onChange={event => change({ port: Number(event.target.value) })} /></div>
            <div className="space-y-2"><Label htmlFor="smtp-tls">Connection security</Label><select id="smtp-tls" className="w-full rounded-md border bg-background p-2 text-sm" value={form.tls_mode} onChange={event => change({ tls_mode: event.target.value as SMTPSettings['tls_mode'] })}><option value="starttls">STARTTLS (usually port 587)</option><option value="tls">TLS (usually port 465)</option><option value="none" disabled={form.auth_enabled}>None (trusted relay only)</option></select></div>
            <div className="space-y-2"><Label htmlFor="smtp-sender-name">Sender name</Label><Input id="smtp-sender-name" maxLength={200} placeholder="Control One" value={form.sender_name} onChange={event => change({ sender_name: event.target.value })} /></div>
            <div className="space-y-2 sm:col-span-2"><Label htmlFor="smtp-sender-email">Sender email</Label><Input id="smtp-sender-email" type="email" required maxLength={254} placeholder="alerts@example.com" value={form.sender_email} onChange={event => change({ sender_email: event.target.value })} /></div>
          </div>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.auth_enabled} onChange={event => change({ auth_enabled: event.target.checked, ...(event.target.checked && form.tls_mode === 'none' ? { tls_mode: 'starttls' as const } : {}) })} />SMTP authentication</label>
          {form.auth_enabled && <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="smtp-username">Username</Label><Input id="smtp-username" autoComplete="off" required maxLength={320} value={form.username} onChange={event => change({ username: event.target.value })} /></div>
            <div className="space-y-2"><Label htmlFor="smtp-password">Password</Label><Input id="smtp-password" type="password" autoComplete="new-password" maxLength={4096} required={!config.password_configured} value={password} onChange={event => { setPassword(event.target.value); setSaved(false); }} /><p className="text-xs text-muted-foreground">{config.password_configured ? 'Password saved. Leave blank to keep it.' : 'Enter the SMTP password or app password.'}</p></div>
          </div>}
          {!form.auth_enabled && config.password_configured && <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={clearPassword} onChange={event => { setClearPassword(event.target.checked); setSaved(false); }} />Remove saved password</label>}
          <Button type="submit" disabled={saving || (form.auth_enabled && !config.encryption_available)}>{saving ? 'Saving…' : 'Save SMTP settings'}</Button>
        </fieldset>
        {saved && <p role="status" className="text-sm">SMTP settings saved. No email has been sent.</p>}
      </form>}
  </Panel>;
}
