import { useQueryClient, type QueryKey } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useApiClient } from './useApiClient';
import type { LiveState } from '@/components/kit';
import { liveEventsMode, liveEventsUseSSE } from '@/config/live';

export interface LiveEvent {
  topic: string;
  tenant_id: string;
  node_id?: string;
  payload?: unknown;
  timestamp?: string;
}

export interface SubscriptionConfig {
  topic: string;
  invalidate?: QueryKey[];
  onEvent?: (ev: LiveEvent) => void;
}

const DEBOUNCE_MS = 500;

/**
 * useLiveSubscribe — coalesced live subscription that drives react-query
 * invalidations when the deployment opts into SSE. Each topic registers query
 * keys to invalidate; bursts of events within `DEBOUNCE_MS` cause a single
 * invalidation per query key.
 *
 * Returns the connection state for a `<LiveBadge>`.
 */
export function useLiveSubscribe(
  tenantId: string | undefined,
  subs: SubscriptionConfig[],
): { state: LiveState } {
  const client = useApiClient();
  const queryClient = useQueryClient();
  const subsRef = useRef(subs);
  subsRef.current = subs;
  const [state, setState] = useState<LiveState>('offline');

  const topics = useMemo(() => Array.from(new Set(subs.map((s) => s.topic))), [subs]);
  const topicKey = topics.join(',');

  const pending = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const dispatch = useCallback(
    (ev: LiveEvent) => {
      const matches = subsRef.current.filter((s) => s.topic === ev.topic);
      for (const m of matches) {
        m.onEvent?.(ev);
        if (m.invalidate) {
          for (const key of m.invalidate) {
            const id = JSON.stringify(key);
            const existing = pending.current.get(id);
            if (existing) clearTimeout(existing);
            pending.current.set(
              id,
              setTimeout(() => {
                queryClient.invalidateQueries({ queryKey: key });
                pending.current.delete(id);
              }, DEBOUNCE_MS),
            );
          }
        }
      }
    },
    [queryClient],
  );

  useEffect(() => {
    if (!tenantId || topics.length === 0) {
      setState('offline');
      return undefined;
    }
    if (!liveEventsUseSSE()) {
      setState(liveEventsMode === 'off' ? 'offline' : 'live');
      return undefined;
    }
    if (!client?.streamEvents) {
      setState('offline');
      return undefined;
    }
    setState('reconnecting');
    let cancelled = false;
    let firstEvent = true;
    const cancel = client.streamEvents(
      { tenantId, topics },
      (raw) => {
        if (cancelled) return;
        if (firstEvent) {
          firstEvent = false;
          setState('live');
        }
        dispatch(raw as LiveEvent);
      },
    );
    // Optimistic transition to "live" after a short grace; SSE handshake is fast.
    const handshake = window.setTimeout(() => {
      if (!cancelled) setState('live');
    }, 800);
    return () => {
      cancelled = true;
      window.clearTimeout(handshake);
      cancel();
      setState('offline');
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, tenantId, topicKey]);

  return { state };
}

/**
 * Default topic set used by every dashboard.
 */
export const DEFAULT_LIVE_TOPICS = [
  'security.event',
  'health.incident',
  'rule.triggered',
  'remediation.applied',
  'compliance.fired',
  'alert.opened',
] as const;
