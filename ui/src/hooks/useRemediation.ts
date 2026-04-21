import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ListRemediationApprovalsParams,
  PaginatedResponse,
  RemediationApproval,
  RemediationFailures,
  RemediationFailuresParams,
  RemediationStats,
  RemediationStatsParams,
  RemediationVerificationStats,
} from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface AsyncState<T> {
  data: T;
  loading: boolean;
  error: string | null;
}

interface Reloadable<T> extends AsyncState<T> {
  reload: () => void;
}

export function useRemediationStats(params: RemediationStatsParams = {}): Reloadable<RemediationStats | null> {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load remediation stats');
  const [state, setState] = useState<AsyncState<RemediationStats | null>>({
    data: null,
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalized = useMemo(
    () => ({
      window: params.window,
      tenant_id: params.tenant_id,
      node_id: params.node_id,
    }),
    [params.window, params.tenant_id, params.node_id],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));
    api
      .getRemediationStats(normalized)
      .then((data) => {
        if (!cancelled) {
          setState({ data, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({ data: null, loading: false, error: handleError(error) });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [api, normalized, reloadToken, handleError]);

  return { ...state, reload: () => setReloadToken((t) => t + 1) };
}

export function useRemediationFailures(params: RemediationFailuresParams = {}): Reloadable<RemediationFailures | null> {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load remediation failures');
  const [state, setState] = useState<AsyncState<RemediationFailures | null>>({
    data: null,
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalized = useMemo(
    () => ({
      window: params.window,
      tenant_id: params.tenant_id,
      node_id: params.node_id,
      rule_id: params.rule_id,
    }),
    [params.window, params.tenant_id, params.node_id, params.rule_id],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));
    api
      .getRemediationFailures(normalized)
      .then((data) => {
        if (!cancelled) {
          setState({ data, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({ data: null, loading: false, error: handleError(error) });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [api, normalized, reloadToken, handleError]);

  return { ...state, reload: () => setReloadToken((t) => t + 1) };
}

export function useRemediationVerificationStats(
  params: RemediationStatsParams = {},
): Reloadable<RemediationVerificationStats | null> {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load verification stats');
  const [state, setState] = useState<AsyncState<RemediationVerificationStats | null>>({
    data: null,
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalized = useMemo(
    () => ({
      window: params.window,
      tenant_id: params.tenant_id,
      node_id: params.node_id,
    }),
    [params.window, params.tenant_id, params.node_id],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));
    api
      .getRemediationVerificationStats(normalized)
      .then((data) => {
        if (!cancelled) {
          setState({ data, loading: false, error: null });
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({ data: null, loading: false, error: handleError(error) });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [api, normalized, reloadToken, handleError]);

  return { ...state, reload: () => setReloadToken((t) => t + 1) };
}

interface ApprovalsState extends PaginatedResponse<RemediationApproval> {
  loading: boolean;
  error: string | null;
}

interface UseApprovalsResult extends ApprovalsState {
  reload: () => void;
  approve: (id: string) => Promise<void>;
  deny: (id: string) => Promise<void>;
  actionState: { inFlightId: string | null; error: string | null };
}

export function useRemediationApprovals(
  params: ListRemediationApprovalsParams = {},
): UseApprovalsResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load approvals');
  const handleActionError = useApiErrorHandler('Approval action failed');

  const [state, setState] = useState<ApprovalsState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);
  const [actionState, setActionState] = useState<{ inFlightId: string | null; error: string | null }>({
    inFlightId: null,
    error: null,
  });

  const normalized = useMemo(
    () => ({
      status: params.status,
      tenant_id: params.tenant_id,
      node_id: params.node_id,
      limit: params.limit,
      offset: params.offset,
    }),
    [params.status, params.tenant_id, params.node_id, params.limit, params.offset],
  );

  useEffect(() => {
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));
    api
      .listRemediationApprovals(normalized)
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
  }, [api, normalized, reloadToken, handleError]);

  const reload = useCallback(() => setReloadToken((t) => t + 1), []);

  const approve = useCallback(
    async (id: string) => {
      setActionState({ inFlightId: id, error: null });
      try {
        await api.approveRemediationApproval(id);
        setActionState({ inFlightId: null, error: null });
        reload();
      } catch (error) {
        setActionState({ inFlightId: null, error: handleActionError(error as Error) });
        throw error;
      }
    },
    [api, reload, handleActionError],
  );

  const deny = useCallback(
    async (id: string) => {
      setActionState({ inFlightId: id, error: null });
      try {
        await api.denyRemediationApproval(id);
        setActionState({ inFlightId: null, error: null });
        reload();
      } catch (error) {
        setActionState({ inFlightId: null, error: handleActionError(error as Error) });
        throw error;
      }
    },
    [api, reload, handleActionError],
  );

  return {
    ...state,
    reload,
    approve,
    deny,
    actionState,
  };
}
