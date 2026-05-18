import { useEffect, useState } from 'react';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Loader, Eyebrow } from '@/components/kit';
import { Badge } from '@/components/Badge';
import EventTimeline from '@/components/EventTimeline';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { formatBytes, formatDuration, formatTs } from '@/lib/format';
import type { ConnectionDetail } from '@/lib/api';

export interface ConnectionDetailSheetProps {
  /** Connection id to fetch + render. Sheet is open while truthy. */
  connId: string | null;
  onClose: () => void;
}

export function ConnectionDetailSheet({ connId, onClose }: ConnectionDetailSheetProps) {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [detail, setDetail] = useState<ConnectionDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!connId || !currentTenantId) {
      setDetail(null);
      setLoading(false);
      setError(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    client
      .getConnectionDetail(connId, { tenantId: currentTenantId })
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'load failed');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, connId, currentTenantId]);

  return (
    <Sheet open={!!connId} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full overflow-y-auto p-6 sm:max-w-[540px]">
        <SheetHeader className="text-left">
          <Eyebrow>Connection</Eyebrow>
          <SheetTitle className="break-all font-mono text-sm">
            {connId ?? '—'}
          </SheetTitle>
          <SheetDescription className="sr-only">
            Connection lifecycle metadata and forensic timeline.
          </SheetDescription>
        </SheetHeader>

        {loading && (
          <div className="mt-4">
            <Loader label="Loading connection…" />
          </div>
        )}
        {error && (
          <p className="mt-4 text-sm text-state-critical">{error}</p>
        )}

        {detail && <DetailBody detail={detail} />}
      </SheetContent>
    </Sheet>
  );
}

function DetailBody({ detail }: { detail: ConnectionDetail }) {
  const c = detail.connection;
  return (
    <>
      <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <Term label="Process">
          {c.process_name ?? '—'}{' '}
          <span className="text-text-muted">· pid {c.pid ?? '—'}</span>
        </Term>
        <Term label="User">{c.user_name ?? '—'}</Term>
        <Term label="Source">
          {c.src_ip}:{c.src_port}
        </Term>
        <Term label="Destination">
          {c.dst_ip}:{c.dst_port}{' '}
          <span className="text-text-muted">({c.protocol ?? 'tcp'})</span>
        </Term>
        <Term label="Started">{formatTs(c.started_at)}</Term>
        <Term label="Ended">{c.ended_at ? formatTs(c.ended_at) : '— still active'}</Term>
        <Term label="Duration">{formatDuration(c.duration_ms)}</Term>
        <Term label="Bytes in / out">
          {formatBytes(c.bytes_in ?? 0)}{' '}
          / <strong className="text-foreground">{formatBytes(c.bytes_out ?? 0)}</strong>
        </Term>
        {c.threat_match && (
          <Term label="Threat">
            <Badge variant="critical">
              {c.threat_feed ?? 'match'} · score {c.threat_score ?? 0}
            </Badge>
          </Term>
        )}
        {c.bastion_session_id && (
          <Term label="Bastion session">
            <span className="font-mono text-[0.7rem]">{c.bastion_session_id}</span>
          </Term>
        )}
      </dl>

      <h4 className="mt-6 font-display text-sm font-semibold text-foreground">
        Forensic timeline
      </h4>
      <div className="mt-2">
        <EventTimeline events={detail.events ?? []} />
      </div>
    </>
  );
}

function Term({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <dt className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
        {label}
      </dt>
      <dd className="text-text-secondary">{children}</dd>
    </>
  );
}
