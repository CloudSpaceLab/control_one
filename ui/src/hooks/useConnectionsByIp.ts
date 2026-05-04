import { useQuery } from '@tanstack/react-query';
import { useApiClient } from './useApiClient';
import type { ConnectionDetail, ConnectionRow } from '../lib/api';

export interface UseConnectionsByIpParams {
  tenantId?: string;
  ip: string;
  since?: string;
  until?: string;
  limit?: number;
}

export function useConnectionsByIp({
  tenantId,
  ip,
  since,
  until,
  limit = 250,
}: UseConnectionsByIpParams) {
  const api = useApiClient();
  return useQuery<ConnectionRow[]>({
    queryKey: ['connections.ip', tenantId, ip, since, until, limit],
    queryFn: () => api.listConnections({ tenantId, ip, since, until, limit }),
    enabled: !!ip,
  });
}

export function useConnectionDetail(connId: string | null | undefined) {
  const api = useApiClient();
  return useQuery<ConnectionDetail>({
    queryKey: ['connection.detail', connId],
    queryFn: () => api.getConnectionDetail(connId ?? ''),
    enabled: !!connId,
  });
}
