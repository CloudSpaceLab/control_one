import { cn } from '@/lib/utils';
import { TONE_COLOR, type StateTone } from './types';

export interface StatusDotProps {
  tone: StateTone;
  pulse?: boolean;
  size?: 'xs' | 'sm' | 'md';
  label?: string;
  className?: string;
}

const SIZE: Record<NonNullable<StatusDotProps['size']>, string> = {
  xs: 'h-1.5 w-1.5',
  sm: 'h-2 w-2',
  md: 'h-2.5 w-2.5',
};

export function StatusDot({ tone, pulse, size = 'sm', label, className }: StatusDotProps) {
  return (
    <span
      role={label ? 'status' : undefined}
      aria-label={label}
      className={cn('inline-flex items-center gap-2', className)}
    >
      <span
        className={cn(
          'inline-block rounded-full',
          SIZE[size],
          pulse && 'co-anim-dot-pulse',
        )}
        style={{ backgroundColor: TONE_COLOR[tone], color: TONE_COLOR[tone] }}
        aria-hidden
      />
      {label && <span className="text-xs text-text-secondary">{label}</span>}
    </span>
  );
}
