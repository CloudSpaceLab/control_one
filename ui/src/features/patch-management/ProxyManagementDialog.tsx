import {useCallback, useEffect, useRef, useState} from 'react';
import {useApiClient} from '../../hooks/useApiClient';
import type {NodeSummary, ProxyManagementView, SquidProxy} from '../../lib/api';
import {Button} from '../../components/ui/button';
import {Alert} from '../../components/kit';
import {ConfirmModal} from '../../components/ConfirmModal';

const lines = (value: string): string[] => value.split(/[\s,]+/).map(v => v.trim()).filter(Boolean);
type Action = 'install' | 'reconfigure' | 'remove';
type Request = {expected_revision: number; whitelist: string[]; client_cidrs: string[]; probe_urls: string[]; squid_version?: string};

export function ProxyManagementDialog({proxy, tenantId, onClose, onChanged}: {proxy: SquidProxy; tenantId: string; onClose: () => void; onChanged: () => void}): JSX.Element {
  const client = useApiClient();
  const [view, setView] = useState<ProxyManagementView | null>(null);
  const [nodes, setNodes] = useState<NodeSummary[]>([]);
  const [nodeId, setNodeId] = useState('');
  const [clients, setClients] = useState('127.0.0.1/32');
  const [hosts, setHosts] = useState(proxy.Whitelist.join('\n'));
  const [probes, setProbes] = useState(proxy.Whitelist.map(h => `https://${h}/`).join('\n'));
  const [version, setVersion] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [review, setReview] = useState<{action: Action; payload: Request} | null>(null);
  const generation = useRef(0);
  const submitting = useRef(false);
  const revision = useRef<number | null>(null);

  const load = useCallback(async (hydrate: boolean) => {
    const current = ++generation.current;
    if (proxy.TenantID !== tenantId) {setView(null); setError('Selected proxy does not belong to the current tenant'); return;}
    try {
      const [next, list] = await Promise.all([client.getProxyManagement(proxy.ID), client.listNodes({tenantId, hostnamePrefix: proxy.Host, limit: 200})]);
      if (generation.current !== current) return;
      setView(next);
      setNodes(list.data.filter(n => n.tenant_id === tenantId && n.hostname.toLowerCase().replace(/\.$/, '') === proxy.Host.toLowerCase().replace(/\.$/, '')));
      if (hydrate && next.management) {
        revision.current = next.management.revision;
        setNodeId(next.management.node_id);
        setClients(next.management.client_cidrs.join('\n'));
        setProbes(next.management.probe_urls.join('\n'));
        const payload = next.job?.payload as {whitelist?: unknown} | undefined;
        if (Array.isArray(payload?.whitelist) && payload.whitelist.every(h => typeof h === 'string')) setHosts(payload.whitelist.join('\n'));
      }
      setError(null);
    } catch (err) { if (generation.current === current) setError(err instanceof Error ? err.message : 'Proxy management unavailable'); }
  }, [client, proxy.ID, proxy.Host, proxy.TenantID, tenantId]);

  useEffect(() => {void load(true); return () => {generation.current++;};}, [load]);
  const active = view?.job?.status === 'running' || view?.job?.status === 'queued';
  useEffect(() => {if (!active) return; const timer = window.setInterval(() => void load(false), 3000); return () => window.clearInterval(timer);}, [active, load]);

  const bind = async () => {
    if (submitting.current) return;
    submitting.current = true; setBusy(true); setError(null);
    try {
      await client.bindProxyManagement(proxy.ID, {node_id: nodeId, client_cidrs: lines(clients), probe_urls: lines(probes)});
      await load(true); onChanged();
    } catch (err) {setError(err instanceof Error ? err.message : 'Binding failed');}
    finally {submitting.current = false; setBusy(false);}
  };
  const prepare = (action: Action) => {
    if (revision.current === null) return;
    setReview({action, payload: {expected_revision: revision.current, whitelist: lines(hosts), client_cidrs: lines(clients), probe_urls: lines(probes), ...(action === 'install' ? {squid_version: version.trim()} : {})}});
  };
  const submit = async () => {
    if (!review || submitting.current) return;
    submitting.current = true; setBusy(true); setError(null);
    try {
      await client.requestProxyLifecycle(proxy.ID, review.action, review.payload);
      setReview(null); await load(true); onChanged();
    } catch (err) {setError(err instanceof Error ? err.message : 'Change request failed');}
    finally {submitting.current = false; setBusy(false);}
  };
  const fieldClass = 'mt-1 w-full rounded border border-border bg-surface-1 p-2 text-sm';
  return <aside role="dialog" aria-modal="true" aria-label={`Manage proxy ${proxy.Host}`} className="fixed inset-y-0 right-0 z-50 w-full max-w-xl space-y-4 overflow-y-auto border-l border-border bg-surface p-6 shadow-xl">
    <div className="flex items-start justify-between gap-3"><h2 className="text-xl font-semibold">Manage proxy {proxy.Host}:3128</h2><Button variant="ghost" onClick={onClose} disabled={busy}>Close management</Button></div>
    <p className="text-sm text-text-secondary">Binding authorizes one enrolled host. Each change has its own reviewed policy revision and durable result. Registration and pending jobs do not prove service health.</p>
    {error && <Alert variant="critical" title="Proxy management unavailable">{error}</Alert>}
    {!view && !error && <p>Loading management evidence…</p>}
    {view && <>
      <Alert variant={view.eligible ? 'info' : 'warning'} title={view.eligible ? 'Host supports managed actions' : 'Host qualification required'}>{view.reason}</Alert>
      <fieldset disabled={busy || Boolean(review) || Boolean(active)} className="space-y-3">
        <label className="block text-sm">Enrolled management host<select aria-label="Enrolled management host" className={fieldClass} value={nodeId} disabled={Boolean(view.management)} onChange={e => setNodeId(e.target.value)}><option value="">Select matching host</option>{nodes.map(n => <option key={n.id} value={n.id}>{n.hostname} · {n.id.slice(0, 8)}</option>)}</select></label>
        {!view.management && <p className="text-xs text-text-secondary">Only hosts matching the registered hostname are listed. A host can manage one proxy on port 3128. Register the enrolled hostname if the list is empty.</p>}
        <label className="block text-sm">Repository hosts<textarea aria-label="Repository hosts" className={fieldClass} value={hosts} disabled={!view.management} onChange={e => setHosts(e.target.value)} /></label>
        <label className="block text-sm">Approved client CIDRs<textarea aria-label="Approved client CIDRs" className={fieldClass} value={clients} onChange={e => setClients(e.target.value)} /></label>
        <p className="text-xs text-text-secondary">Use explicit source networks, for example 10.20.30.0/24. Default routes are rejected. Localhost remains permitted for readiness checks.</p>
        <label className="block text-sm">HTTPS repository probes<textarea aria-label="HTTPS repository probes" className={fieldClass} value={probes} onChange={e => setProbes(e.target.value)} /></label>
        <p className="text-xs text-text-secondary">Probes must use approved repository hosts and return HTTP 2xx at the exact reviewed URL. Certificate validation stays enabled; redirects are rejected.</p>
        {!view.management ? <Button onClick={() => void bind()} disabled={!nodeId || Boolean(error)}>Bind management host</Button> : <>
          <p>Reviewed policy revision: {view.management.revision}</p>
          <label className="block text-sm">Exact Squid package version (new installation)<input aria-label="Exact Squid package version" className={fieldClass} value={version} onChange={e => setVersion(e.target.value)} /></label>
          <p className="text-xs text-text-secondary">Installation uses the host’s configured APT repositories and may install required dependencies. Existing Squid configurations must be reviewed through reconfiguration.</p>
          <div className="flex flex-wrap gap-2"><Button disabled={!view.eligible || Boolean(error) || !version.trim() || proxy.Status === 'removed'} onClick={() => prepare('install')}>Review installation</Button><Button disabled={!view.eligible || Boolean(error) || proxy.Status === 'removed'} onClick={() => prepare('reconfigure')}>Review reconfiguration</Button><Button variant="outline" disabled={!view.eligible || Boolean(error) || proxy.Status === 'removed'} onClick={() => prepare('remove')}>Review removal</Button></div>
        </>}
      </fieldset>
      {view.job && <section className="space-y-2 rounded border border-border p-3 text-sm" aria-label="Latest proxy job"><p>Latest job: <span className="font-mono break-all">{view.job.id}</span></p><p>Result: {view.job.status}</p>{view.message && <p>{view.message}</p>}<p>{active ? 'Waiting for authenticated target completion. Do not infer that the change has been applied.' : 'Review the recorded result and service verification before rollout.'}</p></section>}
      <Button variant="outline" onClick={() => void load(true)} disabled={busy || Boolean(review)}>Reload management evidence</Button>
    </>}
    <ConfirmModal open={Boolean(review)} title={`Confirm proxy ${review?.action ?? 'change'}`} body={review?.action === 'remove' ? 'Stop, disable and mask the managed proxy service and restore its original configuration. Software packages and audit history are retained. Reassign proxy-mode clients first.' : 'Apply this exact policy on the bound host. Repository and client access will change, and the Squid service will restart. A queued request is not a successful installation.'} confirmLabel="Submit reviewed change" cancelLabel="Cancel review" confirmDisabled={busy} cancelDisabled={busy} onConfirm={() => void submit()} onCancel={() => setReview(null)}>
      {review && <div className="space-y-2 text-xs"><p>Host: {view?.management?.node_id} · base revision {review.payload.expected_revision}</p><p>Repositories: {review.payload.whitelist.join(', ')}</p><p>Clients: {review.payload.client_cidrs.join(', ')}</p><p>Probes: {review.payload.probe_urls.join(', ')}</p>{review.payload.squid_version && <p>Squid: {review.payload.squid_version}</p>}</div>}
      {error && <Alert variant="critical" title="Request rejected">{error}</Alert>}
    </ConfirmModal>
  </aside>;
}
