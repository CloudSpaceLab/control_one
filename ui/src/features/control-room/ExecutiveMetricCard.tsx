import type { ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { StateTone } from '@/components/kit';

interface ExecutiveMetricCardProps {
  label: string;
  value: ReactNode;
  hint: ReactNode;
  tone?: StateTone;
  icon?: ReactNode;
  loading?: boolean;
  disabled?: boolean;
  onClick: () => void;
}

const TONE_DOT: Record<StateTone, string> = {
  healthy: 'bg-state-healthy',
  warning: 'bg-state-warning',
  degraded: 'bg-state-warning',
  critical: 'bg-state-critical',
  info: 'bg-state-info',
  unknown: 'bg-text-muted',
};

export function ExecutiveMetricCard({
  label,
  value,
  hint,
  tone = 'unknown',
  icon,
  loading,
  disabled,
  onClick,
}: ExecutiveMetricCardProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'group min-w-0 rounded-lg border border-border-subtle bg-elevated p-4 text-left shadow-[var(--shadow-panel)]',
        'transition hover:border-border-strong hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/40',
        disabled && 'cursor-default opacity-70 hover:border-border-subtle hover:bg-elevated',
      )}
      aria-label={`${label}: view details`}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className={cn('h-1.5 w-1.5 shrink-0 rounded-full', TONE_DOT[tone])} aria-hidden />
          <span className="truncate text-xs font-semibold uppercase tracking-wide text-text-muted">{label}</span>
        </div>
        <div className="flex shrink-0 items-center gap-2 text-text-muted">
          {icon && <span className="[&_svg]:h-4 [&_svg]:w-4">{icon}</span>}
          <ChevronRight className="h-4 w-4 transition-transform group-hover:translate-x-0.5" />
        </div>
      </div>
      <div className="mt-4 font-mono text-2xl font-semibold tabular-nums text-foreground sm:text-3xl">
        {loading ? '—' : value}
      </div>
      <div className="mt-2 min-h-8 text-xs leading-5 text-text-muted">{hint}</div>
    </button>
  );
}
