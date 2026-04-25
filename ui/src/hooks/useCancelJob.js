import { useCallback, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
import { useToast } from '../providers/ToastProvider';
export function useCancelJob() {
    const api = useApiClient();
    const { showToast } = useToast();
    const handleError = useApiErrorHandler('Failed to cancel job');
    const [state, setState] = useState({
        cancelling: false,
        error: null,
    });
    const cancelJob = useCallback(async (jobId) => {
        setState({ cancelling: true, error: null });
        try {
            const job = await api.cancelJob(jobId);
            showToast(`Job ${job.id} cancelled`, 'success');
            setState({ cancelling: false, error: null });
            return job;
        }
        catch (error) {
            const message = handleError(error, 'Unable to cancel job');
            setState({ cancelling: false, error: message });
            return null;
        }
    }, [api, handleError, showToast]);
    return {
        ...state,
        cancelJob,
    };
}
