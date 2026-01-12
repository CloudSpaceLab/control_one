import { useEffect, useMemo, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
export function useComplianceResults(params = {}) {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load compliance results');
    const [state, setState] = useState({
        data: [],
        pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    const normalizedParams = useMemo(() => ({
        tenant_id: params.tenant_id,
        node_id: params.node_id,
        job_id: params.job_id,
        scan_id: params.scan_id,
        rule_id: params.rule_id,
        passed: params.passed,
        severity: params.severity,
        since: params.since,
        until: params.until,
        limit: params.limit,
        offset: params.offset,
    }), [
        params.tenant_id,
        params.node_id,
        params.job_id,
        params.scan_id,
        params.rule_id,
        params.passed,
        params.severity,
        params.since,
        params.until,
        params.limit,
        params.offset,
    ]);
    useEffect(() => {
        let cancelled = false;
        setState((prev) => ({ ...prev, loading: true, error: null }));
        api
            .listComplianceResults(normalizedParams)
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
export function useComplianceSummary(params = {}) {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load compliance summary');
    const [state, setState] = useState({
        data: null,
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    const normalizedParams = useMemo(() => ({
        tenant_id: params.tenant_id,
        node_id: params.node_id,
    }), [params.tenant_id, params.node_id]);
    useEffect(() => {
        let cancelled = false;
        setState((prev) => ({ ...prev, loading: true, error: null }));
        api
            .getComplianceSummary(normalizedParams)
            .then((data) => {
            if (!cancelled) {
                setState({ data, loading: false, error: null });
            }
        })
            .catch((error) => {
            if (!cancelled) {
                setState({
                    data: null,
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
export function useComplianceTrends(params = {}) {
    const api = useApiClient();
    const handleError = useApiErrorHandler('Failed to load compliance trends');
    const [state, setState] = useState({
        data: [],
        loading: true,
        error: null,
    });
    const [reloadToken, setReloadToken] = useState(0);
    const normalizedParams = useMemo(() => ({
        tenant_id: params.tenant_id,
        node_id: params.node_id,
        days: params.days ?? 30,
    }), [params.tenant_id, params.node_id, params.days]);
    useEffect(() => {
        let cancelled = false;
        setState((prev) => ({ ...prev, loading: true, error: null }));
        api
            .getComplianceTrends(normalizedParams)
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
    }, [api, normalizedParams, reloadToken, handleError]);
    return {
        ...state,
        reload: () => setReloadToken((token) => token + 1),
    };
}
