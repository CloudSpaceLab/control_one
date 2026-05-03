import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowRight } from 'lucide-react';
import {
  Alert,
  DataTable,
  EmptyState,
  IpActionMenu,
  KpiTile,
  SectionHeader,
  StatusDot,
} from '@/components/kit';
import { Badge } from '@/components/Badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ConnectionDetailSheet } from '@/components/investigate/ConnectionDetailSheet';
import { entityRoute } from '@/lib/entity';
import { formatBytes, formatDuration, formatTs, isIpv4 } from '@/lib/format';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import type { ConnectionRow } from '@/lib/api';
import type { ColumnDef } from '@tanstack/react-table';

// Connections — full lifecycle table. Click a row → forensic timeline sheet
// keyed by correlation_id. When the user types a complete IP and submits
// (Enter / Investigate IP →), we redirect to the canonical IP investigate
// page (cross-node aggregate view + side-by-side compare).
export function Connections(): JSX.Element {
  const client = useApiClient();
  const navigate = useNavigate();
  const { currentTenantId } = useTenant();

  const [rows, setRows] = useState<ConnectionRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [ipInput, setIpInput] = useState('');
  const [appliedIp, setAppliedIp] = useState('');
  const [threatOnly, setThreatOnly] = useState(false);
  const [openConnId, setOpenConnId] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!appliedIp) return;
    setLoading(true);
    try {
      const resp = await client.listConnections({
        tenantId: currentTenantId ?? undefined,
        ip: appliedIp,
        limit: 200,
      });
      setRows(threatOnly ? resp.filter((r) => r.threat_match) : resp);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId, appliedIp, threatOnly]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    const value = ipInput.trim();
    if (isIpv4(value)) {
      // Canonical IP investigate page handles cross-node aggregation, compare,
      // and lifecycle drill-in — funnel users there instead of duplicating.
      navigate(entityRoute('ip', value));
      return;
    }
    setAppliedIp(value);
  };

  const totals = useMemo(
    () => ({
      bytesOut: rows.reduce((s, r) => s + (r.bytes_out ?? 0), 0),
      bytesIn: rows.reduce((s, r) => s + (r.bytes_in ?? 0), 0),
      threatHits: rows.filter((r) => r.threat_match).length,
    }),
    [rows],
  );

  const columns = useMemo<ColumnDef<ConnectionRow>[]>(
    () => [
      {
        header: 'Started',
        accessorKey: 'started_at',
        cell: ({ row }) => (
          <span className="font-mono text-xs text-text-secondary tabular-nums">
            {formatTs(row.original.started_at)}
          </span>
        ),
      },
      {
        header: 'Process',
        accessorKey: 'process_name',
        cell: ({ row }) => (
          <span>
            {row.original.process_name ?? '—'}
            {row.original.pid && (
              <span className="ml-1 font-mono text-[0.7rem] text-text-muted">
                pid {row.original.pid}
              </span>
            )}
          </span>
        ),
      },
      {
        header: 'User',
        accessorKey: 'user_name',
        cell: ({ row }) => row.original.user_name ?? '—',
      },
      {
        header: 'Source',
        accessorKey: 'src_ip',
        cell: ({ row }) => (
          <span
            className="inline-flex items-center gap-1 font-mono text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            {row.original.src_ip ?? '—'}
            {row.original.src_port ? `:${row.original.src_port}` : ''}
            {row.original.src_ip && <IpActionMenu ip={row.original.src_ip} />}
          </span>
        ),
      },
      {
        header: 'Destination',
        accessorKey: 'dst_ip',
        cell: ({ row }) => (
          <span
            className="inline-flex items-center gap-1 font-mono text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            {row.original.dst_ip ?? '—'}
            {row.original.dst_port ? `:${row.original.dst_port}` : ''}
            {row.original.dst_ip && <IpActionMenu ip={row.original.dst_ip} />}
          </span>
        ),
      },
      {
        header: 'Bytes out',
        accessorKey: 'bytes_out',
        cell: ({ row }) => (
          <span className="font-mono text-xs tabular-nums">
            {formatBytes(row.original.bytes_out ?? 0)}
          </span>
        ),
      },
      {
        header: 'Duration',
        accessorKey: 'duration_ms',
        cell: ({ row }) => (
          <span className="font-mono text-xs tabular-nums text-text-secondary">
            {formatDuration(row.original.duration_ms)}
          </span>
        ),
      },
      {
        header: 'Threat',
        accessorKey: 'threat_match',
        cell: ({ row }) =>
          row.original.threat_match ? (
            <Badge variant="critical">{row.original.threat_feed ?? 'match'}</Badge>
          ) : (
            <StatusDot tone="healthy" size="xs" label="clean" />
          ),
      },
    ],
    [],
  );

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="DETECT & RESPOND · NETWORK FORENSICS"
        title="Connections"
        description="Every external connection across every node — process, user, bytes, duration, threat. Click a row to see correlated files, queries, and log events; type an IP to pivot to the cross-node investigate view."
      />

      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={handleSubmit}
        role="search"
        aria-label="Filter connections"
      >
        <Input
          type="search"
          placeholder="IP address (8.8.8.8) — Enter to investigate cross-node"
          value={ipInput}
          onChange={(e) => setIpInput(e.target.value)}
          className="max-w-md"
        />
        <label className="inline-flex select-none items-center gap-2 text-sm text-text-secondary">
          <input
            type="checkbox"
            checked={threatOnly}
            onChange={(e) => setThreatOnly(e.target.checked)}
            className="accent-brand-500"
          />
          Threat-match only
        </label>
        <Button type="submit" variant="secondary" size="md">
          {isIpv4(ipInput.trim()) ? (
            <>
              Investigate IP <ArrowRight className="h-4 w-4" />
            </>
          ) : (
            'Refresh'
          )}
        </Button>
      </form>

      {error && <Alert variant="critical">{error}</Alert>}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile
          label="Total bytes out"
          value={formatBytes(totals.bytesOut)}
          tone="warning"
        />
        <KpiTile
          label="Total bytes in"
          value={formatBytes(totals.bytesIn)}
          tone="healthy"
        />
        <KpiTile
          label="Threat hits"
          value={String(totals.threatHits)}
          tone={totals.threatHits > 0 ? 'critical' : 'healthy'}
        />
      </div>

      {rows.length === 0 && !loading && !appliedIp ? (
        <EmptyState
          title="Enter an IP to load connections"
          description="Type a source or destination IP and press Enter — you'll be sent to the cross-node investigate view, with side-by-side compare for any two lifecycles."
        />
      ) : rows.length === 0 && !loading ? (
        <EmptyState
          title="No connections found"
          description="Nothing matches the current filter in this window."
        />
      ) : (
        <DataTable
          columns={columns}
          rows={rows}
          rowKey={(r) => r.conn_id}
          loading={loading}
          onRowClick={(r) => setOpenConnId(r.conn_id)}
          sticky
        />
      )}

      <ConnectionDetailSheet
        connId={openConnId}
        onClose={() => setOpenConnId(null)}
      />
    </div>
  );
}
