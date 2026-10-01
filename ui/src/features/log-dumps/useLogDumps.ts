import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '@/hooks/useApiClient';
import type { LogDump } from '@/lib/api';

interface LogDumpState {
  data: LogDump[];
  loading: boolean;
  error: string | null;
}

export function useLogDumps(tenantId: string, nodeId?: string) {
  const client = useApiClient();
  const [state, setState] = useState<LogDumpState>({
    data: [],
    loading: false,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const reload = useCallback(() => setReloadToken((value) => value + 1), []);

  useEffect(() => {
    if (!tenantId || !nodeId) {
      setState({ data: [], loading: false, error: null });
      return;
    }

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const load = async (initial: boolean) => {
      if (initial) {
        setState((current) => ({ ...current, loading: true, error: null }));
      }
      try {
        const response = await client.listLogDumps({
          tenantId,
          nodeId,
          limit: 50,
          offset: 0,
        });
        if (cancelled) return;
        setState({ data: response.data, loading: false, error: null });
        const active = response.data.some(
          (dump) => dump.status === 'requested' || dump.status === 'capturing',
        );
        timer = setTimeout(() => void load(false), active ? 3_000 : 15_000);
      } catch (error) {
        if (cancelled) return;
        setState((current) => ({
          ...current,
          loading: false,
          error: error instanceof Error ? error.message : 'Failed to load raw logs',
        }));
        timer = setTimeout(() => void load(false), 15_000);
      }
    };

    void load(true);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [client, tenantId, nodeId, reloadToken]);

  return { ...state, reload };
}
