import { useEffect, useMemo, useState } from 'react';
import {
  ComplianceResult,
  ComplianceSummary,
  ComplianceTrend,
  ControlPostureResponse,
  ListComplianceResultsParams,
  ComplianceTrendsParams,
  PaginatedResponse,
} from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface ComplianceResultsState extends PaginatedResponse<ComplianceResult> {
  loading: boolean;
  error: string | null;
}

interface UseComplianceResultsResult extends ComplianceResultsState {
  reload: () => void;
}

export function useComplianceResults(params: ListComplianceResultsParams = {}): UseComplianceResultsResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load compliance results');
  const [state, setState] = useState<ComplianceResultsState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalizedParams = useMemo(
    () => ({
      tenant_id: params.tenant_id,
      node_id: params.node_id,
      job_id: params.job_id,
      scan_id: params.scan_id,
      rule_id: params.rule_id,
      passed: params.passed,
      severity: params.severity,
      framework: params.framework,
      since: params.since,
      until: params.until,
      limit: params.limit,
      offset: params.offset,
    }),
    [
      params.tenant_id,
      params.node_id,
      params.job_id,
      params.scan_id,
      params.rule_id,
      params.passed,
      params.severity,
      params.framework,
      params.since,
      params.until,
      params.limit,
      params.offset,
    ],
  );

  useEffect(() => {
    let cancelled = false;

    if (!normalizedParams.tenant_id) {
      setState({
        data: [],
        pagination: {
          total: 0,
          count: 0,
          limit: normalizedParams.limit ?? 0,
          offset: normalizedParams.offset ?? 0,
          nextOffset: null,
          prevOffset: null,
        },
        loading: false,
        error: null,
      });
      return () => {
        cancelled = true;
      };
    }

    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .listComplianceResults(normalizedParams)
      .then((response) => {
        if (!cancelled) {
          setState({ ...response, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
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

interface ComplianceSummaryState {
  data: ComplianceSummary | null;
  loading: boolean;
  error: string | null;
}

interface UseComplianceSummaryResult extends ComplianceSummaryState {
  reload: () => void;
}

export function useComplianceSummary(params: { tenant_id?: string; node_id?: string } = {}): UseComplianceSummaryResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load compliance summary');
  const [state, setState] = useState<ComplianceSummaryState>({
    data: null,
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalizedParams = useMemo(
    () => ({
      tenant_id: params.tenant_id,
      node_id: params.node_id,
    }),
    [params.tenant_id, params.node_id],
  );

  useEffect(() => {
    let cancelled = false;

    if (!normalizedParams.tenant_id) {
      setState({
        data: null,
        loading: false,
        error: null,
      });
      return () => {
        cancelled = true;
      };
    }

    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .getComplianceSummary(normalizedParams)
      .then((data) => {
        if (!cancelled) {
          setState({ data, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
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

interface ComplianceTrendsState {
  data: ComplianceTrend[];
  loading: boolean;
  error: string | null;
}

interface UseComplianceTrendsResult extends ComplianceTrendsState {
  reload: () => void;
}

export function useComplianceTrends(params: ComplianceTrendsParams = {}): UseComplianceTrendsResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load compliance trends');
  const [state, setState] = useState<ComplianceTrendsState>({
    data: [],
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalizedParams = useMemo(
    () => ({
      tenant_id: params.tenant_id,
      node_id: params.node_id,
      days: params.days ?? 30,
    }),
    [params.tenant_id, params.node_id, params.days],
  );

  useEffect(() => {
    let cancelled = false;

    if (!normalizedParams.tenant_id) {
      setState({
        data: [],
        loading: false,
        error: null,
      });
      return () => {
        cancelled = true;
      };
    }

    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .getComplianceTrends(normalizedParams)
      .then((data) => {
        if (!cancelled) {
          setState({ data, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
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



interface ControlPostureState {
  data: ControlPostureResponse | null;
  loading: boolean;
  error: string | null;
}

export interface UseControlPostureParams {
  framework?: string;
  tenant_id?: string;
  period_start?: string;
  period_end?: string;
}

export interface UseControlPostureResult extends ControlPostureState {
  reload: () => void;
}

// useControlPosture loads per-control coverage roll-ups for a tenant + framework.
// Returns null data until both framework and tenant_id are provided. Empty
// coverage (NO_COVERAGE for every control) is rendered by the consumer rather
// than treated as an error here — it's a normal state for new tenants.
export function useControlPosture(params: UseControlPostureParams): UseControlPostureResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load control posture');
  const [state, setState] = useState<ControlPostureState>({ data: null, loading: false, error: null });
  const [reloadToken, setReloadToken] = useState(0);

  const normalized = useMemo(
    () => ({
      framework: params.framework,
      tenant_id: params.tenant_id,
      period_start: params.period_start,
      period_end: params.period_end,
    }),
    [params.framework, params.tenant_id, params.period_start, params.period_end],
  );

  useEffect(() => {
    if (!normalized.framework || !normalized.tenant_id) {
      setState({ data: null, loading: false, error: null });
      return;
    }
    let cancelled = false;
    setState((s) => ({ ...s, loading: true, error: null }));
    api
      .getControlPosture({
        framework: normalized.framework,
        tenant_id: normalized.tenant_id,
        period_start: normalized.period_start,
        period_end: normalized.period_end,
      })
      .then((response) => {
        if (cancelled) return;
        setState({ data: response, loading: false, error: null });
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setState({ data: null, loading: false, error: handleError(error) });
      });
    return () => {
      cancelled = true;
    };
  }, [api, normalized, reloadToken, handleError]);

  return { ...state, reload: () => setReloadToken((t) => t + 1) };
}
