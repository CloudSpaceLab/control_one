import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { Network, Plus, RefreshCw } from 'lucide-react';
import type { ColumnDef } from '@tanstack/react-table';
import { useApiClient } from '../hooks/useApiClient';
import { useAuth } from '../providers/AuthProvider';
import { useTenant } from '../providers/TenantProvider';
import { useToast } from '../providers/ToastProvider';
import { Alert, DataTable, EmptyState, Panel, SectionHeader } from '../components/kit';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../components/ui/dialog';
import type { NetworkTarget } from '../lib/api';
import { NetworkDeviceWizard } from '../components/NetworkDeviceWizard';
import { NetworkInventoryPanel } from '../components/NetworkInventoryPanel';
import { NetworkTelemetryPanel } from '../components/NetworkTelemetryPanel';

const TYPES = [
  ['router', 'Router'], ['switch', 'Switch'], ['firewall', 'Firewall'],
  ['load_balancer', 'Load balancer'], ['waf', 'Web application firewall'],
  ['vpn_gateway', 'VPN gateway'], ['wireless_controller', 'Wireless controller'],
  ['access_point', 'Access point'], ['ids_ips', 'IDS / IPS'], ['network_appliance', 'Network appliance'],
] as const;
const selectClass = 'h-10 w-full rounded-md border border-border-subtle bg-surface px-3 text-sm';
const message = (err: unknown) => err instanceof Error ? err.message : 'Unable to load network devices.';
const typeName = (value: string) => TYPES.find(([key]) => key === value)?.[1] ?? value;
const when = (value?: string) => value ? new Date(value).toLocaleString() : 'Never';

