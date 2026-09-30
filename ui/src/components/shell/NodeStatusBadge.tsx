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
  const { data, nodes, loading } = useFleetSummary({ tenantId: currentTenantId ?? undefined });

  if (loading) {
    return (
      <span
        aria-hidden
        className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-state-unknown"
      />
    );
  }
  // The fleet endpoint can fall back to postgres when Doris is degraded;
  // when neither is wired the response can omit `totals` entirely. Default
  // to all-zeros so the badge renders 0/0 instead of crashing the shell.
  const totals = data?.totals ?? {
    nodes: 0,
    healthy: 0,
    warning: 0,
    degraded: 0,
    critical: 0,
    unknown: 0,
  };
  // The node list is the source of truth for agent connectivity. Fleet health
  // can remain healthy after an agent stops, so do not use it to report online nodes.
  const hasCompleteNodeList = Array.isArray(nodes);
  const nodeList = nodes ?? [];
  const online = hasCompleteNodeList
    ? nodeList.filter((node) => {
        if (!node.last_seen_at) return false;
        const lastSeen = new Date(node.last_seen_at).getTime();
        return Number.isFinite(lastSeen) && Date.now() - lastSeen < 5 * 60 * 1000;
      }).length
    : totals.healthy + totals.warning;
  const total = hasCompleteNodeList ? nodeList.length : totals.nodes;
  const offline = Math.max(0, total - online);
  const tone = offline > 0 ? 'warning' : pickTone(totals);
  const tooltip = `${online} online · ${offline} offline · ${totals.warning} warning · ${totals.degraded} degraded · ${totals.critical} critical`;

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
            <span className="text-text-muted/60"> / {total}</span>
          </span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="right" className="font-display text-xs">
        {tooltip}
      </TooltipContent>
    </Tooltip>
  );
}
