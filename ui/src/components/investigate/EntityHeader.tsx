import { Ban, Check, Copy, Tag } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Eyebrow, StatusTag, type StateTone } from '@/components/kit';
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card';
import type { ClassificationChip, EntityDetail } from '@/lib/api';
import { ENTITY_TYPE_LABELS } from '@/lib/entity';
import { type EntityType } from '@/components/kit';

const SEV_TO_TONE: Record<string, StateTone> = {
  critical: 'critical',
  high: 'degraded',
  warning: 'warning',
  info: 'info',
  healthy: 'healthy',
};

export interface EntityHeaderProps {
  type: EntityType;
  id: string;
  detail?: EntityDetail;
  loading?: boolean;
  onAction?: (action: 'block' | 'allow' | 'quarantine') => void;
  onTag?: () => void;
  canMutate?: boolean;
}

export function EntityHeader({ type, id, detail, loading, onAction, onTag, canMutate = false }: EntityHeaderProps) {
  const chips: ClassificationChip[] = detail?.classification ?? [];

  return (
    <header className="flex flex-col gap-4 rounded-lg border border-border-subtle bg-elevated p-5 shadow-[var(--shadow-panel)]">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-2 min-w-0">
          <Eyebrow tone="brand">{ENTITY_TYPE_LABELS[type]}</Eyebrow>
          <h1 className="font-mono text-xl font-semibold text-foreground tabular-nums break-all sm:text-2xl">
            {id}
          </h1>
          <div className="flex flex-wrap items-center gap-1.5">
            {loading && (
              <span className="text-xs text-text-muted">Loading classification…</span>
            )}
            {chips.map((c, i) => (
              <StatusTag key={i} tone={SEV_TO_TONE[c.tone ?? 'info'] ?? 'info'}>{c.label}</StatusTag>
            ))}
            {!loading && chips.length === 0 && (
              <StatusTag tone="unknown">UNCLASSIFIED</StatusTag>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => navigator.clipboard?.writeText(id)}
            aria-label="Copy id"
          >
            <Copy className="h-3.5 w-3.5" /> Copy
          </Button>
          {canMutate && (
            <>
              <Button variant="secondary" size="sm" onClick={onTag}>
                <Tag className="h-3.5 w-3.5" /> Tag
              </Button>
              <HoverCard>
                <HoverCardTrigger asChild>
                  <Button variant="danger" size="sm" onClick={() => onAction?.('block')}>
                    <Ban className="h-3.5 w-3.5" /> Block
                  </Button>
                </HoverCardTrigger>
                <HoverCardContent className="w-56 text-xs">
                  Blocks this entity at the policy layer. Emits an audit row and a remediation event.
                </HoverCardContent>
              </HoverCard>
              <Button variant="ghost" size="sm" onClick={() => onAction?.('allow')}>
                <Check className="h-3.5 w-3.5" /> Allow
              </Button>
            </>
          )}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat label="Events" value={detail?.counts?.events ?? 0} />
        <Stat label="Alerts" value={detail?.counts?.alerts ?? 0} />
        <Stat label="Audit" value={detail?.counts?.audit ?? 0} />
        <Stat label="Sessions" value={detail?.counts?.sessions ?? 0} />
        <Stat label="First seen" value={detail?.first_seen ? new Date(detail.first_seen).toLocaleString() : '—'} mono />
        <Stat label="Last seen" value={detail?.last_seen ? new Date(detail.last_seen).toLocaleString() : '—'} mono />
        <Stat label="Remediations" value={detail?.counts?.remediations ?? 0} />
      </div>
    </header>
  );
}

function Stat({ label, value, mono = false }: { label: string; value: string | number; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-border-subtle bg-surface px-3 py-2">
      <span className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">{label}</span>
      <span className={mono ? 'font-mono text-xs tabular-nums' : 'font-mono text-sm font-semibold tabular-nums'}>
        {value}
      </span>
    </div>
  );
}
