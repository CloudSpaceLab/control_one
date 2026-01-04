import { useEffect, useMemo, useState } from 'react';
import { ListNodesParams, PaginatedResponse, NodeSummary } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface NodeState extends PaginatedResponse<NodeSummary> {
  loading: boolean;
  error: string | null;
}

interface UseNodesResult extends NodeState {
  reload: () => void;
}

export function useNodes(params: ListNodesParams = {}): UseNodesResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load nodes');
  const [state, setState] = useState<NodeState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalizedParams = useMemo(
    () => ({
      tenantId: params.tenantId,
      hostnamePrefix: params.hostnamePrefix,
      limit: params.limit,
      offset: params.offset,
    }),
    [params.tenantId, params.hostnamePrefix, params.limit, params.offset],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .listNodes(normalizedParams)
      .then((response) => {
        if (!cancelled) {
          setState({ ...response, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({
            data: [],
            pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
            loading: false,
            error: handleError(error),
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [api, normalizedParams, reloadToken, handleError]);

  return {
    ...state,
    reload: () => setReloadToken((token) => token + 1),
  };
}
