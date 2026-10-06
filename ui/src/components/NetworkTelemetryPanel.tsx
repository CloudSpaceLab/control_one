import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { useApiClient } from '../hooks/useApiClient';
import { Alert } from './kit';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import type { NetworkSourceConfig } from '../lib/api';

const sourceNames: Record<string, string> = { snmp_poll: 'SNMP polling', snmp_trap: 'SNMP traps', syslog: 'Syslog', netflow: 'NetFlow', ipfix: 'IPFIX', sflow: 'sFlow', ssh_config: 'SSH config snapshot', netconf: 'NETCONF', restconf: 'RESTCONF', vendor_api: 'Vendor API' };
const sourceTypes = Object.keys(sourceNames);
const receiverTypes = ['syslog', 'snmp_trap', 'netflow', 'ipfix', 'sflow'];
const when = (value?: string) => value ? new Date(value).toLocaleString() : 'Never';
const stateName = (value: string) => value.replaceAll('_', ' ');

export function NetworkTelemetryPanel({ targetId, tenantId, site = '', canConfigure = false }: { targetId: string; tenantId: string; site?: string; canConfigure?: boolean }): JSX.Element {
  const api = useApiClient();
  const cache = useQueryClient();
  const telemetry = useQuery({ queryKey: ['network-telemetry', tenantId, targetId], queryFn: () => api.getNetworkTelemetry(targetId), refetchInterval: 30000 });
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [config, setConfig] = useState<NetworkSourceConfig>({ source_type: 'syslog', collector_id: '', site, sender_address: '', stale_after_seconds: 300 });
  async function save() {
    setBusy(true); setError(null);
    try {
      await api.configureNetworkSource(targetId, { ...config, sender_address: receiverTypes.includes(config.source_type) ? config.sender_address : '' });
      await cache.invalidateQueries({ queryKey: ['network-telemetry', tenantId, targetId] });
      setEditing(false);
    } catch (e) { setError(e instanceof Error ? e.message : 'Unable to bind source.'); }
    finally { setBusy(false); }
  }
  return <section aria-label="Network telemetry sources" className="grid gap-3 rounded border border-border-subtle p-4">
    <div className="flex justify-between gap-3"><h3 className="font-semibold">Network telemetry sources</h3><Button variant="secondary" onClick={() => void telemetry.refetch()} disabled={telemetry.isFetching}>Refresh source states</Button></div>
    <p className="text-sm text-text-secondary">Source readiness and collector freshness are separate. Missing Syslog or Flow does not change device reachability. Manual inventory collection does not enable recurring SNMP polling.</p>
    {(telemetry.error || error) && <Alert variant="critical" title="Source states unavailable">{error ?? (telemetry.error instanceof Error ? telemetry.error.message : 'Unable to load sources.')}</Alert>}
    {telemetry.isLoading ? <p>Loading source states…</p> : telemetry.data && <div className="grid gap-3">{sourceTypes.map(type => {
      const source = telemetry.data.sources.find(row => row.source_type === type);
      return <div key={type} className="rounded border border-border-subtle p-3"><h4 className="font-semibold">{sourceNames[type]} · {stateName(source?.state ?? 'not_configured')}</h4>
        {source ? <><p>Collector: {source.collector_id} · Tenant: {source.tenant_id} · Assigned site: {source.site || 'Unassigned'}</p>
          <p>Collector state: {stateName(source.collector_status)} · Heartbeat: {when(source.collector_heartbeat_at)}</p>
          <p>Source contact: {when(source.last_contact_at)} · Observation: {when(source.observed_at)}</p>
          <p>Queue: {source.queue_depth} · Lag: {source.lag_millis} ms · Freshness window: {source.stale_after_seconds} seconds</p>
          {source.sender_address && <p>Transport sender: {source.sender_address}</p>}</> : <p className="text-sm text-text-secondary">No collection binding. Assign an existing tenant collector with this protocol configured.</p>}
      </div>;
    })}</div>}
    {canConfigure && <Button variant="secondary" onClick={() => setEditing(!editing)} disabled={busy}>Configure source binding</Button>}
    {editing && <form className="grid gap-3" onSubmit={e => { e.preventDefault(); void save(); }}>
      <p className="text-sm">Use a collector already registered for this tenant. This binds collection evidence; it does not deploy a receiver or poller. Rebinding clears earlier evidence.</p>
      <Label>Source<select className="block w-full rounded border bg-surface p-2" value={config.source_type} onChange={e => setConfig({ ...config, source_type: e.target.value as NetworkSourceConfig['source_type'] })}>{sourceTypes.map(type => <option key={type} value={type}>{sourceNames[type]}</option>)}</select></Label>
      <Label>Collector ID<Input required value={config.collector_id} onChange={e => setConfig({ ...config, collector_id: e.target.value })} /></Label>
      <Label>Assigned site<Input value={config.site} onChange={e => setConfig({ ...config, site: e.target.value })} /></Label>
      {receiverTypes.includes(config.source_type) && <Label>Transport sender IP<Input required value={config.sender_address} onChange={e => setConfig({ ...config, sender_address: e.target.value })} /></Label>}
      <Label>Freshness window (seconds)<Input type="number" min={60} max={86400} value={config.stale_after_seconds} onChange={e => setConfig({ ...config, stale_after_seconds: Number(e.target.value) })} /></Label>
      <Button type="submit" disabled={busy}>{busy ? 'Saving…' : 'Save source binding'}</Button>
    </form>}
  </section>;
}

export function NetworkObservabilityPanel({ tenantId }: { tenantId?: string }): JSX.Element {
  const api = useApiClient();
  const [offset, setOffset] = useState(0);
  const targets = useQuery({ queryKey: ['network-observability', tenantId ?? 'all', offset], queryFn: () => api.listNetworkTargets({ tenantId, limit: 10, offset }) });
  return <section aria-label="Network device observability" className="grid gap-3 rounded border border-border-subtle p-4"><h2 className="font-semibold">Network device source readiness</h2>
    {targets.error && <p role="alert">Network source inventory unavailable.</p>}
    {targets.data && <p>{targets.data.pagination.total} network devices · {tenantId ? 'Selected tenant' : 'All authorized tenants'}</p>}
    {targets.data?.data.map(target => <details key={target.id} className="rounded border border-border-subtle p-3"><summary className="cursor-pointer">{target.display_name} · Reachability: {stateName(target.reachability_state)} · Site: {target.site || 'Unassigned'}</summary>
      <Link className="underline" to={`/network-devices?device=${encodeURIComponent(target.id)}`}>Open device</Link>
      <NetworkTelemetryPanel targetId={target.id} tenantId={target.tenant_id} site={target.site} />
    </details>)}
    {targets.data?.pagination.total === 0 && <p>No network devices in this scope.</p>}
    <div className="flex gap-3"><Button variant="secondary" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 10))}>Previous network devices</Button><Button variant="secondary" disabled={!targets.data || offset + 10 >= targets.data.pagination.total} onClick={() => setOffset(offset + 10)}>Next network devices</Button></div>
  </section>;
}
