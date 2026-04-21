import { useEffect, useMemo, useState } from 'react';
import { Cluster, ListClustersParams, PaginatedResponse } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface ClustersState extends PaginatedResponse<Cluster> {
  loading: boolean;
  error: string | null;
}

interface UseClustersResult extends ClustersState {
  reload: () => void;
}

export function useClusters(params: ListClustersParams = {}): UseClustersResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load clusters');
  const [state, setState] = useState<ClustersState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalized = useMemo(
    () => ({ tenantId: params.tenantId, limit: params.limit, offset: params.offset }),
    [params.tenantId, params.limit, params.offset],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));
    api
      .listClusters(normalized)
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
  }, [api, normalized, reloadToken, handleError]);

  return {
    ...state,
    reload: () => setReloadToken((token) => token + 1),
  };
}
