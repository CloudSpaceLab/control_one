import { useState, type FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useApiClient } from '../hooks/useApiClient';
import { useAuth } from '../providers/AuthProvider';
import { Alert, Panel } from './kit';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';

const queryKey = ['network-onboarding-policy'];

export function NetworkOnboardingPolicyPanel(): JSX.Element | null {
  const api = useApiClient();
  const cache = useQueryClient();
  const { profile } = useAuth();
  const isAdmin = profile?.roles.some(role => role.trim().toLowerCase() === 'admin') ?? false;
  const policy = useQuery({ queryKey, queryFn: () => api.getNetworkOnboardingPolicy(), enabled: isAdmin });
  const [entry, setEntry] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  if (!isAdmin) return null;

  const save = async (allowed: string[]): Promise<boolean> => {
    setSaving(true); setError(''); setSaved(false);
    try {
      const result = await api.updateNetworkOnboardingPolicy({ allowed_cidrs: allowed });
      cache.setQueryData(queryKey, result);
      await cache.invalidateQueries({ queryKey });
      setSaved(true);
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to update allowed device networks.');
      return false;
    } finally { setSaving(false); }
  };

  const add = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const value = entry.trim();
    if (!value || saving) return;
    if (await save([...(policy.data?.allowed_cidrs ?? []), value])) setEntry('');
  };

  return <Panel title="Allowed device networks" eyebrow="NETWORK ONBOARDING POLICY" toneAccent="warning">
    <p className="text-sm text-text-secondary">Only these destination IPs and CIDR blocks can be contacted by read-only onboarding tests and SNMP inventory refreshes. This setting applies across tenants and requires administrator access.</p>
    {policy.isLoading && <p role="status" className="text-sm">Loading network access policy…</p>}
    {policy.error && <Alert variant="critical" title="Policy unavailable">{policy.error instanceof Error ? policy.error.message : 'Unable to load allowed device networks.'}</Alert>}
    {error && <Alert variant="critical" title="Could not save policy">{error}</Alert>}
    {saved && <Alert variant="success" title="Policy saved">New device connection tests will use this allowlist immediately.</Alert>}
    {policy.data && <>
      <p className="text-xs text-text-secondary">Current source: {policy.data.source === 'database' ? 'saved in this Control One database' : 'server configuration'}.</p>
      {policy.data.allowed_cidrs.length === 0
        ? <Alert variant="warning" title="No device destinations allowed">Add a trusted device IP or CIDR block before testing a network device.</Alert>
        : <ul aria-label="Allowed device IPs and CIDR blocks" className="flex flex-wrap gap-2">{policy.data.allowed_cidrs.map(cidr => <li key={cidr} className="flex items-center gap-2 rounded-md border border-border-subtle px-3 py-2 font-mono text-sm"><span>{cidr}</span><button type="button" aria-label={`Remove ${cidr}`} className="text-text-secondary hover:text-foreground" disabled={saving} onClick={() => void save(policy.data!.allowed_cidrs.filter(value => value !== cidr))}>×</button></li>)}</ul>}
      <form onSubmit={add} className="flex flex-col gap-2 sm:flex-row sm:items-end">
        <div className="flex-1"><Label htmlFor="network-onboarding-allowlist-entry">Device IP or CIDR block</Label><Input id="network-onboarding-allowlist-entry" value={entry} onChange={event => setEntry(event.target.value)} placeholder="192.168.56.10 or 192.168.56.0/24" disabled={saving || policy.isLoading} autoComplete="off" /><p className="mt-1 text-xs text-text-secondary">A single IP is saved as a host-only /32 or /128 rule. Avoid broad ranges; the app also blocks loopback and link-local destinations.</p></div>
        <Button type="submit" disabled={saving || policy.isLoading || !entry.trim()}>{saving ? 'Saving…' : 'Add allowed IP / CIDR'}</Button>
      </form>
    </>}
  </Panel>;
}
