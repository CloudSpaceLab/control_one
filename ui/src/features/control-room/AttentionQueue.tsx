import { Link } from 'react-router-dom';
import { ArrowRight, ShieldCheck } from 'lucide-react';
import type { ControlRoomExecutiveAttention } from '@/lib/api';
import { EmptyState, Panel, StatusTag, severityTone } from '@/components/kit';
import { CONTROL_ROOM_COPY } from './copy';

interface AttentionQueueProps {
  attention: ControlRoomExecutiveAttention;
  available: boolean;
}

export function AttentionQueue({ attention, available }: AttentionQueueProps) {
  return (
    <Panel
      eyebrow={CONTROL_ROOM_COPY.needsAttention.toUpperCase()}
      title={available ? attentionSummary(attention) : CONTROL_ROOM_COPY.dataUnavailable}
      toneAccent={attention.critical > 0 ? 'critical' : attention.total > 0 ? 'warning' : 'healthy'}
    >
      {!available ? (
        <EmptyState title="Needs attention unavailable" description="Retry the dashboard." />
      ) : attention.items.length === 0 ? (
        <EmptyState
          tone="success"
          icon={<ShieldCheck />}
          title="No items need attention"
          description="Nothing requires review or approval."
        />
      ) : (
        <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle">
          {attention.items.map((item) => (
            <Link
              key={`${item.kind}:${item.id}`}
              to={item.drilldown}
              className="grid grid-cols-[auto_1fr_auto] items-center gap-3 px-3 py-3 transition hover:bg-hover"
            >
              <StatusTag tone={kindTone(item.kind)} variant="outline">
                {kindLabel(item.kind)}
              </StatusTag>
              <span className="min-w-0">
                <span className="block truncate text-sm font-medium text-foreground">{item.title}</span>
                <span className="mt-0.5 block truncate text-xs text-text-muted">
                  {item.domain}{item.reason ? ` · ${item.reason}` : ''}
                </span>
              </span>
              <span className="flex items-center gap-2">
                <StatusTag tone={severityTone(item.severity)}>{item.severity}</StatusTag>
                <span className="hidden whitespace-nowrap text-xs text-text-muted sm:inline">{timeAgo(item.created_at)}</span>
                <ArrowRight className="h-4 w-4 text-text-muted" />
              </span>
            </Link>
          ))}
        </div>
      )}
      {available && attention.total > attention.items.length ? (
        <p className="text-xs text-text-muted">
          Showing {attention.items.length} highest-priority items of {attention.total}.
        </p>
      ) : null}
    </Panel>
  );
}

function attentionSummary(attention: ControlRoomExecutiveAttention): string {
  if (attention.total === 0) return 'No action required';
  return `${attention.total} ${attention.total === 1 ? 'item' : 'items'} require action`;
}

function kindTone(kind: string): 'info' | 'warning' | 'critical' {
  if (kind === 'approval') return 'warning';
  if (kind === 'intervention') return 'critical';
  return 'info';
}

function kindLabel(kind: string): string {
  if (kind === 'approval') return CONTROL_ROOM_COPY.approval;
  if (kind === 'intervention') return CONTROL_ROOM_COPY.intervention;
  return CONTROL_ROOM_COPY.review;
}

function timeAgo(value: string): string {
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return '';
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return 'now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}
