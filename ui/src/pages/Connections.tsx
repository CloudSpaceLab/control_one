import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowRight, RefreshCw } from 'lucide-react';
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
import { hasConnectionShape, isExternalConnection, isPublicIP } from '@/lib/network';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import type { ConnectionRow } from '@/lib/api';
import type { ColumnDef } from '@tanstack/react-table';

export function Connections(): JSX.Element {
  const client = useApiClient();
  const navigate = useNavigate();
  const { currentTenantId } = useTenant();

  const [rows, setRows] = useState<ConnectionRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [ipInput, setIpInput] = useState('');
  const [threatOnly, setThreatOnly] = useState(false);
  const [showInternal, setShowInternal] = useState(false);
  const [openConnId, setOpenConnId] = useState<string | null>(null);
  const since = useMemo(() => new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(), []);

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setRows([]);
      return;
    }
    setLoading(true);
    try {
      const resp = await client.listConnections({
        tenantId: currentTenantId,
        since,
        limit: 500,
      });
      setRows(resp);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId, since]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    const value = ipInput.trim();
    if (isIpv4(value)) {
      navigate(entityRoute('ip', value));
      return;
    }
    void refresh();
  };

  const shapedRows = useMemo(() => rows.filter(hasConnectionShape), [rows]);
  const filteredRows = useMemo(
    () => shapedRows.filter((row) => !threatOnly || row.threat_match),
    [shapedRows, threatOnly],
  );
  const visibleRows = useMemo(
    () => filteredRows.filter((row) => showInternal || isExternalConnection(row)),
    [filteredRows, showInternal],
  );
  const hiddenRows = Math.max(0, filteredRows.length - visibleRows.length);
  const incompleteRows = Math.max(0, rows.length - shapedRows.length);

  const totals = useMemo(
    () => ({
      bytesOut: visibleRows.reduce((s, r) => s + (r.bytes_out ?? 0), 0),
      bytesIn: visibleRows.reduce((s, r) => s + (r.bytes_in ?? 0), 0),
      threatHits: visibleRows.filter((r) => r.threat_match).length,
    }),
    [visibleRows],
  );

  const columns = useMemo<ColumnDef<ConnectionRow>[]>(
    () => [
      {
        header: 'Started',
        accessorKey: 'started_at',
        cell: ({ row }) => (
          <span className="font-mono text-xs tabular-nums text-text-secondary">
            {formatTs(row.original.started_at)}
          </span>
        ),
      },
      {
        header: 'Node',
        accessorKey: 'node_id',
        cell: ({ row }) => (
          <span className="font-mono text-xs text-text-secondary">
            {row.original.node_id ? row.original.node_id.slice(0, 8) : '-'}
          </span>
        ),
      },
      {
        header: 'Process',
        accessorKey: 'process_name',
        cell: ({ row }) => (
          <span>
            {row.original.process_name ?? '-'}
            {row.original.pid ? (
              <span className="ml-1 font-mono text-[0.7rem] text-text-muted">
                pid {row.original.pid}
              </span>
            ) : null}
          </span>
        ),
      },
      {
        header: 'User',
        accessorKey: 'user_name',
        cell: ({ row }) => row.original.user_name ?? '-',
      },
      {
        header: 'Source',
        accessorKey: 'src_ip',
        cell: ({ row }) => (
          <span
            className="inline-flex items-center gap-1 font-mono text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            {row.original.src_ip ?? '-'}
            {row.original.src_port ? `:${row.original.src_port}` : ''}
            {row.original.src_ip && isPublicIP(row.original.src_ip) ? <IpActionMenu ip={row.original.src_ip} /> : null}
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
            {row.original.dst_ip ?? '-'}
            {row.original.dst_port ? `:${row.original.dst_port}` : ''}
            {row.original.dst_ip && isPublicIP(row.original.dst_ip) ? <IpActionMenu ip={row.original.dst_ip} /> : null}
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
        eyebrow="DETECT & RESPOND - NETWORK FORENSICS"
        title="Connections"
        description="Recent external connections across every node. Click a row for the forensic timeline, or enter a full IP to pivot to the cross-node investigation view."
      />

      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={handleSubmit}
        role="search"
        aria-label="Filter connections"
      >
        <Input
          type="search"
          placeholder="IP address, e.g. 8.8.8.8"
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
        <label className="inline-flex select-none items-center gap-2 text-sm text-text-secondary">
          <input
            type="checkbox"
            checked={showInternal}
            onChange={(e) => setShowInternal(e.target.checked)}
            className="accent-brand-500"
          />
          Show internal/private
        </label>
        <Button type="submit" variant="secondary" size="md" disabled={loading}>
          {isIpv4(ipInput.trim()) ? (
            <>
              Investigate IP <ArrowRight className="h-4 w-4" />
            </>
          ) : (
            <>
              <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> Refresh
            </>
          )}
        </Button>
      </form>

      {error && <Alert variant="critical">{error}</Alert>}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="Total bytes out" value={formatBytes(totals.bytesOut)} tone="warning" />
        <KpiTile label="Total bytes in" value={formatBytes(totals.bytesIn)} tone="healthy" />
        <KpiTile
          label="Threat hits"
          value={String(totals.threatHits)}
          tone={totals.threatHits > 0 ? 'critical' : 'healthy'}
        />
      </div>

      {!showInternal && hiddenRows > 0 && (
        <p className="text-xs text-text-muted">
          Showing external peers only; {hiddenRows} internal or listener row{hiddenRows === 1 ? '' : 's'} hidden.
        </p>
      )}
      {incompleteRows > 0 && (
        <p className="text-xs text-text-muted">
          Suppressed {incompleteRows} incomplete placeholder row{incompleteRows === 1 ? '' : 's'} with no usable peer, process, or port.
        </p>
      )}

      {visibleRows.length === 0 && !loading ? (
        <EmptyState
          title={showInternal ? 'No connections found' : 'No external connections found'}
          description={showInternal
            ? 'No recent connection rows matched the current filters.'
            : 'Recent rows were internal, listener-only, incomplete, or filtered out. Toggle Show internal/private to inspect them.'}
        />
      ) : (
        <DataTable
          columns={columns}
          rows={visibleRows}
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
