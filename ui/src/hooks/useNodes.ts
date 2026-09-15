import { useEffect, useMemo, useState } from 'react';
import { ListNodesParams, PaginatedResponse, NodeSummary } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

export interface UseNodesParams extends ListNodesParams {
  pollIntervalMs?: number;
}

interface NodeState extends PaginatedResponse<NodeSummary> {
  loading: boolean;
  error: string | null;
}

interface UseNodesResult extends NodeState {
  reload: () => void;
}

export function useNodes(params: UseNodesParams = {}): UseNodesResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load nodes');
  const [state, setState] = useState<NodeState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const { pollIntervalMs = 10_000, ...queryParams } = params;

  const normalizedParams = useMemo(
    () => ({
      tenantId: queryParams.tenantId,
      hostnamePrefix: queryParams.hostnamePrefix,
      limit: queryParams.limit,
      offset: queryParams.offset,
    }),
    [queryParams.tenantId, queryParams.hostnamePrefix, queryParams.limit, queryParams.offset],
  );

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | undefined;

    const fetchNodes = async () => {
      try {
        setState((prev) => ({ ...prev, loading: true, error: null }));
        const response = await api.listNodes(normalizedParams);
        if (!cancelled) {
          setState({ ...response, loading: false, error: null });
        }
      } catch (error) {
        if (!cancelled) {
          setState({
            data: [],
            pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
            loading: false,
            error: handleError(error as Error),
          });
        }
      }
    };

    fetchNodes();

    if (pollIntervalMs && pollIntervalMs > 0) {
      timer = setInterval(fetchNodes, pollIntervalMs);
    }

    return () => {
      cancelled = true;
      if (timer) {
        clearInterval(timer);
      }
    };
  }, [api, normalizedParams, pollIntervalMs, reloadToken, handleError]);

  return {
    ...state,
    reload: () => setReloadToken((token) => token + 1),
  };
}
