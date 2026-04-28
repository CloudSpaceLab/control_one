import { cn } from '@/lib/utils';
import { TONE_COLOR, type StateTone } from './types';

export interface PostureSegment {
  tone: StateTone;
  weight: number;
  label?: string;
}

export interface PostureBarProps {
  score?: number;
  segments?: PostureSegment[];
  ariaLabel: string;
  showLabels?: boolean;
  className?: string;
}

export function PostureBar({ score, segments, ariaLabel, showLabels, className }: PostureBarProps) {
  // Score mode: single colored bar 0-100
  if (segments === undefined) {
    const pct = Math.min(100, Math.max(0, score ?? 0));
    const tone: StateTone = pct >= 80 ? 'healthy' : pct >= 60 ? 'warning' : pct >= 40 ? 'degraded' : 'critical';
    return (
      <div
        role="img"
        aria-label={ariaLabel}
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        className={cn('flex flex-col gap-1', className)}
      >
        <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-surface-2">
          <div
            className="h-full rounded-full transition-all"
            style={{ width: `${pct}%`, backgroundColor: TONE_COLOR[tone] }}
          />
        </div>
        {showLabels && (
          <span className="font-mono text-xs tabular-nums" style={{ color: TONE_COLOR[tone] }}>
            {pct.toFixed(0)}%
          </span>
        )}
      </div>
    );
  }

  // Segmented mode
  const total = segments.reduce((acc, s) => acc + s.weight, 0) || 1;
  return (
    <div role="img" aria-label={ariaLabel} className={cn('flex flex-col gap-1', className)}>
      <div className="flex h-1.5 w-full overflow-hidden rounded-full bg-surface-2">
        {segments.map((seg, i) => (
          <span
            key={i}
            style={{ width: `${(seg.weight / total) * 100}%`, backgroundColor: TONE_COLOR[seg.tone] }}
            className="h-full"
            title={seg.label}
          />
        ))}
      </div>
      {showLabels && (
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-text-muted">
          {segments.map((s, i) => (
            <span key={i} className="inline-flex items-center gap-1.5">
              <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: TONE_COLOR[s.tone] }} />
              {s.label ?? s.tone}
              <span className="font-mono tabular-nums">{((s.weight / total) * 100).toFixed(0)}%</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
