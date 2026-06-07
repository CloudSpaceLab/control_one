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
import type {
  ConnectionDetail,
  ForensicEvent,
  InvestigationTimelineItem,
} from '@/lib/api';

export interface ConnectionDetailSheetProps {
  /** Connection id to fetch + render. Sheet is open while truthy. */
  connId: string | null;
  onClose: () => void;
}

export function ConnectionDetailSheet({ connId, onClose }: ConnectionDetailSheetProps) {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [detail, setDetail] = useState<ConnectionDetail | null>(null);
  const [timelineEvents, setTimelineEvents] = useState<ForensicEvent[]>([]);
  const [timelineLoading, setTimelineLoading] = useState(false);
  const [timelineError, setTimelineError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!connId || !currentTenantId) {
      setDetail(null);
      setTimelineEvents([]);
      setTimelineLoading(false);
      setTimelineError(null);
      setLoading(false);
      setError(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setTimelineLoading(true);
    setError(null);
    setTimelineError(null);
    setTimelineEvents([]);
    Promise.allSettled([
      client.getConnectionDetail(connId, { tenantId: currentTenantId }),
      client.buildInvestigationTimeline({
        tenantId: currentTenantId,
        connId,
        entityType: 'connection',
        entityId: connId,
        limit: 25,
      }),
    ]).then(([detailResult, timelineResult]) => {
      if (cancelled) return;
      if (detailResult.status === 'fulfilled') {
        setDetail(detailResult.value);
      } else {
        setError(detailResult.reason instanceof Error ? detailResult.reason.message : 'load failed');
      }
      if (timelineResult.status === 'fulfilled') {
        setTimelineEvents(timelineResult.value.items.map(timelineItemToForensicEvent));
      } else {
        setTimelineError(
          timelineResult.reason instanceof Error ? timelineResult.reason.message : 'timeline unavailable',
        );
      }
    }).finally(() => {
      if (!cancelled) {
        setLoading(false);
        setTimelineLoading(false);
      }
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

        {detail && (
          <DetailBody
            detail={detail}
            timelineEvents={timelineEvents}
            timelineLoading={timelineLoading}
            timelineError={timelineError}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function DetailBody({
  detail,
  timelineEvents,
  timelineLoading,
  timelineError,
}: {
  detail: ConnectionDetail;
  timelineEvents: ForensicEvent[];
  timelineLoading: boolean;
  timelineError: string | null;
}) {
  const c = detail.connection;
  const events = timelineEvents.length > 0 ? timelineEvents : detail.events ?? [];
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
        {timelineLoading && events.length === 0 ? (
          <Loader label="Loading timeline..." />
        ) : (
          <EventTimeline events={events} />
        )}
        {timelineError && events.length === 0 && (
          <p className="mt-2 text-xs text-state-warning">{timelineError}</p>
        )}
      </div>
    </>
  );
}

function timelineItemToForensicEvent(item: InvestigationTimelineItem): ForensicEvent {
  const details = item.details ?? {};
  return {
    ts: item.ts,
    source: timelineSource(item),
    event_type: item.event_type,
    pid: item.pid,
    process_name: item.process_name,
    user_name: item.user_name,
    path: detailString(details, 'path'),
    op: detailString(details, 'op') ?? detailString(details, 'event_phase'),
    bytes: item.bytes_out ?? item.bytes_in ?? detailNumber(details, 'bytes_out') ?? detailNumber(details, 'bytes_in'),
    query_text: detailString(details, 'query_text'),
    rows_affected: detailNumber(details, 'rows_affected'),
    exec_time_ms: detailNumber(details, 'exec_time_ms'),
    message: item.message,
    severity: item.severity,
  };
}

function timelineSource(item: InvestigationTimelineItem): ForensicEvent['source'] {
  switch (item.source_table) {
    case 'file_accesses':
      return 'file';
    case 'db_queries':
      return 'db';
    case 'logs':
    case 'telemetry_logs':
      return 'log';
    case 'alerts':
      return 'alert';
    case 'processes':
      return 'process';
    default:
      return 'event';
  }
}

function detailString(details: Record<string, unknown>, key: string): string | undefined {
  const value = details[key];
  return typeof value === 'string' && value.trim() ? value : undefined;
}

function detailNumber(details: Record<string, unknown>, key: string): number | undefined {
  const value = details[key];
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return undefined;
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
