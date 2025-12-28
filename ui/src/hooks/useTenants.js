import { useEffect, useMemo, useState } from 'react';
import { useApiClient } from './useApiClient';
export function useTenants(params = {}) {
    const api = useApiClient();
    const [state, setState] = useState({
        data: [],
        pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
        loading: true,
        error: null,
    });
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
