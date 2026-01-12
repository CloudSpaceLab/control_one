import { useState, useEffect } from 'react';
import { useApiClient } from './useApiClient';
export function useSecretGroups(params = {}) {
    const api = useApiClient();
    const [data, setData] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [pagination, setPagination] = useState({ total: 0, count: 0, limit: 50, offset: 0 });
    const reload = async () => {
        setLoading(true);
        setError(null);
        try {
            const response = await api.listSecretGroups(params);
            setData(response.data);
            setPagination(response.pagination);
        }
        catch (err) {
            setError(err?.message || 'Failed to load secret groups');
        }
        finally {
            setLoading(false);
        }
    };
    useEffect(() => {
        reload();
    }, [params.tenant_id, params.limit, params.offset]);
    return { data, loading, error, pagination, reload };
}
export function useSecretSyncs(groupId, params = {}) {
    const api = useApiClient();
    const [data, setData] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const [pagination, setPagination] = useState({ total: 0, count: 0, limit: 50, offset: 0 });
    const reload = async () => {
        if (!groupId) {
            setData([]);
            setLoading(false);
            return;
        }
        setLoading(true);
        setError(null);
        try {
            const response = await api.listSecretSyncs(groupId, params);
            setData(response.data);
            setPagination(response.pagination);
        }
        catch (err) {
            setError(err?.message || 'Failed to load secret syncs');
        }
        finally {
            setLoading(false);
        }
    };
    useEffect(() => {
        reload();
    }, [groupId, params.limit, params.offset]);
    return { data, loading, error, pagination, reload };
}
