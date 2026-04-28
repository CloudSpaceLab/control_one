import { type ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { Skeleton } from '@/components/ui/skeleton';
import { Eyebrow } from './Eyebrow';
import { MetricDelta } from './MetricDelta';
import { Sparkline } from './Sparkline';
import { type StateTone, TONE_COLOR } from './types';

export interface KpiTileProps {
  label: string;
  value: ReactNode;
  delta?: number;
  deltaFormat?: 'percent' | 'absolute';
  invertDelta?: boolean;
  sparkline?: number[];
  tone?: StateTone | 'brand' | 'accent';
  icon?: ReactNode;
  size?: 'sm' | 'md' | 'lg';
  loading?: boolean;
  hint?: ReactNode;
  className?: string;
}

const SIZE = {
  sm: { value: 'text-xl', pad: 'p-4', spark: 24 },
  md: { value: 'text-2xl sm:text-3xl', pad: 'p-5', spark: 32 },
  lg: { value: 'text-3xl sm:text-4xl', pad: 'p-6', spark: 44 },
} as const;

export function KpiTile({
  label,
  value,
  delta,
  deltaFormat = 'percent',
  invertDelta,
  sparkline,
  tone = 'brand',
  icon,
  size = 'md',
  loading,
  hint,
  className,
}: KpiTileProps) {
  const sz = SIZE[size];
  const accent = tone === 'brand' || tone === 'accent' ? `var(--${tone}-500)` : TONE_COLOR[tone];

  return (
    <div
      className={cn(
        'group relative flex min-w-0 flex-col gap-3 overflow-hidden rounded-lg border border-border-subtle bg-elevated shadow-[var(--shadow-panel)] transition-all',
        'hover:-translate-y-0.5 hover:border-border-strong',
        sz.pad,
        className,
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <Eyebrow>{label}</Eyebrow>
        {icon && <div className="text-text-muted [&_svg]:h-4 [&_svg]:w-4">{icon}</div>}
      </div>
      <div className="flex items-baseline justify-between gap-3 min-w-0">
        {loading ? (
          <Skeleton className="h-8 w-24" />
        ) : (
          <span
            className={cn('font-mono font-semibold tracking-tight text-foreground tabular-nums', sz.value)}
          >
            {value}
          </span>
        )}
        {delta !== undefined && !loading && (
          <MetricDelta value={delta} format={deltaFormat} invertColor={invertDelta} />
        )}
      </div>
      {sparkline && sparkline.length > 1 && !loading && (
        <Sparkline data={sparkline} tone={tone} height={sz.spark} ariaLabel={`${label} trend`} />
      )}
      {hint && <div className="text-xs text-text-muted">{hint}</div>}
      <span
        aria-hidden
        className="absolute inset-x-0 bottom-0 h-0.5 opacity-80 transition-opacity group-hover:opacity-100"
        style={{ backgroundColor: accent }}
      />
    </div>
  );
}
