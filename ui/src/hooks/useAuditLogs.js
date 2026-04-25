import { useEffect, useMemo, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
export function useAuditLogs(params = {}) {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load audit logs');
    const [state, setState] = useState({
        data: [],
        pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    const normalizedParams = useMemo(() => ({
        tenant_id: params.tenant_id,
        actor_type: params.actor_type,
        action: params.action,
        resource_type: params.resource_type,
        resource_id: params.resource_id,
        since: params.since,
        until: params.until,
        limit: params.limit,
        offset: params.offset,
    }), [
        params.tenant_id,
        params.actor_type,
        params.action,
        params.resource_type,
        params.resource_id,
        params.since,
        params.until,
        params.limit,
        params.offset,
    ]);
    useEffect(() => {
        let cancelled = false;
        setState((prev) => ({ ...prev, loading: true, error: null }));
        api
            .listAuditLogs(normalizedParams)
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
