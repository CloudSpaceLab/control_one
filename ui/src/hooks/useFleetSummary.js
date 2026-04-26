import { useEffect, useState } from 'react';
import { useApiClient } from './useApiClient';
export function useFleetSummary(opts = {}) {
    const api = useApiClient();
    const [data, setData] = useState(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    useEffect(() => {
        let cancelled = false;
        let timer = null;
        const tick = async () => {
            try {
                const snap = await api.fleetHealthSnapshot({
                    tenantId: opts.tenantId,
                    since: opts.since,
                });
                if (!cancelled) {
                    setData(snap);
                    setLoading(false);
                }
            }
            catch (err) {
                if (!cancelled) {
                    setError(err);
                    setLoading(false);
                }
            }
            finally {
                if (!cancelled) {
                    timer = setTimeout(tick, opts.intervalMs ?? 30000);
                }
            }
        };
        tick();
        return () => {
            cancelled = true;
            if (timer)
                clearTimeout(timer);
        };
    }, [api, opts.tenantId, opts.since, opts.intervalMs]);
    return { data, loading, error };
}
