import { useEffect, useMemo, useState } from 'react';
import { APIError, type CoverageDomain, type CoverageMatrixResponse } from '../lib/api';
import { useApiClient } from './useApiClient';

interface CoverageMatrixState {
  data: CoverageMatrixResponse | null;
  loading: boolean;
  error: string | null;
  unavailable: boolean;
}

export interface UseCoverageMatrixParams {
  tenantId?: string | null;
  domain?: CoverageDomain;
  enabled?: boolean;
}

export interface UseCoverageMatrixResult extends CoverageMatrixState {
  reload: () => void;
}

export function useCoverageMatrix({
  tenantId,
  domain,
  enabled = true,
}: UseCoverageMatrixParams = {}): UseCoverageMatrixResult {
  const api = useApiClient();
  const [state, setState] = useState<CoverageMatrixState>({
    data: null,
    loading: enabled,
    error: null,
    unavailable: false,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const normalized = useMemo(
    () => ({
      tenant_id: tenantId ?? undefined,
      domain,
    }),
    [tenantId, domain],
  );

  useEffect(() => {
    if (!enabled) {
      setState({ data: null, loading: false, error: null, unavailable: false });
      return;
    }

    let cancelled = false;
    setState((current) => ({ ...current, loading: true, error: null }));

    api
      .getCoverageMatrix(normalized)
      .then((data) => {
        if (cancelled) return;
        setState({ data, loading: false, error: null, unavailable: false });
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        if (error instanceof APIError && (error.status === 404 || error.status === 501)) {
          setState({ data: null, loading: false, error: null, unavailable: true });
          return;
        }
        setState({
          data: null,
          loading: false,
          error: error instanceof Error ? error.message : 'Coverage matrix unavailable',
          unavailable: false,
        });
      });

    return () => {
      cancelled = true;
    };
  }, [api, enabled, normalized, reloadToken]);

  return {
    ...state,
    reload: () => setReloadToken((token) => token + 1),
  };
}
