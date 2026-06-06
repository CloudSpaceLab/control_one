import { useEffect, useMemo, useState } from 'react';
import { Webhook, ListWebhooksParams, PaginatedResponse } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface WebhooksState extends PaginatedResponse<Webhook> {
  loading: boolean;
  error: string | null;
}

interface UseWebhooksResult extends WebhooksState {
  reload: () => void;
}

export function useWebhooks(params: ListWebhooksParams = {}): UseWebhooksResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load webhooks');
  const [state, setState] = useState<WebhooksState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalizedParams = useMemo(
    () => ({
      tenant_id: params.tenant_id,
      enabled: params.enabled,
      limit: params.limit,
      offset: params.offset,
    }),
    [params.tenant_id, params.enabled, params.limit, params.offset],
  );

  useEffect(() => {
    let cancelled = false;
    if (!normalizedParams.tenant_id) {
      setState({
        data: [],
        pagination: {
          total: 0,
          count: 0,
          limit: normalizedParams.limit ?? 0,
          offset: normalizedParams.offset ?? 0,
          nextOffset: null,
          prevOffset: null,
        },
        loading: false,
        error: null,
      });
      return () => {
        cancelled = true;
      };
    }
    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .listWebhooks(normalizedParams)
      .then((response) => {
        if (!cancelled) {
          setState({ ...response, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({
            data: [],
            pagination: {
              total: 0,
              count: 0,
              limit: normalizedParams.limit ?? 0,
              offset: normalizedParams.offset ?? 0,
              nextOffset: null,
              prevOffset: null,
            },
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

