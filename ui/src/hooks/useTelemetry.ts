import { useEffect, useMemo, useState } from 'react';
import {
  TelemetryMetric,
  TelemetryLog,
  ListTelemetryMetricsParams,
  ListTelemetryLogsParams,
  PaginatedResponse,
} from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface TelemetryMetricsState extends PaginatedResponse<TelemetryMetric> {
  loading: boolean;
  error: string | null;
}

interface UseTelemetryMetricsResult extends TelemetryMetricsState {
  reload: () => void;
}

export function useTelemetryMetrics(params: ListTelemetryMetricsParams = {}): UseTelemetryMetricsResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load telemetry metrics');
  const [state, setState] = useState<TelemetryMetricsState>({
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
      metric_name: params.metric_name,
      since: params.since,
      until: params.until,
      limit: params.limit,
      offset: params.offset,
    }),
    [
      params.tenant_id,
      params.node_id,
      params.metric_name,
      params.since,
      params.until,
      params.limit,
      params.offset,
    ],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .listTelemetryMetrics(normalizedParams)
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

interface TelemetryLogsState extends PaginatedResponse<TelemetryLog> {
  loading: boolean;
  error: string | null;
}

interface UseTelemetryLogsResult extends TelemetryLogsState {
  reload: () => void;
}

export function useTelemetryLogs(params: ListTelemetryLogsParams = {}): UseTelemetryLogsResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load telemetry logs');
  const [state, setState] = useState<TelemetryLogsState>({
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
      log_level: params.log_level,
      log_source: params.log_source,
      since: params.since,
      until: params.until,
      limit: params.limit,
      offset: params.offset,
    }),
    [
      params.tenant_id,
      params.node_id,
      params.log_level,
      params.log_source,
      params.since,
      params.until,
      params.limit,
      params.offset,
    ],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));

    api
      .listTelemetryLogs(normalizedParams)
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

