import { useEffect, useMemo, useState } from 'react';
import { Job, ListJobsParams, PaginatedResponse } from '../lib/api';
import { useApiClient } from './useApiClient';

export interface UseJobsOptions extends Omit<ListJobsParams, 'tenantId'> {
  tenantId?: string;
  pollIntervalMs?: number;
}

interface JobsState extends PaginatedResponse<Job> {
  loading: boolean;
  error: string | null;
}

export function useJobs(options: UseJobsOptions = {}): JobsState {
  const api = useApiClient();
  const [state, setState] = useState<JobsState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });

  const { tenantId, status, type, limit, offset, pollIntervalMs } = options;

  const params = useMemo(() => {
    const normalized: ListJobsParams = {};
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
    let timer: ReturnType<typeof setInterval> | undefined;

    const fetchJobs = async () => {
      try {
        setState((prev) => ({ ...prev, loading: true, error: null }));
        const response = await api.listJobs(params);
        if (!cancelled) {
          setState({ ...response, loading: false, error: null });
        }
      } catch (error) {
        if (!cancelled) {
          setState({
            data: [],
            pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
            loading: false,
            error: (error as Error).message,
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
  }, [api, params, pollIntervalMs]);

  return state;
}
