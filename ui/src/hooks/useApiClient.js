import { useAuth } from '../providers/AuthProvider';
export function useApiClient() {
    const { apiClient } = useAuth();
    return apiClient;
}
