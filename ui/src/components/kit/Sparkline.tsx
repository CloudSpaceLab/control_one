import { useMemo } from 'react';
import { cn } from '@/lib/utils';
import { TONE_COLOR, type StateTone } from './types';

export interface SparklineProps {
  data: number[];
  tone?: StateTone | 'brand' | 'accent';
  height?: number;
  ariaLabel: string;
  fill?: boolean;
  loading?: boolean;
  /** Shown when data is empty (and not loading). */
  emptyLabel?: string;
  className?: string;
}

const NON_STATE_COLOR: Record<'brand' | 'accent', string> = {
  brand: 'var(--brand-500)',
  accent: 'var(--accent-500)',
};

export function Sparkline({
  data,
  tone = 'brand',
  height = 28,
  ariaLabel,
  fill = true,
  loading,
  emptyLabel = 'No data',
  className,
}: SparklineProps) {
  const { d, fillD } = useMemo(() => {
    if (!data.length) return { d: '', fillD: '' };
    const w = 120;
    const min = Math.min(...data);
    const max = Math.max(...data);
    const range = max - min || 1;
    const step = data.length > 1 ? w / (data.length - 1) : 0;
    const points = data.map((v, i) => {
      const x = i * step;
      const y = height - ((v - min) / range) * height;
      return [x, y] as const;
    });
    const path = points.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ');
    const fillPath = `${path} L${(points[points.length - 1][0]).toFixed(1)} ${height} L0 ${height} Z`;
    return { d: path, fillD: fillPath };
  }, [data, height]);

  if (loading) {
    return (
      <div
        role="img"
        aria-label={`${ariaLabel} (loading)`}
        className={cn(
          'w-full animate-pulse rounded-sm bg-text-muted/15',
          className,
        )}
        style={{ height }}
      />
    );
  }

  if (!data.length) {
    return (
      <div
        role="img"
        aria-label={ariaLabel}
        className={cn(
          'flex w-full items-center justify-center border-b border-dashed border-border-subtle text-[0.65rem] text-text-muted',
          className,
        )}
        style={{ height }}
      >
        {emptyLabel}
      </div>
    );
  }

  const stroke = tone === 'brand' || tone === 'accent' ? NON_STATE_COLOR[tone] : TONE_COLOR[tone];

  return (
    <svg
      role="img"
      aria-label={ariaLabel}
      viewBox={`0 0 120 ${height}`}
      preserveAspectRatio="none"
      className={cn('w-full', className)}
      style={{ height }}
    >
      {fill && fillD && (
        <path d={fillD} fill={stroke} fillOpacity={0.16} />
      )}
      <path d={d} fill="none" stroke={stroke} strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}
