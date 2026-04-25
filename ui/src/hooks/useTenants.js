import { useEffect, useMemo, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
export function useTenants(params = {}) {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load tenants');
    const [state, setState] = useState({
        data: [],
        pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    const normalizedParams = useMemo(() => ({
        namePrefix: params.namePrefix,
        limit: params.limit,
        offset: params.offset,
    }), [params.namePrefix, params.limit, params.offset]);
    useEffect(() => {
        let cancelled = false;
        setState((prev) => ({ ...prev, loading: true, error: null }));
        api
            .listTenants(normalizedParams)
            .then((response) => {
            if (!cancelled) {
                setState({ ...response, loading: false, error: null });
            }
        })
            .catch((error) => {
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
