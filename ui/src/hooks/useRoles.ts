import { useEffect, useState } from 'react';
import { Role } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface RolesState {
  data: Role[];
  loading: boolean;
  error: string | null;
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
  });
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .listRoles()
      .then((data) => {
        if (!cancelled) {
          setState({ data, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({
            data: [],
            loading: false,
            error: handleError(error),
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

