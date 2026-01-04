import { useCallback, useState } from 'react';

interface FormFeedback {
  success: string | null;
  error: string | null;
  showSuccess: (message: string) => void;
  showError: (message: string) => void;
  reset: () => void;
}

export function useFormFeedback(): FormFeedback {
  const [success, setSuccess] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const showSuccess = useCallback((message: string) => {
    setSuccess(message);
    setError(null);
  }, []);

  const showError = useCallback((message: string) => {
    setError(message);
    setSuccess(null);
  }, []);

  const reset = useCallback(() => {
    setSuccess(null);
    setError(null);
  }, []);

  return { success, error, showSuccess, showError, reset };
}
