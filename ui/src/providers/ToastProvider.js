import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { createContext, useCallback, useContext, useMemo, useState } from 'react';
const ToastContext = createContext(undefined);
export function ToastProvider({ children }) {
    const [toasts, setToasts] = useState([]);
    const dismissToast = useCallback((id) => {
        setToasts((prev) => prev.filter((toast) => toast.id !== id));
    }, []);
    const showToast = useCallback((message, variant = 'info') => {
        const id = crypto.randomUUID();
        setToasts((prev) => [...prev, { id, message, variant }]);
        setTimeout(() => dismissToast(id), 5000);
    }, [dismissToast]);
    const value = useMemo(() => ({
        toasts,
        showToast,
        dismissToast,
    }), [toasts, showToast, dismissToast]);
    return (_jsxs(ToastContext.Provider, { value: value, children: [children, _jsx("div", { className: "toast-stack", children: toasts.map((toast) => (_jsxs("div", { className: `toast toast-${toast.variant}`, children: [_jsx("span", { children: toast.message }), _jsx("button", { type: "button", onClick: () => dismissToast(toast.id), children: "\u00D7" })] }, toast.id))) })] }));
}
export function useToast() {
    const context = useContext(ToastContext);
    if (!context) {
        throw new Error('useToast must be used within a ToastProvider');
    }
    return context;
}
