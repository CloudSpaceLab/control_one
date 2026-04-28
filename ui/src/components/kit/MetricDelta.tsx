import { ArrowDown, ArrowRight, ArrowUp } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface MetricDeltaProps {
  value: number;
  format?: 'percent' | 'absolute';
  invertColor?: boolean;
  precision?: number;
  className?: string;
}

export function MetricDelta({ value, format = 'percent', invertColor = false, precision = 1, className }: MetricDeltaProps) {
  const direction = value === 0 ? 'flat' : value > 0 ? 'up' : 'down';
  const isGood = direction === 'flat' ? null : invertColor ? direction === 'down' : direction === 'up';
  const tone =
    isGood === null ? 'text-text-muted' : isGood ? 'text-state-healthy' : 'text-state-critical';

  const Icon = direction === 'up' ? ArrowUp : direction === 'down' ? ArrowDown : ArrowRight;
  const formatted = format === 'percent'
    ? `${Math.abs(value).toFixed(precision)}%`
    : Math.abs(value).toLocaleString();

  return (
    <span className={cn('inline-flex items-center gap-1 font-mono text-xs font-medium', tone, className)}>
      <Icon className="h-3 w-3" aria-hidden />
      <span>{formatted}</span>
    </span>
  );
}
