import { useState, useEffect, useCallback } from 'react';
import { useApiClient } from './useApiClient';
import { SecretGroup, SecretSync, ListSecretGroupsParams, ListSecretSyncsParams } from '../lib/api';

export function useSecretGroups(params: ListSecretGroupsParams = {}) {
  const api = useApiClient();
  const [data, setData] = useState<SecretGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pagination, setPagination] = useState({ total: 0, count: 0, limit: 50, offset: 0 });

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await api.listSecretGroups(params);
      setData(response.data);
      setPagination(response.pagination);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load secret groups');
    } finally {
      setLoading(false);
    }
  }, [api, params]);

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

  const reload = useCallback(async () => {
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
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load secret syncs');
    } finally {
      setLoading(false);
    }
  }, [api, groupId, params]);

  useEffect(() => {
    reload();
  }, [reload]);

  return { data, loading, error, pagination, reload };
}

