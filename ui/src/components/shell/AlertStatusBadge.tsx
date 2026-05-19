import { useCallback, useEffect, useState } from 'react';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useApiClient } from '@/hooks/useApiClient';
import { useEventStream } from '@/hooks/useEventStream';
import { useTenant } from '@/providers/TenantProvider';
import { cn } from '@/lib/utils';

const REFRESH_MS = 60_000;

export function AlertStatusBadge() {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [count, setCount] = useState(0);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setCount(0);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const resp = await client.listAlerts({
        tenantId: currentTenantId,
        state: 'open',
        limit: 1,
        offset: 0,
      });
      setCount(resp.pagination.total ?? resp.data.length);
    } catch {
      setCount(0);
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [refresh]);

  useEventStream(currentTenantId ?? undefined, ['alert.opened'], () => {
    void refresh();
  });

  if (loading && count === 0) {
    return (
      <span
        aria-hidden
        className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-state-warning"
      />
    );
  }

  if (count <= 0) return null;

  const label = count > 99 ? '99+' : String(count);
  const tooltip = `${count} open alert${count === 1 ? '' : 's'}`;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            'inline-flex h-5 min-w-5 items-center justify-center rounded-full border px-1.5',
            'border-state-critical/30 bg-state-critical/15 font-mono text-[0.65rem] font-semibold leading-none text-state-critical',
          )}
        >
          {label}
        </span>
      </TooltipTrigger>
      <TooltipContent side="right" className="font-display text-xs">
        {tooltip}
      </TooltipContent>
    </Tooltip>
  );
}
