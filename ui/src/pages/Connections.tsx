import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { SectionHeader, IpActionMenu } from '../components/kit';
import { Badge } from '../components/Badge';
import { EmptyState } from '../components/EmptyState';
import EventTimeline from '../components/EventTimeline';
import StatusDot from '../components/glyphs/StatusDot';
import { useTenant } from '../providers/TenantProvider';
import type { ConnectionDetail, ConnectionRow } from '../lib/api';

// Connections — full lifecycle table the user asked for. src/dst, pid,
// process, started/finished, duration, bytes_out, threat_match. Click a
// row → forensic timeline panel keyed by correlation_id.
export function Connections(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [rows, setRows] = useState<ConnectionRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<{ ip?: string; threatOnly: boolean }>({ threatOnly: false });
  const [selected, setSelected] = useState<ConnectionDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const refresh = useCallback(async () => {
    // Backend requires both tenant_id and ip — skip auto-load until ip is entered
    if (!filter.ip) return;
    setLoading(true);
    try {
      const resp = await client.listConnections({ tenantId: currentTenantId ?? undefined, ip: filter.ip, limit: 100 });
      setRows(filter.threatOnly ? resp.filter((r) => r.threat_match) : resp);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId, filter.ip, filter.threatOnly]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const openDetail = useCallback(
    async (row: ConnectionRow) => {
      setDetailLoading(true);
      try {
        const detail = await client.getConnectionDetail(row.conn_id);
        setSelected(detail);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'load failed');
      } finally {
        setDetailLoading(false);
      }
    },
    [client],
  );

  const totals = useMemo(
    () => ({
      bytesOut: rows.reduce((s, r) => s + (r.bytes_out ?? 0), 0),
      bytesIn: rows.reduce((s, r) => s + (r.bytes_in ?? 0), 0),
      threatHits: rows.filter((r) => r.threat_match).length,
    }),
    [rows],
  );

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="DETECT & RESPOND · NETWORK FORENSICS"
        title="Connections"
        description="Every external connection on every node — process, user, bytes in/out, duration. Click a row to see files touched, DB queries, and log events that share its correlation window."
      />
      <div className="flex items-center gap-2">
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <input
            type="search"
            placeholder="Filter by IP…"
            value={filter.ip ?? ''}
            onChange={(e) => setFilter((f) => ({ ...f, ip: e.target.value || undefined }))}
            className="search-input"
          />
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13 }}>
            <input
              type="checkbox"
              checked={filter.threatOnly}
              onChange={(e) => setFilter((f) => ({ ...f, threatOnly: e.target.checked }))}
            />
            Threat-match only
          </label>
          <button type="button" className="secondary-button" onClick={refresh} disabled={loading}>
            {loading ? 'Loading…' : 'Refresh'}
          </button>
        </div>
      </div>

      {error ? <p className="error-banner">{error}</p> : null}

      <div className="card-grid" style={{ gridTemplateColumns: 'repeat(3, 1fr)', marginBottom: 16 }}>
        <Stat label="Total bytes out" value={formatBytes(totals.bytesOut)} state="warning" />
        <Stat label="Total bytes in" value={formatBytes(totals.bytesIn)} state="healthy" />
        <Stat label="Threat hits" value={String(totals.threatHits)} state={totals.threatHits > 0 ? 'critical' : 'healthy'} />
      </div>

      {rows.length === 0 && !loading ? (
        <EmptyState
          title={filter.ip ? 'No connections found' : 'Enter an IP address to search'}
          description={
            filter.ip
              ? 'No connections match the current filter in this window.'
              : 'Type a source or destination IP in the filter above, then click Refresh to load connections.'
          }
        />
      ) : (
        <table className="data-table" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th>Started</th>
              <th>Process</th>
              <th>User</th>
              <th>Source</th>
              <th>Destination</th>
              <th>Bytes out</th>
              <th>Duration</th>
              <th>Threat</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.conn_id} onClick={() => openDetail(r)} style={{ cursor: 'pointer' }}>
                <td>{formatTs(r.started_at)}</td>
                <td>
                  {r.process_name ?? '—'}
                  {r.pid ? <small style={{ color: 'var(--text-secondary)' }}> · pid {r.pid}</small> : null}
                </td>
                <td>{r.user_name ?? '—'}</td>
                <td onClick={(e) => e.stopPropagation()}>
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                    {r.src_ip ?? '—'}
                    {r.src_port ? `:${r.src_port}` : ''}
                    {r.src_ip ? <IpActionMenu ip={r.src_ip} /> : null}
                  </span>
                </td>
                <td onClick={(e) => e.stopPropagation()}>
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                    {r.dst_ip ?? '—'}
                    {r.dst_port ? `:${r.dst_port}` : ''}
                    {r.dst_ip ? <IpActionMenu ip={r.dst_ip} /> : null}
                  </span>
                </td>
                <td>{formatBytes(r.bytes_out ?? 0)}</td>
                <td>{formatDuration(r.duration_ms)}</td>
                <td>
                  {r.threat_match ? (
                    <Badge variant="critical">{r.threat_feed ?? 'match'}</Badge>
                  ) : (
                    <StatusDot state="healthy" title="clean" />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {selected ? (
        <DetailPanel
          detail={selected}
          loading={detailLoading}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </div>
  );
}

function Stat({ label, value, state }: { label: string; value: string; state: 'healthy' | 'warning' | 'critical' }) {
  return (
    <div className="card" style={{ padding: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
        <StatusDot state={state} />
        <small style={{ color: 'var(--text-secondary)' }}>{label}</small>
      </div>
      <div style={{ fontSize: 22, fontWeight: 600 }}>{value}</div>
    </div>
  );
}

function DetailPanel({
  detail,
  loading,
  onClose,
}: {
  detail: ConnectionDetail;
  loading: boolean;
  onClose: () => void;
}) {
  const c = detail.connection;
  return (
    <aside
      style={{
        position: 'fixed',
        right: 0,
        top: 0,
        bottom: 0,
        width: 'min(540px, 90vw)',
        background: 'var(--bg-secondary)',
        borderLeft: '1px solid var(--border-color)',
        boxShadow: `0 0 24px var(--shadow)`,
        padding: 24,
        overflow: 'auto',
        zIndex: 100,
        animation: `slide-in var(--motion-med) var(--ease-out)`,
      }}
    >
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16 }}>
        <div>
          <p className="eyebrow">Connection</p>
          <h3 style={{ marginTop: 4, wordBreak: 'break-all' }}>{c.conn_id}</h3>
        </div>
        <button type="button" className="secondary-button" onClick={onClose}>
          Close
        </button>
      </header>

      <dl style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', fontSize: 13, marginTop: 16 }}>
        <dt>Process</dt>
        <dd>{c.process_name ?? '—'} · pid {c.pid ?? '—'}</dd>
        <dt>User</dt>
        <dd>{c.user_name ?? '—'}</dd>
        <dt>Source</dt>
        <dd>{c.src_ip}:{c.src_port}</dd>
        <dt>Destination</dt>
        <dd>{c.dst_ip}:{c.dst_port} ({c.protocol ?? 'tcp'})</dd>
        <dt>Started</dt>
        <dd>{formatTs(c.started_at)}</dd>
        <dt>Ended</dt>
        <dd>{c.ended_at ? formatTs(c.ended_at) : '— still active'}</dd>
        <dt>Duration</dt>
        <dd>{formatDuration(c.duration_ms)}</dd>
        <dt>Bytes in / out</dt>
        <dd>
          {formatBytes(c.bytes_in ?? 0)} / <strong>{formatBytes(c.bytes_out ?? 0)}</strong>
        </dd>
        {c.threat_match ? (
          <>
            <dt>Threat</dt>
            <dd>
              <Badge variant="critical">{c.threat_feed ?? 'match'} · score {c.threat_score ?? 0}</Badge>
            </dd>
          </>
        ) : null}
        {c.bastion_session_id ? (
          <>
            <dt>Bastion session</dt>
            <dd style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12 }}>{c.bastion_session_id}</dd>
          </>
        ) : null}
      </dl>

      <h4 style={{ marginTop: 24 }}>Forensic timeline</h4>
      {loading ? (
        <p style={{ color: 'var(--text-secondary)' }}>Loading correlated events…</p>
      ) : (
        <EventTimeline events={detail.events ?? []} />
      )}

      <style>{`@keyframes slide-in { from { transform: translateX(100%); } to { transform: translateX(0); } }`}</style>
    </aside>
  );
}

function formatTs(iso?: string): string {
  if (!iso) return '—';
  try {
    const d = new Date(iso);
    return d.toLocaleString();
  } catch {
    return iso;
  }
}

function formatBytes(n: number): string {
  if (!n) return '0 B';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function formatDuration(ms?: number): string {
  if (!ms) return '—';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`;
  return `${Math.floor(ms / 3_600_000)}h ${Math.floor((ms % 3_600_000) / 60_000)}m`;
}
