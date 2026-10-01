import { useQuery } from '@tanstack/react-query';
import { useApiClient } from './useApiClient';
import type { ConnectionDetail, ConnectionListResult } from '../lib/api';

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
  return useQuery<ConnectionListResult>({
    queryKey: ['connections.ip', tenantId, ip, since, until, limit],
    queryFn: () => api.listConnectionsDetailed({ tenantId, ip, since, until, limit }),
    enabled: !!tenantId && !!ip,
    retry: false,
  });
}

export function useConnectionDetail(connId: string | null | undefined, tenantId?: string | null) {
  const api = useApiClient();
  return useQuery<ConnectionDetail>({
    queryKey: ['connection.detail', tenantId, connId],
    queryFn: () => api.getConnectionDetail(connId ?? '', { tenantId }),
    enabled: !!connId && !!tenantId,
  });
}
