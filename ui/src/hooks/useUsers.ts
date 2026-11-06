import { useEffect, useMemo, useState } from 'react';
import { APIError, User, ListUsersParams, PaginatedResponse } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface UsersState extends PaginatedResponse<User> {
  loading: boolean;
  error: string | null;
  errorStatus: number | null;
}

interface UseUsersResult extends UsersState {
  reload: () => void;
}

export function useUsers(params: ListUsersParams = {}): UseUsersResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load users');
  const [state, setState] = useState<UsersState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
    errorStatus: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalizedParams = useMemo(
    () => ({
      limit: params.limit,
      offset: params.offset,
    }),
    [params.limit, params.offset],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null, errorStatus: null }));

    api
      .listUsers(normalizedParams)
      .then((response) => {
        if (!cancelled) {
          setState({ ...response, loading: false, error: null, errorStatus: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({
            data: [],
            pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
            loading: false,
            error: error instanceof APIError && error.status === 403 ? 'Access denied' : handleError(error),
            errorStatus: error instanceof APIError ? error.status : null,
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

