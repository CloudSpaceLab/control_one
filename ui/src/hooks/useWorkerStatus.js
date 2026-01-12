import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
export function useWorkerStatus(options = {}) {
    const { pollIntervalMs = 8000 } = options;
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load worker status');
    const [state, setState] = useState({
        status: null,
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    useEffect(() => {
        let cancelled = false;
        let timer;
        const fetchStatus = async () => {
            try {
                setState((prev) => ({ ...prev, loading: true, error: null }));
                const status = await api.getWorkerStatus();
                if (!cancelled) {
                    setState({ status, loading: false, error: null });
                }
            }
            catch (error) {
                if (!cancelled) {
                    setState({
                        status: null,
                        loading: false,
                        error: handleError(error, 'Unable to fetch worker status'),
                    });
                }
            }
        };
        fetchStatus();
        if (pollIntervalMs && pollIntervalMs > 0) {
            timer = setInterval(fetchStatus, pollIntervalMs);
        }
        return () => {
            cancelled = true;
            if (timer) {
                clearInterval(timer);
            }
        };
    }, [api, handleError, pollIntervalMs, reloadToken]);
    const refresh = useCallback(() => {
        setReloadToken((token) => token + 1);
    }, []);
    return {
        ...state,
        refresh,
    };
}
