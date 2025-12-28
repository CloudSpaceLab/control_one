import { useEffect, useMemo, useState } from 'react';
import { ListNodesParams, PaginatedResponse, NodeSummary } from '../lib/api';
import { useApiClient } from './useApiClient';

interface NodeState extends PaginatedResponse<NodeSummary> {
  loading: boolean;
  error: string | null;
}

export function useNodes(params: ListNodesParams = {}): NodeState {
  const api = useApiClient();
  const [state, setState] = useState<NodeState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });

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
            error: error.message,
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [api, normalizedParams]);

  return state;
}
