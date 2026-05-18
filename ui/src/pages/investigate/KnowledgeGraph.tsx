import { useEffect, useMemo, useState } from 'react';
import { Download, RefreshCw } from 'lucide-react';
import {
  Alert,
  DataTable,
  EmptyState,
  KpiTile,
  Loader,
  SectionHeader,
  StatusTag,
} from '@/components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useApiClient } from '@/hooks/useApiClient';
import { useNodes } from '@/hooks/useNodes';
import { useTenant } from '@/providers/TenantProvider';
import { formatTs } from '@/lib/format';
import type { NodeService } from '@/lib/api';
import type { ColumnDef } from '@tanstack/react-table';

export function KnowledgeGraph(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [services, setServices] = useState<NodeService[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState('');

  const { data: nodes } = useNodes({ tenantId: currentTenantId ?? undefined, limit: 500, offset: 0 });
  const nodesById = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);

  const refresh = async () => {
    if (!currentTenantId || nodes.length === 0) return;
    setLoading(true);
    setError(null);
    try {
      const all = await Promise.all(
        nodes.map((n) =>
          client.listNodeServices(n.id).then((r) => r.data ?? []).catch(() => [] as NodeService[]),
        ),
      );
      setServices(all.flat());
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Service inventory failed to load');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentTenantId, nodes.length]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return services;
    return services.filter(
      (s) =>
        s.process.toLowerCase().includes(q) ||
        s.service_kind.toLowerCase().includes(q) ||
        String(s.port).includes(q) ||
        (s.probe_title ?? '').toLowerCase().includes(q),
    );
  }, [filter, services]);

  const totals = useMemo(() => {
    const distinctNodes = new Set(services.map((s) => s.node_id));
    const distinctKinds = new Set(services.map((s) => s.service_kind));
    return {
      services: services.length,
      nodes: distinctNodes.size,
      kinds: distinctKinds.size,
    };
  }, [services]);

  const columns = useMemo<ColumnDef<NodeService>[]>(
    () => [
      {
        header: 'Node',
        accessorKey: 'node_id',
        cell: ({ row }) => {
          const node = nodesById.get(row.original.node_id);
          return (
            <span className="font-mono text-xs text-text-secondary">
              {node?.hostname ?? row.original.node_id.slice(0, 8)}
            </span>
          );
        },
      },
      {
        header: 'Process',
        accessorKey: 'process',
        cell: ({ row }) => row.original.process || '—',
      },
      {
        header: 'Port',
        accessorKey: 'port',
        cell: ({ row }) => (
          <span className="font-mono tabular-nums text-text-secondary">{row.original.port}</span>
        ),
      },
      {
        header: 'Kind',
        accessorKey: 'service_kind',
        cell: ({ row }) => <StatusTag tone="info">{row.original.service_kind}</StatusTag>,
      },
      {
        header: 'Server',
        accessorKey: 'probe_server',
        cell: ({ row }) => row.original.probe_server ?? '—',
      },
      {
        header: 'Title',
        accessorKey: 'probe_title',
        cell: ({ row }) => (
          <span className="truncate text-text-secondary">{row.original.probe_title ?? '—'}</span>
        ),
      },
      {
        header: 'Observed',
        accessorKey: 'observed_at',
        cell: ({ row }) => (
          <span className="font-mono text-[0.7rem] text-text-muted">
            {formatTs(row.original.observed_at)}
          </span>
        ),
      },
    ],
    [nodesById],
  );

  const downloadMd = async () => {
    if (!currentTenantId) return;
    try {
      const md = await client.getKnowledgeGraphMarkdown(currentTenantId);
      const blob = new Blob([md], { type: 'text/markdown' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `knowledge_graph_${currentTenantId}.md`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Knowledge graph download failed');
    }
  };

  return (
    <div className="flex flex-col gap-5 px-4 py-6 sm:px-6 lg:px-8">
      <SectionHeader
        eyebrow="INVESTIGATE · KNOWLEDGE GRAPH"
        title="Services & URLs across the fleet"
        description="What's listening on every enrolled node — process, port, service kind, and (when probed) URL + page title. The same data flows into a per-tenant markdown document an LLM can ground answers against."
        actions={
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={refresh} disabled={loading}>
              <RefreshCw className="h-3.5 w-3.5" /> Refresh
            </Button>
            <Button variant="secondary" size="sm" onClick={downloadMd} disabled={!currentTenantId}>
              <Download className="h-4 w-4" /> Download .md
            </Button>
          </div>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="Total services" value={String(totals.services)} tone="brand" />
        <KpiTile label="Nodes reporting" value={`${totals.nodes} / ${nodes.length}`} tone="info" />
        <KpiTile label="Service kinds" value={String(totals.kinds)} tone="accent" />
      </div>

      <Input
        type="search"
        placeholder="Filter by process, kind, port, or title…"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        className="max-w-md"
      />

      {error && <Alert variant="critical">{error}</Alert>}

      {loading && services.length === 0 ? (
        <Loader size="md" label="Polling agents…" />
      ) : services.length === 0 ? (
        <EmptyState
          title="No services reported yet"
          description={nodes.length === 0 ? 'No enrolled nodes are available for this tenant.' : 'No node agent has reported listening services for this tenant. Check agent version, service discovery permissions, and heartbeat freshness.'}
        />
      ) : (
        <DataTable
          columns={columns}
          rows={filtered}
          rowKey={(s) => s.id}
          loading={loading}
        />
      )}
    </div>
  );
}
