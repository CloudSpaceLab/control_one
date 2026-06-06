import { useState, useEffect, useCallback, useMemo } from 'react';
import { useApiClient } from './useApiClient';
import { SecretGroup, SecretSync, ListSecretGroupsParams, ListSecretSyncsParams } from '../lib/api';

function emptyPagination(limit = 50, offset = 0) {
  return { total: 0, count: 0, limit, offset };
}

export function useSecretGroups(params: ListSecretGroupsParams = {}) {
  const api = useApiClient();
  const [data, setData] = useState<SecretGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pagination, setPagination] = useState({ total: 0, count: 0, limit: 50, offset: 0 });
  const normalizedParams = useMemo(
    () => ({
      tenant_id: params.tenant_id,
      limit: params.limit,
      offset: params.offset,
    }),
    [params.tenant_id, params.limit, params.offset],
  );

  const reload = useCallback(async () => {
    if (!normalizedParams.tenant_id) {
      setData([]);
      setPagination(emptyPagination(normalizedParams.limit, normalizedParams.offset));
      setError(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const response = await api.listSecretGroups(normalizedParams);
      setData(response.data);
      setPagination(response.pagination);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load secret groups');
    } finally {
      setLoading(false);
    }
  }, [api, normalizedParams]);

  useEffect(() => {
    reload();
  }, [reload]);

  return { data, loading, error, pagination, reload };
}

export function useSecretSyncs(groupId: string | null, params: ListSecretSyncsParams = {}) {
  const api = useApiClient();
  const [data, setData] = useState<SecretSync[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pagination, setPagination] = useState({ total: 0, count: 0, limit: 50, offset: 0 });
  const normalizedParams = useMemo(
    () => ({
      limit: params.limit,
      offset: params.offset,
    }),
    [params.limit, params.offset],
  );

  const reload = useCallback(async () => {
    if (!groupId) {
      setData([]);
      setPagination(emptyPagination(normalizedParams.limit, normalizedParams.offset));
      setError(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const response = await api.listSecretSyncs(groupId, normalizedParams);
      setData(response.data);
      setPagination(response.pagination);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load secret syncs');
    } finally {
      setLoading(false);
    }
  }, [api, groupId, normalizedParams]);

  useEffect(() => {
    reload();
  }, [reload]);

  return { data, loading, error, pagination, reload };
}

