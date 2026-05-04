import { useFleetSummary } from '@/hooks/useFleetSummary';
import { useTenant } from '@/providers/TenantProvider';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';

type Tone = 'healthy' | 'warning' | 'critical' | 'unknown';

function pickTone(totals: {
  healthy: number;
  warning: number;
  degraded: number;
  critical: number;
  unknown: number;
}): Tone {
  if (totals.critical > 0) return 'critical';
  if (totals.degraded > 0 || totals.unknown > 0) return 'warning';
  if (totals.warning > 0) return 'warning';
  return 'healthy';
}

const TONE_CLASS: Record<Tone, string> = {
  healthy: 'bg-state-healthy',
  warning: 'bg-state-warning',
  critical: 'bg-state-critical',
  unknown: 'bg-state-unknown',
};

export function NodeStatusBadge() {
  const { currentTenantId } = useTenant();
  const { data, loading } = useFleetSummary({ tenantId: currentTenantId ?? undefined });

  if (loading) {
    return (
      <span
        aria-hidden
        className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-state-unknown"
      />
    );
  }
  if (!data) return null;

  const { totals } = data;
  const online = totals.healthy + totals.warning;
  const tone = pickTone(totals);
  const tooltip = `${totals.healthy} healthy · ${totals.warning} warning · ${totals.degraded} degraded · ${totals.critical} critical · ${totals.unknown} unknown`;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center gap-1.5 font-mono text-[0.65rem] tabular-nums text-text-muted">
          <span
            aria-hidden
            className={cn('h-1.5 w-1.5 rounded-full', TONE_CLASS[tone])}
          />
          <span>
            {online}
            <span className="text-text-muted/60"> / {totals.nodes}</span>
          </span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="right" className="font-display text-xs">
        {tooltip}
      </TooltipContent>
    </Tooltip>
  );
}
