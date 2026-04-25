import { useCallback, useState } from 'react';
export function useFormFeedback() {
    const [success, setSuccess] = useState(null);
    const [error, setError] = useState(null);
    const showSuccess = useCallback((message) => {
        setSuccess(message);
        setError(null);
    }, []);
    const showError = useCallback((message) => {
        setError(message);
        setSuccess(null);
    }, []);
    const reset = useCallback(() => {
        setSuccess(null);
        setError(null);
    }, []);
    return { success, error, showSuccess, showError, reset };
}
