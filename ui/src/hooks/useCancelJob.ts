import { useCallback, useState } from 'react';
import { Job } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
import { useToast } from '../providers/ToastProvider';

interface UseCancelJobState {
  cancelling: boolean;
  error: string | null;
}

interface UseCancelJobResult extends UseCancelJobState {
  cancelJob: (jobId: string) => Promise<Job | null>;
}

export function useCancelJob(): UseCancelJobResult {
  const api = useApiClient();
  const { showToast } = useToast();
  const handleError = useApiErrorHandler('Failed to cancel job');
  const [state, setState] = useState<UseCancelJobState>({
    cancelling: false,
    error: null,
  });

  const cancelJob = useCallback(
    async (jobId: string): Promise<Job | null> => {
      setState({ cancelling: true, error: null });
      try {
        const job = await api.cancelJob(jobId);
        showToast(`Job ${job.id} cancelled`, 'success');
        setState({ cancelling: false, error: null });
        return job;
      } catch (error) {
        const message = handleError(error, 'Unable to cancel job');
        setState({ cancelling: false, error: message });
        return null;
      }
    },
    [api, handleError, showToast],
  );

  return {
    ...state,
    cancelJob,
  };
}
