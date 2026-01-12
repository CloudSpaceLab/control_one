import { useEffect, useMemo, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
export function useJobs(options = {}) {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load jobs');
    const [state, setState] = useState({
        data: [],
        pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    const { tenantId, status, type, limit, offset, pollIntervalMs } = options;
    const params = useMemo(() => {
        const normalized = {};
        if (tenantId) {
            normalized.tenantId = tenantId;
        }
        if (status) {
            normalized.status = status;
        }
        if (type) {
            normalized.type = type;
        }
        if (typeof limit === 'number') {
            normalized.limit = limit;
        }
        if (typeof offset === 'number') {
            normalized.offset = offset;
        }
        return normalized;
    }, [tenantId, status, type, limit, offset]);
    useEffect(() => {
        let cancelled = false;
        let timer;
        const fetchJobs = async () => {
            try {
                setState((prev) => ({ ...prev, loading: true, error: null }));
                const response = await api.listJobs(params);
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
                        error: handleError(error, 'Unable to fetch jobs'),
                    });
                }
            }
        };
        fetchJobs();
        if (pollIntervalMs && pollIntervalMs > 0) {
            timer = setInterval(fetchJobs, pollIntervalMs);
        }
        return () => {
            cancelled = true;
            if (timer) {
                clearInterval(timer);
            }
        };
    }, [api, params, pollIntervalMs, reloadToken, handleError]);
    return {
        ...state,
        refresh: () => setReloadToken((token) => token + 1),
    };
}
