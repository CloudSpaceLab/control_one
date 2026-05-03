import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { GitCompare } from 'lucide-react';
import {
  Alert,
  EmptyState,
  IpActionMenu,
  KpiTile,
  Loader,
  Panel,
  StatusTag,
} from '@/components/kit';
import { Button } from '@/components/ui/button';
import { ConnectionDetailSheet } from './ConnectionDetailSheet';
import { useConnectionsByIp } from '@/hooks/useConnectionsByIp';
import { useNodes } from '@/hooks/useNodes';
import { useTenant } from '@/providers/TenantProvider';
import { formatBytes, formatDuration, formatTs } from '@/lib/format';
import { cn } from '@/lib/utils';
import type { ConnectionRow } from '@/lib/api';

const TIME_WINDOWS: { label: string; ms: number }[] = [
  { label: 'Last 1h', ms: 60 * 60 * 1000 },
  { label: 'Last 24h', ms: 24 * 60 * 60 * 1000 },
  { label: 'Last 7d', ms: 7 * 24 * 60 * 60 * 1000 },
];

export interface IpLifecyclePanelProps {
  ip: string;
}

export function IpLifecyclePanel({ ip }: IpLifecyclePanelProps): JSX.Element {
  const navigate = useNavigate();
  const { currentTenantId } = useTenant();
  const [windowMs, setWindowMs] = useState(TIME_WINDOWS[2].ms);
  const since = useMemo(() => new Date(Date.now() - windowMs).toISOString(), [windowMs]);
  const [openConn, setOpenConn] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const query = useConnectionsByIp({ tenantId: currentTenantId ?? undefined, ip, since });
  const rows = useMemo(() => query.data ?? [], [query.data]);

  const { data: allNodes } = useNodes({ tenantId: currentTenantId ?? undefined, limit: 500, offset: 0 });
  const nodesById = useMemo(() => new Map(allNodes.map((n) => [n.id, n])), [allNodes]);

  const totals = useMemo(() => {
    const distinctNodes = new Set<string>();
    const distinctProcesses = new Set<string>();
    const distinctUsers = new Set<string>();
    let bytesIn = 0;
    let bytesOut = 0;
    let threats = 0;
    for (const r of rows) {
      if (r.node_id) distinctNodes.add(r.node_id);
      if (r.process_name) distinctProcesses.add(r.process_name);
      if (r.user_name) distinctUsers.add(r.user_name);
      bytesIn += r.bytes_in ?? 0;
      bytesOut += r.bytes_out ?? 0;
      if (r.threat_match) threats += 1;
    }
    return {
      total: rows.length,
      bytesIn,
      bytesOut,
      threats,
      nodes: distinctNodes.size,
      processes: distinctProcesses.size,
      users: distinctUsers.size,
    };
  }, [rows]);

  const toggleSelect = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else if (next.size < 2) next.add(id);
      else {
        // Replace oldest selection to keep at most 2.
        const first = next.values().next().value;
        if (first) next.delete(first);
        next.add(id);
      }
      return next;
    });
  };

  const compareDisabled = selected.size !== 2;
  const onCompare = () => {
    if (selected.size !== 2) return;
    const [a, b] = Array.from(selected);
    navigate(`/investigate/ip/${encodeURIComponent(ip)}/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`);
  };

  // Group rows by node for the timeline strip.
  const groupedByNode = useMemo(() => {
    const m = new Map<string, ConnectionRow[]>();
    for (const r of rows) {
      const k = r.node_id ?? 'unknown';
      const arr = m.get(k) ?? [];
      arr.push(r);
      m.set(k, arr);
    }
    return m;
  }, [rows]);

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <KpiTile label="Lifecycles" value={String(totals.total)} tone="brand" />
        <KpiTile label="Distinct nodes" value={String(totals.nodes)} tone="info" />
        <KpiTile label="Bytes in / out" value={`${formatBytes(totals.bytesIn)} / ${formatBytes(totals.bytesOut)}`} tone="accent" />
        <KpiTile
          label="Threat hits"
          value={String(totals.threats)}
          tone={totals.threats > 0 ? 'critical' : 'healthy'}
        />
      </div>

      <Panel
        padding="md"
        eyebrow="LIFECYCLES"
        title={`Connections to/from ${ip}`}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {TIME_WINDOWS.map((w) => (
              <Button
                key={w.label}
                variant={windowMs === w.ms ? 'primary' : 'ghost'}
                size="sm"
                onClick={() => setWindowMs(w.ms)}
              >
                {w.label}
              </Button>
            ))}
            <Button
              variant="secondary"
              size="sm"
              disabled={compareDisabled}
              onClick={onCompare}
              title={compareDisabled ? 'Select exactly two lifecycles to compare' : 'Compare selected'}
            >
              <GitCompare className="h-4 w-4" />
              Compare ({selected.size})
            </Button>
            <IpActionMenu ip={ip} />
          </div>
        }
      >
        {query.isLoading && <Loader size="md" label="Loading lifecycles…" />}
        {query.error && <Alert variant="critical">{(query.error as Error).message}</Alert>}
        {!query.isLoading && rows.length === 0 ? (
          <EmptyState
            title="No lifecycles found"
            description={`No connections involving ${ip} in the selected time window.`}
          />
        ) : (
          <>
            <TimelineStrip groupedByNode={groupedByNode} nodesById={nodesById} since={since} />
            <table className="mt-4 w-full text-left text-sm">
              <thead>
                <tr className="border-b border-border-subtle text-text-muted">
                  <th className="w-8 py-2"></th>
                  <th className="py-2 font-mono text-[0.65rem] uppercase tracking-wider">Started</th>
                  <th className="py-2 font-mono text-[0.65rem] uppercase tracking-wider">Node</th>
                  <th className="py-2 font-mono text-[0.65rem] uppercase tracking-wider">Process</th>
                  <th className="py-2 font-mono text-[0.65rem] uppercase tracking-wider">Peer</th>
                  <th className="py-2 font-mono text-[0.65rem] uppercase tracking-wider">Bytes in/out</th>
                  <th className="py-2 font-mono text-[0.65rem] uppercase tracking-wider">Duration</th>
                  <th className="py-2 font-mono text-[0.65rem] uppercase tracking-wider">Threat</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const isSelected = selected.has(r.conn_id);
                  const node = r.node_id ? nodesById.get(r.node_id) : null;
                  // Show whichever endpoint is the *other* side of the
                  // connection from the IP we're investigating.
                  const peer = r.src_ip === ip
                    ? `${r.dst_ip ?? ''}:${r.dst_port ?? ''}`
                    : `${r.src_ip ?? ''}:${r.src_port ?? ''}`;
                  return (
                    <tr
                      key={r.conn_id}
                      className={cn(
                        'cursor-pointer border-b border-border-subtle/50 transition-colors hover:bg-hover',
                        isSelected && 'bg-brand-500/8',
                      )}
                      onClick={() => setOpenConn(r.conn_id)}
                    >
                      <td className="py-2" onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => toggleSelect(r.conn_id)}
                          aria-label={`Select ${r.conn_id}`}
                          className="accent-brand-500"
                        />
                      </td>
                      <td className="py-2 font-mono text-xs text-text-secondary">
                        {formatTs(r.started_at)}
                      </td>
                      <td className="py-2 font-mono text-xs text-text-secondary">
                        {node?.hostname ?? r.node_id?.slice(0, 8) ?? '—'}
                      </td>
                      <td className="py-2 text-xs">
                        {r.process_name ?? '—'}
                        {r.pid && (
                          <span className="ml-1 font-mono text-[0.65rem] text-text-muted">pid {r.pid}</span>
                        )}
                      </td>
                      <td className="py-2 font-mono text-xs">{peer}</td>
                      <td className="py-2 font-mono text-xs tabular-nums">
                        {formatBytes(r.bytes_in ?? 0)} / {formatBytes(r.bytes_out ?? 0)}
                      </td>
                      <td className="py-2 font-mono text-xs tabular-nums text-text-secondary">
                        {formatDuration(r.duration_ms)}
                      </td>
                      <td className="py-2">
                        {r.threat_match ? (
                          <StatusTag tone="critical">{r.threat_feed ?? 'match'}</StatusTag>
                        ) : (
                          <StatusTag tone="healthy">clean</StatusTag>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </>
        )}
      </Panel>

      <ConnectionDetailSheet
        connId={openConn}
        onClose={() => setOpenConn(null)}
      />
    </div>
  );
}

