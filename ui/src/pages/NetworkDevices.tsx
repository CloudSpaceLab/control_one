import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
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
import { NetworkConfigurationPanel } from '../components/NetworkConfigurationPanel';
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
const attention = (target: NetworkTarget) => {
  if (target.reachability_state === 'unreachable') return 'Unreachable';
  if (target.collection_state === 'stale') return 'Collection stale';
  if (target.collection_state === 'authenticated' && !target.last_successful_collection_at) return 'Inventory not collected';
  if (target.reachability_state === 'unknown' || target.collection_state === 'discovered') return 'Not verified';
  return '—';
};

export function NetworkDevices(): JSX.Element {
  const api = useApiClient();
  const cache = useQueryClient();
  const { profile } = useAuth();
  const { currentTenantId, tenants } = useTenant();
  const { showToast } = useToast();
  const [params, setParams] = useSearchParams();
  const selectedId = params.get('device');
  const [adding, setAdding] = useState(params.get('add') === '1');
  const [search, setSearch] = useState(params.get('search') ?? '');
  const [type, setType] = useState(params.get('type') ?? '');
  const [site, setSite] = useState(params.get('site') ?? '');
  const [group, setGroup] = useState(params.get('group') ?? '');
  const [vendor, setVendor] = useState(params.get('vendor') ?? '');
  const [model, setModel] = useState(params.get('model') ?? '');
  const [platform, setPlatform] = useState(params.get('platform') ?? '');
  const [firmware, setFirmware] = useState(params.get('firmware') ?? '');
  const [reachability, setReachability] = useState(params.get('reachability_state') ?? '');
  const [collection, setCollection] = useState(params.get('collection_state') ?? '');
  const [offset, setOffset] = useState(Number(params.get('offset') ?? 0));
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [identityOnly, setIdentityOnly] = useState(false);
  const canWrite = profile?.permissions?.includes('targets.write') ?? profile?.roles.some((role) => ['admin', 'operator'].includes(role)) ?? false;
  const canConnect = profile?.permissions?.includes('targets.connect') ?? profile?.roles.some((role) => ['admin', 'operator'].includes(role)) ?? false;
  const tenantScope = currentTenantId ?? undefined;
  const previousTenant = useRef(currentTenantId);
  const resetFilters = useCallback(() => {
    setSearch(''); setType(''); setSite(''); setGroup(''); setVendor(''); setModel('');
    setPlatform(''); setFirmware(''); setReachability(''); setCollection(''); setOffset(0);
    setParams({}, { replace: true });
  }, [setParams]);
  const filterKey = params.toString();
  useEffect(() => {
    const current = new URLSearchParams(filterKey);
    setSearch(current.get('search') ?? ''); setType(current.get('type') ?? '');
    setSite(current.get('site') ?? ''); setGroup(current.get('group') ?? '');
    setVendor(current.get('vendor') ?? ''); setModel(current.get('model') ?? '');
    setPlatform(current.get('platform') ?? ''); setFirmware(current.get('firmware') ?? '');
    setReachability(current.get('reachability_state') ?? ''); setCollection(current.get('collection_state') ?? '');
    setOffset(Number(current.get('offset') ?? 0));
  }, [filterKey]);
  // A tenant switch clears an open detail from the previous scope.
  useEffect(() => {
    if (previousTenant.current !== currentTenantId) {
      previousTenant.current = currentTenantId;
      resetFilters();
      setAdding(false);
    }
  }, [currentTenantId, resetFilters]);
  const list = useQuery({
    queryKey: ['network-targets', tenantScope, search, type, site, group, vendor, model, platform, firmware, reachability, collection, offset],
    queryFn: () => api.listNetworkTargets({
      tenantId: tenantScope, search: search.trim(), type, site: site.trim(), group: group.trim(),
      vendor: vendor.trim(), model: model.trim(), platform: platform.trim(), firmware: firmware.trim(),
      reachabilityState: reachability, collectionState: collection, limit: 20, offset,
    }),
  });
  const detail = useQuery({
    queryKey: ['network-target', selectedId, tenantScope],
    queryFn: () => api.getNetworkTarget(selectedId!),
    enabled: !!selectedId,
  });
  const device = detail.data?.family === 'network_security' && (!currentTenantId || detail.data.tenant_id === currentTenantId) ? detail.data : undefined;
  const tenantName = (id: string) => tenants.find((tenant) => tenant.id === id)?.name ?? id;
  const updateFilter = (key: string, value: string, update: (value: string) => void) => {
    update(value);
    setOffset(0);
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (value.trim()) next.set(key, value.trim()); else next.delete(key);
      next.delete('offset');
      return next;
    }, { replace: true });
  };
  const changePage = (nextOffset: number) => {
    setOffset(nextOffset);
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (nextOffset > 0) next.set('offset', String(nextOffset)); else next.delete('offset');
      return next;
    }, { replace: true });
  };
  const openDevice = (target: NetworkTarget) => setParams((current) => { const next = new URLSearchParams(current); next.set('device', target.id); return next; });
  const columns: ColumnDef<NetworkTarget, unknown>[] = [
    { id: 'device', header: 'Device', enableSorting: false, cell: ({ row }) => <Button variant="ghost" onClick={() => openDevice(row.original)}>{row.original.display_name}</Button> },
    { id: 'type', header: 'Type', enableSorting: false, cell: ({ row }) => typeName(row.original.type) },
    { id: 'tenant', header: 'Tenant', enableSorting: false, cell: ({ row }) => tenantName(row.original.tenant_id) },
    { id: 'vendor-model', header: 'Vendor / model', enableSorting: false, cell: ({ row }) => [row.original.vendor, row.original.model].filter(Boolean).join(' / ') || 'Not detected' },
    { id: 'site', header: 'Site / group', enableSorting: false, cell: ({ row }) => [row.original.site, row.original.group].filter(Boolean).join(' / ') || '—' },
    { id: 'address', header: 'Management address', enableSorting: false, cell: ({ row }) => row.original.addresses.filter((address) => address.current && address.purpose === 'management').map((address) => address.address).join(', ') || '—' },
    { id: 'platform-firmware', header: 'Platform / firmware', enableSorting: false, cell: ({ row }) => [row.original.platform, row.original.firmware].filter(Boolean).join(' / ') || 'Not detected' },
    { id: 'reachability', header: 'Reachability', enableSorting: false, cell: ({ row }) => row.original.reachability_state === 'unknown' ? 'Not verified' : row.original.reachability_state },
    { id: 'collection', header: 'Collection', enableSorting: false, cell: ({ row }) => row.original.collection_state.replaceAll('_', ' ') },
    { id: 'observed', header: 'Last observed', enableSorting: false, cell: ({ row }) => when(row.original.last_observed_at) },
    { id: 'attention', header: 'Attention', enableSorting: false, cell: ({ row }) => attention(row.original) },
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
      resetFilters();
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
      <div><Label htmlFor="device-search">Search devices</Label><Input id="device-search" placeholder="Name, hostname, serial or address" value={search} onChange={(event) => updateFilter('search', event.target.value, setSearch)} /></div>
      <div><Label htmlFor="device-type">Device type</Label><select id="device-type" className={selectClass} value={type} onChange={(event) => updateFilter('type', event.target.value, setType)}><option value="">All types</option>{TYPES.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></div>
      <div><Label htmlFor="device-site">Site</Label><Input id="device-site" value={site} onChange={(event) => updateFilter('site', event.target.value, setSite)} /></div>
      <div><Label htmlFor="device-group">Group</Label><Input id="device-group" value={group} onChange={(event) => updateFilter('group', event.target.value, setGroup)} /></div>
      <div><Label htmlFor="device-vendor">Vendor</Label><Input id="device-vendor" value={vendor} onChange={(event) => updateFilter('vendor', event.target.value, setVendor)} /></div>
      <div><Label htmlFor="device-model">Model</Label><Input id="device-model" value={model} onChange={(event) => updateFilter('model', event.target.value, setModel)} /></div>
      <div><Label htmlFor="device-platform">Platform</Label><Input id="device-platform" value={platform} onChange={(event) => updateFilter('platform', event.target.value, setPlatform)} /></div>
      <div><Label htmlFor="device-firmware">Firmware</Label><Input id="device-firmware" value={firmware} onChange={(event) => updateFilter('firmware', event.target.value, setFirmware)} /></div>
      <div><Label htmlFor="device-reachability">Reachability</Label><select id="device-reachability" className={selectClass} value={reachability} onChange={(event) => updateFilter('reachability_state', event.target.value, setReachability)}><option value="">All states</option><option value="unknown">Not verified</option><option value="reachable">Reachable</option><option value="unreachable">Unreachable</option></select></div>
      <div><Label htmlFor="device-collection">Telemetry / collection state</Label><select id="device-collection" className={selectClass} value={collection} onChange={(event) => updateFilter('collection_state', event.target.value, setCollection)}><option value="">All states</option><option value="discovered">Discovered</option><option value="reachable">Reachable</option><option value="authenticated">Authenticated</option><option value="inventory_ready">Inventory ready</option><option value="telemetry_partial">Telemetry partial</option><option value="telemetry_ready">Telemetry ready</option><option value="stale">Stale</option><option value="auth_failed">Authentication failed</option><option value="unreachable">Unreachable</option><option value="unsupported">Unsupported</option><option value="policy_blocked">Policy blocked</option></select></div>
    </div></Panel>
    <p className="text-sm text-text-secondary">{currentTenantId ? tenantName(currentTenantId) : 'All tenants'} · {list.data?.pagination.total ?? 0} devices</p>
    {list.error && <Alert variant="critical" title="Inventory unavailable">{message(list.error)}</Alert>}
    <DataTable columns={columns} rows={list.data?.data ?? []} rowKey={(target) => target.id} loading={list.isFetching}
      empty={<EmptyState icon={<Network />} title="No network devices" description="Register a device or adjust your filters." />} />
    <div className="flex items-center justify-between gap-3"><Button variant="secondary" disabled={offset === 0 || list.isFetching} onClick={() => changePage(Math.max(0, offset - 20))}>Previous</Button><span className="text-sm">Page {Math.floor(offset / 20) + 1}</span><Button variant="secondary" disabled={list.isFetching || !list.data || offset + 20 >= list.data.pagination.total} onClick={() => changePage(offset + 20)}>Next</Button></div>

    <Dialog open={adding && canWrite} onOpenChange={(open) => { if (!saving) setAdding(open); }}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl"><DialogHeader><DialogTitle>Add network device</DialogTitle><DialogDescription>Connect through read-only appliance protocols or register an unverified identity.</DialogDescription></DialogHeader>
        {canConnect && <Button type="button" variant="secondary" disabled={saving} onClick={() => setIdentityOnly(!identityOnly)}>{identityOnly ? 'Test connection before saving' : 'Register identity only'}</Button>}
        {canConnect && !identityOnly ? <NetworkDeviceWizard onBusyChange={setSaving} onCancel={() => setAdding(false)} onSaved={(created) => {
          setAdding(false); resetFilters();
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

    <Dialog open={!!selectedId} onOpenChange={(open) => { if (!open) setParams((current) => { const next = new URLSearchParams(current); next.delete('device'); return next; }); }}>
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
          <NetworkConfigurationPanel key={`configuration-${device.id}`} targetId={device.id} />
          <NetworkTelemetryPanel key={`telemetry-${device.id}`} targetId={device.id} tenantId={device.tenant_id} site={device.site} canConfigure={canWrite} />
          <div className="flex flex-wrap gap-3 border-t border-border-subtle pt-4" aria-label="Related device workflows">
            <Link className="underline" to={`/search?q=${encodeURIComponent(device.display_name)}`}>Search events</Link>
            <Link className="underline" to="/cases">Open cases</Link>
            <Link className="underline" to="/observability">Observability</Link>
          </div>
          <Button variant="secondary" onClick={() => setParams((current) => { const next = new URLSearchParams(current); next.delete('device'); return next; })}>Back to inventory</Button>
        </>}
      </DialogContent>
    </Dialog>
    <p className="text-sm text-text-secondary">For agent-managed computers, use <Link className="underline" to="/onboard">machine enrollment</Link>.</p>
  </div>;
}
