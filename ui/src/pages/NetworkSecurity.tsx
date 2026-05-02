import { lazy, Suspense, useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { SectionHeader, EmptyState, StatusTag, KpiTile } from '../components/kit';
import { Skeleton } from '../components/ui/skeleton';
import { Button } from '../components/ui/button';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { ShieldAlert, RefreshCw } from 'lucide-react';
import type { ActiveBlock, NodeFirewallRule } from '../lib/api';

// NetworkSecurity is the consolidated /security/network surface introduced in
// PR 3. It hosts the previously-separate Threat Feeds and Connections pages
// as tabs (lazy-loaded — they keep their own data hooks) and adds two new
// tabs: Active Blocks (rolled-up view of operator-driven IP blocks across
// the fleet) and Firewall Management (per-node firewall_state inspection).
//
// Tab state is mirrored to ?tab=… so deep links survive reloads, and the
// legacy /threat-feeds and /connections routes redirect here preserving
// their tab choice.
const ThreatFeeds = lazy(() => import('./ThreatFeeds').then((m) => ({ default: m.ThreatFeeds })));
const Connections = lazy(() => import('./Connections').then((m) => ({ default: m.Connections })));

const VALID_TABS = ['threats', 'connections', 'blocks', 'firewall'] as const;
type TabKey = (typeof VALID_TABS)[number];

function isValidTab(s: string | null): s is TabKey {
  return !!s && (VALID_TABS as readonly string[]).includes(s);
}

export function NetworkSecurity(): JSX.Element {
  const [params, setParams] = useSearchParams();
  const initial = isValidTab(params.get('tab')) ? (params.get('tab') as TabKey) : 'threats';
  const [tab, setTab] = useState<TabKey>(initial);

  const onTabChange = (next: string) => {
    if (!isValidTab(next)) return;
    setTab(next);
    const updated = new URLSearchParams(params);
    updated.set('tab', next);
    setParams(updated, { replace: true });
  };

  return (
    <div className="space-y-6 p-6">
      <SectionHeader
        title="Network security"
        description="Threat intel, live connections, active blocks, and per-node firewall state — one console."
      />
      <Tabs value={tab} onValueChange={onTabChange} className="w-full">
        <TabsList>
          <TabsTrigger value="threats">Threat feeds</TabsTrigger>
          <TabsTrigger value="connections">Connections</TabsTrigger>
          <TabsTrigger value="blocks">Active blocks</TabsTrigger>
          <TabsTrigger value="firewall">Firewall</TabsTrigger>
        </TabsList>

        <TabsContent value="threats" className="pt-4">
          <Suspense fallback={<Skeleton className="h-96 w-full" />}>
            {tab === 'threats' && <ThreatFeeds />}
          </Suspense>
        </TabsContent>

        <TabsContent value="connections" className="pt-4">
          <Suspense fallback={<Skeleton className="h-96 w-full" />}>
            {tab === 'connections' && <Connections />}
          </Suspense>
        </TabsContent>

        <TabsContent value="blocks" className="pt-4">
          <ActiveBlocksPanel />
        </TabsContent>

        <TabsContent value="firewall" className="pt-4">
          <FirewallManagementPanel />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ── Active Blocks ────────────────────────────────────────────────────────

function ActiveBlocksPanel(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [blocks, setBlocks] = useState<ActiveBlock[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<ActiveBlock | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await client.listActiveBlocks({ tenantId: currentTenantId, limit: 100 });
      setBlocks(resp.blocks ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const totals = blocks.reduce(
    (acc, b) => {
      acc.applied += b.NodesApplied;
      acc.failed += b.NodesFailed;
      acc.pending += b.NodesPending;
      return acc;
    },
    { applied: 0, failed: 0, pending: 0 },
  );

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant from the header to view active blocks." />;
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
        <KpiTile label="Active blocks" value={String(blocks.length)} />
        <KpiTile label="Nodes applied" value={String(totals.applied)} tone="healthy" />
        <KpiTile label="Nodes pending" value={String(totals.pending)} tone="warning" />
        <KpiTile label="Nodes failed" value={String(totals.failed)} tone={totals.failed > 0 ? 'critical' : 'unknown'} />
      </div>

      <div className="flex items-center justify-end gap-2">
        <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}

      {!loading && blocks.length === 0 ? (
        <EmptyState
          title="No active blocks"
          description="Operator-driven IP blocks dispatched from Investigate or Connections appear here once the agents pick them up."
        />
      ) : (
        <div className="rounded border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">IP</th>
                <th className="px-3 py-2">Action</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Applied / Total</th>
                <th className="px-3 py-2">Reason</th>
                <th className="px-3 py-2">Created</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {blocks.map((b) => {
                const tone = b.NodesFailed > 0 ? 'critical' : b.NodesPending > 0 ? 'warning' : 'healthy';
                const status = b.NodesFailed > 0 ? 'partial' : b.NodesPending > 0 ? 'pending' : 'applied';
                return (
                  <tr key={b.EntityActionID} className="border-t border-border hover:bg-hover">
                    <td className="px-3 py-2 font-mono text-xs">{b.EntityID}</td>
                    <td className="px-3 py-2">{b.Action}</td>
                    <td className="px-3 py-2">
                      <StatusTag tone={tone}>{status}</StatusTag>
                    </td>
                    <td className="px-3 py-2">
                      {b.NodesApplied}/{b.TotalNodes}
                    </td>
                    <td className="px-3 py-2 text-text-secondary">{b.Reason ?? '—'}</td>
                    <td className="px-3 py-2 text-text-secondary">{new Date(b.CreatedAt).toLocaleString()}</td>
                    <td className="px-3 py-2 text-right">
                      <Button variant="ghost" size="sm" onClick={() => setSelected(b)}>
                        Per-node
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {selected && <BlockNodeDetail block={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}

function BlockNodeDetail({ block, onClose }: { block: ActiveBlock; onClose: () => void }): JSX.Element {
  const client = useApiClient();
  const [rules, setRules] = useState<NodeFirewallRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancel = false;
    (async () => {
      setLoading(true);
      try {
        const resp = await client.listBlockNodes(block.EntityActionID);
        if (!cancel) setRules(resp.rules ?? []);
      } catch (err) {
        if (!cancel) setError(err instanceof Error ? err.message : 'load failed');
      } finally {
        if (!cancel) setLoading(false);
      }
    })();
    return () => {
      cancel = true;
    };
  }, [block.EntityActionID, client]);

  return (
    <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-xl overflow-y-auto border-l border-border bg-elevated p-6 shadow-2xl">
      <div className="mb-4 flex items-start justify-between">
        <div>
          <p className="text-xs uppercase tracking-wider text-text-secondary">Per-node fan-out</p>
          <h3 className="font-mono text-lg">{block.EntityID}</h3>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      {loading && <Skeleton className="h-32 w-full" />}
      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}
      {!loading && rules.length === 0 && <EmptyState title="No nodes" description="No nodes received this block." />}
      {rules.length > 0 && (
        <table className="w-full text-sm">
          <thead className="text-left text-xs uppercase tracking-wider text-text-secondary">
            <tr>
              <th className="px-2 py-1">Node</th>
              <th className="px-2 py-1">Status</th>
              <th className="px-2 py-1">Error</th>
              <th className="px-2 py-1">Applied</th>
            </tr>
          </thead>
          <tbody>
            {rules.map((r) => (
              <tr key={r.ID} className="border-t border-border">
                <td className="px-2 py-1 font-mono text-xs">{r.NodeID.slice(0, 8)}</td>
                <td className="px-2 py-1">
                  <StatusTag tone={statusTone(r.Status)}>{r.Status}</StatusTag>
                </td>
                <td className="px-2 py-1 text-xs text-text-secondary">{r.Error ?? '—'}</td>
                <td className="px-2 py-1 text-xs text-text-secondary">{r.AppliedAt ? new Date(r.AppliedAt).toLocaleString() : '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </aside>
  );
}

function statusTone(s: NodeFirewallRule['Status']): 'healthy' | 'warning' | 'critical' | 'unknown' {
  switch (s) {
    case 'applied':
      return 'healthy';
    case 'pending':
      return 'warning';
    case 'failed':
      return 'critical';
    case 'removed':
    default:
      return 'unknown';
  }
}

// ── Firewall Management ──────────────────────────────────────────────────

// Lightweight read-only view of per-node firewall_state. A full management
// surface (toggles, rule editors) is deferred to a follow-up PR — this tab
// gives operators a starting point for "what firewall is each node running."
function FirewallManagementPanel(): JSX.Element {
  return (
    <EmptyState
      title="Firewall management"
      description="Per-node firewall state inspector arrives in a follow-up PR. Today you can see firewall_type and rule counts on each node's detail page."
      icon={<ShieldAlert className="h-8 w-8" />}
    />
  );
}

export default NetworkSecurity;
