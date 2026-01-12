import { useEffect, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
export function useRoles() {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load roles');
    const [state, setState] = useState({
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
            .catch((error) => {
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
