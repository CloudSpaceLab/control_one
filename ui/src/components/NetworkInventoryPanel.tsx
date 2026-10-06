import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useApiClient } from '../hooks/useApiClient';
import { Alert } from './kit';
import { Button } from './ui/button';
import type { NetworkInventoryFact, NetworkInventoryRecord } from '../lib/api';

const display = (value: unknown) => typeof value === 'object' ? JSON.stringify(value) : String(value);
const time = (value?: string) => value ? new Date(value).toLocaleString() : 'Never';
function Facts({ facts }: { facts: Record<string, NetworkInventoryFact> }) {
  return <dl className="grid gap-3 sm:grid-cols-2">{Object.entries(facts).map(([key, fact]) => <div key={key}><dt className="text-text-secondary">{key.replaceAll('_', ' ')}</dt><dd className="break-words">{display(fact.value)}</dd><dd className="break-all text-xs text-text-secondary">{fact.protocol} · {fact.source} · {time(fact.observed_at)}</dd></div>)}</dl>;
}
function Records({ title, records }: { title: string; records: NetworkInventoryRecord[] }) {
  return <details className="rounded border border-border-subtle p-3"><summary className="cursor-pointer font-semibold">{title} ({records.length})</summary>{records.length ? records.map((record) => <div className="mt-3 border-t border-border-subtle pt-3" key={record.id}><p className="mb-2 font-medium">ID: {record.id}</p><Facts facts={record.facts} /></div>) : <p className="mt-2 text-text-secondary">No facts collected. This does not establish absence on the device.</p>}</details>;
}

export function NetworkInventoryPanel({ targetId, canRefresh, supportsSNMP }: { targetId: string; canRefresh: boolean; supportsSNMP: boolean }): JSX.Element {
  const api = useApiClient();
  const cache = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inventory = useQuery({ queryKey: ['network-inventory', targetId], queryFn: () => api.getNetworkInventory(targetId), refetchInterval: (query) => query.state.data?.state === 'refreshing' ? 2000 : false });
  const snapshot = inventory.data?.snapshot;
  async function refresh() {
    setBusy(true); setError(null);
    try {
      const result = await api.refreshNetworkInventory(targetId);
      cache.setQueryData(['network-inventory', targetId], result);
      await Promise.all([cache.invalidateQueries({ queryKey: ['network-target', targetId] }), cache.invalidateQueries({ queryKey: ['network-targets'] })]);
    } catch (err) { setError(err instanceof Error ? err.message : 'Unable to refresh inventory.'); }
    finally { setBusy(false); }
  }
  return <section className="flex flex-col gap-3 text-sm" aria-label="Collected network inventory">
    <div className="flex items-center justify-between gap-3"><h3 className="font-semibold">Collected inventory</h3>{canRefresh && <Button disabled={busy || !supportsSNMP} onClick={refresh}>{busy ? 'Collecting…' : 'Refresh inventory'}</Button>}</div>
    <p>Latest attempt: {inventory.data?.state.replaceAll('_', ' ') ?? 'Loading'} · Last attempt completed: {time(inventory.data?.completed_at)}</p>
    {!supportsSNMP && <p className="text-text-secondary">Inventory refresh currently requires a verified SNMPv3 connection. SSH inventory commands are not implemented.</p>}
    {error && <Alert variant="critical" title="Inventory refresh failed">{error}</Alert>}
    {inventory.error && <Alert variant="critical">Unable to load inventory.</Alert>}
    {snapshot && inventory.data?.state !== 'inventory_ready' && <Alert variant="warning" title="Previous snapshot">{inventory.data?.state === 'refreshing' ? 'Inventory refresh is in progress. ' : 'The latest refresh did not produce a new inventory. '}The facts below retain their previous observation times.</Alert>}
    {snapshot ? <>
      <p>Snapshot observed: {time(snapshot.observed_at)} · Adapter: {snapshot.adapter}. Inventory refresh is manual. Recurring collection readiness is shown under Network telemetry sources.</p>
      <Facts facts={snapshot.facts} />
      <Records title="Interfaces" records={snapshot.interfaces} />
      <p className="text-xs text-text-secondary">Topology is limited to protocol-reported LLDP/CDP neighbors below. No location-based or inferred links are added; each fact retains its protocol, source, and observation time.</p>
      <Records title="LLDP / CDP neighbors" records={snapshot.neighbors} />
      <Records title="Chassis / modules" records={snapshot.entities} />
      <Records title="IP addresses" records={snapshot.addresses} />
      <Records title="VLANs" records={snapshot.vlans} />
      <Records title="ARP" records={snapshot.arp} />
      <Records title="Route summary" records={snapshot.routes} />
      <Records title="CPU / storage / physical sensors" records={snapshot.resources} />
      <details><summary className="cursor-pointer">Unavailable / incomplete fields</summary><ul className="mt-2 list-disc pl-5">{snapshot.unavailable.map((item) => <li key={item}>{item}</li>)}</ul></details>
      <Records title="Sanitized protocol evidence" records={snapshot.raw_evidence} />
    </> : !inventory.isLoading && <p>No successful inventory collection recorded. Connection authentication alone does not establish inventory readiness.</p>}
  </section>;
}
