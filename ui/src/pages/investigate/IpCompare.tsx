import { useMemo, type ReactNode } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import {
  Alert,
  Eyebrow,
  Loader,
  Panel,
  SectionHeader,
} from '@/components/kit';
import { useConnectionDetail } from '@/hooks/useConnectionsByIp';
import { useTenant } from '@/providers/TenantProvider';
import { formatBytes, formatDuration, formatTs } from '@/lib/format';
import type { ConnectionDetail } from '@/lib/api';
import { cn } from '@/lib/utils';

export function IpCompare(): JSX.Element {
  const { id: ip = '' } = useParams<{ id: string }>();
  const [params] = useSearchParams();
  const a = params.get('a') ?? '';
  const b = params.get('b') ?? '';
  const { currentTenantId } = useTenant();

  const aQ = useConnectionDetail(a, currentTenantId);
  const bQ = useConnectionDetail(b, currentTenantId);

  const loading = aQ.isLoading || bQ.isLoading;
  const error = aQ.error || bQ.error;

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center gap-2 text-xs">
        <Link
          to={`/investigate/ip/${encodeURIComponent(ip)}`}
          className="inline-flex items-center gap-1 text-text-muted transition-colors hover:text-foreground"
        >
          <ArrowLeft className="h-3.5 w-3.5" /> Back to {ip}
        </Link>
      </div>
      <SectionHeader
        eyebrow="INVESTIGATE · COMPARE LIFECYCLES"
        title={`Side-by-side: ${ip}`}
        description="Two connection lifecycles compared on the metrics that matter — time, duration, bytes, threat. Larger values get warning highlights; equal values show once muted."
      />

      {error && <Alert variant="critical">{(error as Error).message}</Alert>}
      {loading && <Loader size="md" label="Loading both lifecycles…" />}

      {aQ.data && bQ.data && (
        <DiffStrip a={aQ.data} b={bQ.data} />
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {aQ.data && <ConnectionPanel detail={aQ.data} eyebrow="A" />}
        {bQ.data && <ConnectionPanel detail={bQ.data} eyebrow="B" />}
      </div>
    </div>
  );
}

function ConnectionPanel({ detail, eyebrow }: { detail: ConnectionDetail; eyebrow: string }) {
  const c = detail.connection;
  return (
    <Panel padding="md" eyebrow={eyebrow} title={c.conn_id}>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <Term label="Process">
          {c.process_name ?? '—'} <span className="text-text-muted">· pid {c.pid ?? '—'}</span>
        </Term>
        <Term label="User">{c.user_name ?? '—'}</Term>
        <Term label="Source">{c.src_ip}:{c.src_port}</Term>
        <Term label="Destination">{c.dst_ip}:{c.dst_port}</Term>
        <Term label="Started">{formatTs(c.started_at)}</Term>
        <Term label="Ended">{c.ended_at ? formatTs(c.ended_at) : '— still active'}</Term>
        <Term label="Duration">{formatDuration(c.duration_ms)}</Term>
        <Term label="Bytes in">{formatBytes(c.bytes_in ?? 0)}</Term>
        <Term label="Bytes out">{formatBytes(c.bytes_out ?? 0)}</Term>
        <Term label="Threat">
          {c.threat_match ? `${c.threat_feed ?? 'match'} · score ${c.threat_score ?? 0}` : 'clean'}
        </Term>
      </dl>
    </Panel>
  );
}

function DiffStrip({ a, b }: { a: ConnectionDetail; b: ConnectionDetail }) {
  const ca = a.connection;
  const cb = b.connection;

  // a and b reference the connection objects we already memoise via
  // react-query; rebuilding cells whenever either one changes identity is
  // the intent — the rule's "missing primitive deps" warning is a false
  // positive when the parent objects are themselves the cache keys.
  const cells = useMemo(
    () => [
      {
        label: 'Time connected',
        a: ca.started_at,
        b: cb.started_at,
        format: formatTs,
        cmp: (x: string, y: string) => new Date(x).getTime() - new Date(y).getTime(),
      },
      {
        label: 'Duration',
        a: ca.duration_ms ?? 0,
        b: cb.duration_ms ?? 0,
        format: (v: number) => formatDuration(v),
        cmp: (x: number, y: number) => x - y,
      },
      {
        label: 'Bytes in',
        a: ca.bytes_in ?? 0,
        b: cb.bytes_in ?? 0,
        format: (v: number) => formatBytes(v),
        cmp: (x: number, y: number) => x - y,
      },
      {
        label: 'Bytes out',
        a: ca.bytes_out ?? 0,
        b: cb.bytes_out ?? 0,
        format: (v: number) => formatBytes(v),
        cmp: (x: number, y: number) => x - y,
      },
      {
        label: 'Threat',
        a: ca.threat_match ? `${ca.threat_feed ?? 'match'} (${ca.threat_score ?? 0})` : 'clean',
        b: cb.threat_match ? `${cb.threat_feed ?? 'match'} (${cb.threat_score ?? 0})` : 'clean',
        format: (v: string) => v,
        cmp: (x: string, y: string) => (x === y ? 0 : x === 'clean' ? -1 : 1),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [a, b],
  );

  return (
    <Panel padding="md" eyebrow="DIFF SUMMARY" title="At-a-glance differences">
      <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
        {cells.map((cell) => {
          const cmp = cell.cmp(cell.a as never, cell.b as never);
          const equal = cmp === 0;
          if (equal) {
            return (
              <DiffCell key={cell.label} label={cell.label} value={cell.format(cell.a as never)} muted />
            );
          }
          const aLarger = cmp > 0;
          return (
            <DiffPair
              key={cell.label}
              label={cell.label}
              left={cell.format(cell.a as never)}
              right={cell.format(cell.b as never)}
              leftWarn={aLarger}
              rightWarn={!aLarger}
            />
          );
        })}
      </dl>
    </Panel>
  );
}

function Term({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{label}</dt>
      <dd className="text-text-secondary">{children}</dd>
    </>
  );
}

function DiffCell({ label, value, muted }: { label: string; value: string; muted?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-border-subtle bg-surface px-3 py-2">
      <Eyebrow>{label}</Eyebrow>
      <span className={cn('text-sm', muted ? 'text-text-muted' : 'text-foreground')}>{value}</span>
    </div>
  );
}

function DiffPair({
  label,
  left,
  right,
  leftWarn,
  rightWarn,
}: {
  label: string;
  left: string;
  right: string;
  leftWarn?: boolean;
  rightWarn?: boolean;
}) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-border-subtle bg-surface px-3 py-2">
      <Eyebrow>{label}</Eyebrow>
      <div className="flex flex-col gap-0.5 text-sm">
        <span className={cn('font-mono text-xs', leftWarn ? 'text-state-warning' : 'text-text-muted')}>
          A · {left}
        </span>
        <span className={cn('font-mono text-xs', rightWarn ? 'text-state-warning' : 'text-text-muted')}>
          B · {right}
        </span>
      </div>
    </div>
  );
}