export function NetworkDevices(): JSX.Element {
  const api = useApiClient();
  const cache = useQueryClient();
  const { profile } = useAuth();
  const { currentTenantId, tenants } = useTenant();
  const { showToast } = useToast();
  const [params, setParams] = useSearchParams();
  const selectedId = params.get('device');
  const [adding, setAdding] = useState(params.get('add') === '1');
  const [search, setSearch] = useState('');
  const [type, setType] = useState('');
  const [site, setSite] = useState('');
  const [group, setGroup] = useState('');
  const [offset, setOffset] = useState(0);
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [identityOnly, setIdentityOnly] = useState(false);
  const canWrite = profile?.permissions?.includes('targets.write') ?? profile?.roles.some((role) => ['admin', 'operator'].includes(role)) ?? false;
  const canConnect = profile?.permissions?.includes('targets.connect') ?? profile?.roles.some((role) => ['admin', 'operator'].includes(role)) ?? false;
  const tenantScope = currentTenantId ?? undefined;
  const previousTenant = useRef(currentTenantId);
  // A tenant switch clears an open detail from the previous scope.
  useEffect(() => {
    if (previousTenant.current !== currentTenantId) {
      previousTenant.current = currentTenantId;
      setOffset(0); setParams({}, { replace: true });
      setAdding(false);
    }
  }, [currentTenantId, setParams]);
  const list = useQuery({
    queryKey: ['network-targets', tenantScope, search, type, site, group, offset],
    queryFn: () => api.listNetworkTargets({ tenantId: tenantScope, search: search.trim(), type, site: site.trim(), group: group.trim(), limit: 20, offset }),
  });
  const detail = useQuery({
    queryKey: ['network-target', selectedId, tenantScope],
    queryFn: () => api.getNetworkTarget(selectedId!),
    enabled: !!selectedId,
  });
  const device = detail.data?.family === 'network_security' && (!currentTenantId || detail.data.tenant_id === currentTenantId) ? detail.data : undefined;
  const tenantName = (id: string) => tenants.find((tenant) => tenant.id === id)?.name ?? id;
  const openDevice = (target: NetworkTarget) => setParams({ device: target.id });
  const columns: ColumnDef<NetworkTarget, unknown>[] = [
    { id: 'device', header: 'Device', enableSorting: false, cell: ({ row }) => <Button variant="ghost" onClick={() => openDevice(row.original)}>{row.original.display_name}</Button> },
    { id: 'type', header: 'Type', enableSorting: false, cell: ({ row }) => typeName(row.original.type) },
    { id: 'tenant', header: 'Tenant', enableSorting: false, cell: ({ row }) => tenantName(row.original.tenant_id) },
    { id: 'site', header: 'Site / group', enableSorting: false, cell: ({ row }) => [row.original.site, row.original.group].filter(Boolean).join(' / ') || '—' },
    { id: 'address', header: 'Management address', enableSorting: false, cell: ({ row }) => row.original.addresses.filter((address) => address.current && address.purpose === 'management').map((address) => address.address).join(', ') || '—' },
    { id: 'reachability', header: 'Reachability', enableSorting: false, cell: ({ row }) => row.original.reachability_state === 'unknown' ? 'Not verified' : row.original.reachability_state },
    { id: 'collection', header: 'Collection', enableSorting: false, cell: ({ row }) => row.original.collection_state.replaceAll('_', ' ') },
    { id: 'observed', header: 'Last observed', enableSorting: false, cell: ({ row }) => when(row.original.last_observed_at) },
  ];

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canWrite || saving) return;
    const values = new FormData(event.currentTarget);
    const value = (name: string) => String(values.get(name) ?? '').trim();
    const tenantId = value('tenant');
    if (!tenantId) { setFormError('Choose a tenant before saving.'); return; }
    setSaving(true);
    setFormError(null);
    try {
      const created = await api.createNetworkTarget({
        tenant_id: tenantId, type: value('type'), display_name: value('name'),
        hostname: value('hostname'), site: value('site'), group: value('group'),
        management_addresses: [value('address')],
      });
      setAdding(false);
      setSearch(''); setType(''); setSite(''); setGroup(''); setOffset(0);
      cache.setQueryData(['network-target', created.id, tenantScope], created);
      await cache.invalidateQueries({ queryKey: ['network-targets'] });
      openDevice(created);
      showToast('Device registered. Connection and collection are not verified.', 'success');
    } catch (err) { setFormError(message(err)); }
    finally { setSaving(false); }
  }

  return <div className="flex flex-col gap-5">
    <SectionHeader eyebrow="OPERATIONS" title="Network devices" description="Agentless network and security inventory."
      actions={<><Button variant="secondary" onClick={() => list.refetch()}><RefreshCw className="mr-2 h-4 w-4" />Refresh</Button>{canWrite && <Button onClick={() => { setFormError(null); setAdding(true); }}><Plus className="mr-2 h-4 w-4" />Add network device</Button>}</>} />
    <Alert variant="info" title="Read-only network onboarding">Test SNMPv3 or SSH credentials before saving a device. Connection authentication and recurring telemetry readiness are separate states.</Alert>
    <Panel><div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <div><Label htmlFor="device-search">Search devices</Label><Input id="device-search" placeholder="Name, hostname, serial or address" value={search} onChange={(event) => { setSearch(event.target.value); setOffset(0); }} /></div>
      <div><Label htmlFor="device-type">Device type</Label><select id="device-type" className={selectClass} value={type} onChange={(event) => { setType(event.target.value); setOffset(0); }}><option value="">All types</option>{TYPES.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></div>
      <div><Label htmlFor="device-site">Site</Label><Input id="device-site" value={site} onChange={(event) => { setSite(event.target.value); setOffset(0); }} /></div>
      <div><Label htmlFor="device-group">Group</Label><Input id="device-group" value={group} onChange={(event) => { setGroup(event.target.value); setOffset(0); }} /></div>
    </div></Panel>
    <p className="text-sm text-text-secondary">{currentTenantId ? tenantName(currentTenantId) : 'All tenants'} · {list.data?.pagination.total ?? 0} devices</p>
    {list.error && <Alert variant="critical" title="Inventory unavailable">{message(list.error)}</Alert>}
    <DataTable columns={columns} rows={list.data?.data ?? []} rowKey={(target) => target.id} loading={list.isFetching}
      empty={<EmptyState icon={<Network />} title="No network devices" description="Register a device or adjust your filters." />} />
    <div className="flex items-center justify-between gap-3"><Button variant="secondary" disabled={offset === 0 || list.isFetching} onClick={() => setOffset(Math.max(0, offset - 20))}>Previous</Button><span className="text-sm">Page {Math.floor(offset / 20) + 1}</span><Button variant="secondary" disabled={list.isFetching || !list.data || offset + 20 >= list.data.pagination.total} onClick={() => setOffset(offset + 20)}>Next</Button></div>

    <Dialog open={adding && canWrite} onOpenChange={(open) => { if (!saving) setAdding(open); }}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl"><DialogHeader><DialogTitle>Add network device</DialogTitle><DialogDescription>Connect through read-only appliance protocols or register an unverified identity.</DialogDescription></DialogHeader>
        {canConnect && <Button type="button" variant="secondary" disabled={saving} onClick={() => setIdentityOnly(!identityOnly)}>{identityOnly ? 'Test connection before saving' : 'Register identity only'}</Button>}
        {canConnect && !identityOnly ? <NetworkDeviceWizard onBusyChange={setSaving} onCancel={() => setAdding(false)} onSaved={(created) => {
          setAdding(false); setSearch(''); setType(''); setSite(''); setGroup(''); setOffset(0);
          cache.setQueryData(['network-target', created.id, tenantScope], created);
          void cache.invalidateQueries({ queryKey: ['network-targets'] });
          openDevice(created); showToast('Device saved with a verified read-only connection.', 'success');
        }} /> : <form onSubmit={save} className="flex flex-col gap-4">
          <Alert variant="warning" title="Identity only">Saving here does not verify a connection or enable telemetry.</Alert>
          <div><Label htmlFor="new-tenant">Tenant</Label><select id="new-tenant" name="tenant" required className={selectClass} defaultValue={currentTenantId ?? ''} disabled={saving}><option value="">Choose tenant</option>{tenants.filter((tenant) => !currentTenantId || tenant.id === currentTenantId).map((tenant) => <option key={tenant.id} value={tenant.id}>{tenant.name}</option>)}</select></div>
          <div className="grid gap-4 sm:grid-cols-2"><div><Label htmlFor="new-name">Device name</Label><Input id="new-name" name="name" required maxLength={255} disabled={saving} placeholder="Lagos branch switch" /></div><div><Label htmlFor="new-type">Device type</Label><select id="new-type" name="type" className={selectClass} defaultValue="switch" disabled={saving}>{TYPES.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></div></div>
          <div><Label htmlFor="new-address">Management address</Label><Input id="new-address" name="address" required maxLength={253} disabled={saving} placeholder="IP address or DNS name" /><p className="mt-1 text-xs text-text-secondary">Use an IP address or DNS name without a protocol or port.</p></div>
          <div><Label htmlFor="new-hostname">Hostname (optional)</Label><Input id="new-hostname" name="hostname" maxLength={255} disabled={saving} /></div>
          <div className="grid gap-4 sm:grid-cols-2"><div><Label htmlFor="new-site">Site (optional)</Label><Input id="new-site" name="site" maxLength={255} disabled={saving} /></div><div><Label htmlFor="new-group">Group (optional)</Label><Input id="new-group" name="group" maxLength={255} disabled={saving} /></div></div>
          {formError && <Alert variant="critical" title="Unable to register device">{formError}</Alert>}
          <div className="flex justify-end gap-2"><Button type="button" variant="secondary" disabled={saving} onClick={() => setAdding(false)}>Cancel</Button><Button type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save device'}</Button></div>
        </form>}
      </DialogContent>
    </Dialog>

    <Dialog open={!!selectedId} onOpenChange={(open) => { if (!open) setParams({}); }}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{device?.display_name ?? 'Device details'}</DialogTitle><DialogDescription>Network &amp; security · {device ? typeName(device.type) : 'Loading identity'}</DialogDescription></DialogHeader>
        {detail.isLoading && <p>Loading device…</p>}
        {detail.error && <Alert variant="critical">{message(detail.error)}</Alert>}
        {detail.data && !device && <Alert variant="warning">This network device is outside the selected tenant.</Alert>}
        {device && <>
          {!device.last_observed_at && <Alert variant="warning" title="Connection not verified">Identity registered. No successful device contact or collection has been recorded.</Alert>}
          <dl className="grid grid-cols-2 gap-4 text-sm">
            {[['Tenant', tenantName(device.tenant_id)], ['Device type', typeName(device.type)], ['Site', device.site || '—'], ['Group', device.group || '—'], ['Lifecycle', device.lifecycle_state], ['Reachability', device.reachability_state === 'unknown' ? 'Not verified' : device.reachability_state], ['Collection', device.collection_state.replaceAll('_', ' ')], ['Last observed', when(device.last_observed_at)], ['Last successful collection', when(device.last_successful_collection_at)], ['Vendor / model', [device.vendor, device.model].filter(Boolean).join(' / ') || 'Not detected'], ['Platform / firmware', [device.platform, device.firmware].filter(Boolean).join(' / ') || 'Not detected'], ['Agent requirement', 'Agentless']].map(([label, value]) => <div key={label}><dt className="text-text-secondary">{label}</dt><dd className="mt-1 break-words font-medium">{value}</dd></div>)}
          </dl>
          <div className="text-sm"><h3 className="font-semibold">Management addresses</h3>{device.addresses.filter((address) => address.purpose === 'management' && address.current).map((address) => <p key={address.address} className="mt-1 break-all">{address.address} · Source: {address.source} · Confidence: {address.confidence}%</p>)}</div>
          <div className="text-sm"><h3 className="font-semibold">Classification evidence</h3><p className="mt-1">Source: {device.classification.source} · Confidence: {device.classification.confidence}%</p><ul className="mt-2 list-disc space-y-1 pl-5">{device.classification.evidence.map((evidence) => <li key={evidence}>{evidence}</li>)}</ul></div>
          <p className="text-sm text-text-secondary">{device.capabilities.length ? `Verified capabilities: ${device.capabilities.join(', ')}` : 'No verified capabilities. Register with a connection test to verify read-only identity access.'}</p>
          {device.collection_state === 'authenticated' && <Alert variant="info" title="Connection verified">This device passed a one-time read-only identity test. Recurring inventory and telemetry collection are not enabled.</Alert>}
          <NetworkInventoryPanel key={device.id} targetId={device.id} canRefresh={canConnect} supportsSNMP={device.management_modes?.includes('snmpv3') ?? false} />
          <NetworkTelemetryPanel key={`telemetry-${device.id}`} targetId={device.id} tenantId={device.tenant_id} site={device.site} canConfigure={canWrite} />
          <Button variant="secondary" onClick={() => setParams({})}>Back to inventory</Button>
        </>}
      </DialogContent>
    </Dialog>
    <p className="text-sm text-text-secondary">For agent-managed computers, use <Link className="underline" to="/onboard">machine enrollment</Link>.</p>
  </div>;
}
