import { useCallback } from 'react';
import { APIError } from '../lib/api';
import { useToast } from '../providers/ToastProvider';

export function useApiErrorHandler(defaultMessage = 'Something went wrong') {
  const { showToast } = useToast();

  return useCallback(
    (error: unknown, fallbackMessage?: string): string => {
      const resolvedMessage =
        error instanceof APIError
          ? error.message
          : error instanceof Error
            ? error.message
            : fallbackMessage ?? defaultMessage;

      showToast(resolvedMessage, 'error');
      return resolvedMessage;
    },
    [defaultMessage, showToast],
  );
}
