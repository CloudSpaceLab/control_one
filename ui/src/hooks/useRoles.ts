import { useEffect, useState } from 'react';
import { APIError, Role } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface RolesState {
  data: Role[];
  loading: boolean;
  error: string | null;
  errorStatus: number | null;
}

interface UseRolesResult extends RolesState {
  reload: () => void;
}

export function useRoles(): UseRolesResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load roles');
  const [state, setState] = useState<RolesState>({
    data: [],
    loading: true,
    error: null,
    errorStatus: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null, errorStatus: null }));

    api
      .listRoles()
      .then((data) => {
        if (!cancelled) {
          setState({ data, loading: false, error: null, errorStatus: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({
            data: [],
            loading: false,
            error: error instanceof APIError && error.status === 403 ? 'Access denied' : handleError(error),
            errorStatus: error instanceof APIError ? error.status : null,
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [api, reloadToken, handleError]);

  return {
    ...state,
    reload: () => setReloadToken((token) => token + 1),
  };
}