function TimelineStrip({
  groupedByNode,
  nodesById,
  since,
}: {
  groupedByNode: Map<string, ConnectionRow[]>;
  nodesById: Map<string, { hostname: string }>;
  since: string;
}) {
  const sinceMs = new Date(since).getTime();
  const nowMs = Date.now();
  const span = Math.max(1, nowMs - sinceMs);

  const entries = Array.from(groupedByNode.entries()).slice(0, 12);
  if (entries.length === 0) return null;

  return (
    <div className="rounded-md border border-border-subtle bg-surface-2 p-3">
      <div className="flex flex-col gap-1.5">
        {entries.map(([nodeId, conns]) => {
          const hostname = nodesById.get(nodeId)?.hostname ?? nodeId.slice(0, 8);
          return (
            <div key={nodeId} className="flex items-center gap-2">
              <span className="w-32 truncate font-mono text-[0.65rem] text-text-muted">
                {hostname}
              </span>
              <div className="relative h-4 flex-1 rounded-full bg-surface">
                {conns.map((c) => {
                  const start = Math.max(0, new Date(c.started_at).getTime() - sinceMs);
                  const end = c.ended_at ? new Date(c.ended_at).getTime() - sinceMs : nowMs - sinceMs;
                  const left = (start / span) * 100;
                  const width = Math.max(0.5, ((end - start) / span) * 100);
                  return (
                    <span
                      key={c.conn_id}
                      title={`${c.process_name ?? ''} · ${formatBytes((c.bytes_in ?? 0) + (c.bytes_out ?? 0))}`}
                      className={cn(
                        'absolute top-0 h-full rounded-sm',
                        c.threat_match ? 'bg-state-critical/70' : 'bg-brand-500/60',
                      )}
                      style={{ left: `${left}%`, width: `${width}%` }}
                    />
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
