import { useEffect, useMemo, useState } from 'react';
import {
  ListTemplatesParams,
  PaginatedResponse,
  Template,
} from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

export type UseTemplatesOptions = ListTemplatesParams;

interface TemplatesState extends PaginatedResponse<Template> {
  loading: boolean;
  error: string | null;
}

interface UseTemplatesResult extends TemplatesState {
  reload: () => void;
}

export function useTemplates(options: UseTemplatesOptions = {}): UseTemplatesResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load templates');
  const [state, setState] = useState<TemplatesState>({
    data: [],
    pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const params = useMemo(() => {
    const normalized: ListTemplatesParams = {};
    if (options.tenantId) {
      normalized.tenantId = options.tenantId;
    }
    if (options.provider) {
      normalized.provider = options.provider;
    }
    if (options.namePrefix) {
      normalized.namePrefix = options.namePrefix;
    }
    if (typeof options.includeArchived === 'boolean') {
      normalized.includeArchived = options.includeArchived;
    }
    if (typeof options.limit === 'number') {
      normalized.limit = options.limit;
    }
    if (typeof options.offset === 'number') {
      normalized.offset = options.offset;
    }
    return normalized;
  }, [
    options.tenantId,
    options.provider,
    options.namePrefix,
    options.includeArchived,
    options.limit,
    options.offset,
  ]);

  useEffect(() => {
    let cancelled = false;

    const fetchTemplates = async () => {
      if (!params.tenantId) {
        setState((prev) => ({
          ...prev,
          data: [],
          pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
          loading: false,
          error: null,
        }));
        return;
      }
      try {
        setState((prev) => ({ ...prev, loading: true, error: null }));
        const response = await api.listTemplates(params);
        if (!cancelled) {
          setState({ ...response, loading: false, error: null });
        }
      } catch (error) {
        if (!cancelled) {
          setState({
            data: [],
            pagination: { total: 0, count: 0, limit: 0, offset: 0, nextOffset: null, prevOffset: null },
            loading: false,
            error: handleError(error, 'Unable to fetch templates'),
          });
        }
      }
    };

    fetchTemplates();

    return () => {
      cancelled = true;
    };
  }, [api, params, reloadToken, handleError]);

  return {
    ...state,
    reload: () => setReloadToken((token) => token + 1),
  };
}
