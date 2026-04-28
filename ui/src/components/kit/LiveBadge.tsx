import { cn } from '@/lib/utils';

export type LiveState = 'live' | 'reconnecting' | 'offline';

export interface LiveBadgeProps {
  state?: LiveState;
  label?: string;
  className?: string;
}

const TONE: Record<LiveState, { dot: string; ring: string; copy: string }> = {
  live: {
    dot: 'bg-state-healthy',
    ring: 'border-state-healthy/30 bg-state-healthy/10 text-state-healthy',
    copy: 'LIVE',
  },
  reconnecting: {
    dot: 'bg-state-warning',
    ring: 'border-state-warning/30 bg-state-warning/10 text-state-warning',
    copy: 'RECONNECTING',
  },
  offline: {
    dot: 'bg-state-critical',
    ring: 'border-state-critical/30 bg-state-critical/10 text-state-critical',
    copy: 'OFFLINE',
  },
};

export function LiveBadge({ state = 'live', label, className }: LiveBadgeProps) {
  const tone = TONE[state];
  return (
    <span
      role="status"
      aria-live="polite"
      className={cn(
        'inline-flex items-center gap-1.5 rounded-sm border px-2 py-0.5 font-mono text-[0.65rem] uppercase tracking-wider',
        tone.ring,
        className,
      )}
    >
      <span
        className={cn('h-1.5 w-1.5 rounded-full', tone.dot, state === 'live' && 'co-anim-dot-pulse')}
        aria-hidden
      />
      {label ?? tone.copy}
    </span>
  );
}
