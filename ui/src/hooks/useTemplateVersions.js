import { useEffect, useMemo, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
export function useTemplateVersions(options) {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load template versions');
    const [state, setState] = useState({
        data: [],
        pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    const params = useMemo(() => {
        const normalized = {};
        if (typeof options.limit === 'number') {
            normalized.limit = options.limit;
        }
        if (typeof options.offset === 'number') {
            normalized.offset = options.offset;
        }
        return normalized;
    }, [options.limit, options.offset]);
    useEffect(() => {
        let cancelled = false;
        const fetchVersions = async () => {
            if (!options.templateId) {
                setState((prev) => ({
                    ...prev,
                    data: [],
                    pagination: { ...prev.pagination, total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
                    loading: false,
                    error: null,
                }));
                return;
            }
            try {
                setState((prev) => ({ ...prev, loading: true, error: null }));
                const response = await api.listTemplateVersions(options.templateId, params);
                if (!cancelled) {
                    setState({ ...response, loading: false, error: null });
                }
            }
            catch (error) {
                if (!cancelled) {
                    setState({
                        data: [],
                        pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
                        loading: false,
                        error: handleError(error, 'Unable to fetch template versions'),
                    });
                }
            }
        };
        fetchVersions();
        return () => {
            cancelled = true;
        };
    }, [api, options.templateId, params, reloadToken, handleError]);
    return {
        ...state,
        reload: () => setReloadToken((token) => token + 1),
    };
}
