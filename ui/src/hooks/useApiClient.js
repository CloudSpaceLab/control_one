import { useMemo } from 'react';
import { APIClient } from '../lib/api';
import { useAuth } from '../providers/AuthProvider';
export function useApiClient() {
    const { token } = useAuth();
    return useMemo(() => new APIClient({ token }), [token]);
}
