import { APIClient } from '../lib/api';
import { useAuth } from '../providers/AuthProvider';

export function useApiClient(): APIClient {
  const { apiClient } = useAuth();
  return apiClient;
}
