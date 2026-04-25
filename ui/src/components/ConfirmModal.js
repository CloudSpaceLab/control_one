import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef } from 'react';
// ConfirmModal replaces window.confirm() throughout the UI. It manages focus
// trapping and ESC-to-close on its own so callers can drop it in-place and
// forget about it.
export function ConfirmModal({ open, title, body, confirmLabel = 'Confirm', cancelLabel = 'Cancel', variant = 'default', onConfirm, onCancel, }) {
    const confirmRef = useRef(null);
    useEffect(() => {
        if (!open)
            return undefined;
        confirmRef.current?.focus();
        const onKey = (e) => {
            if (e.key === 'Escape')
                onCancel();
            if (e.key === 'Enter' && e.target instanceof HTMLElement && e.target.tagName !== 'BUTTON') {
                onConfirm();
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [open, onCancel, onConfirm]);
    if (!open)
        return null;
    return (_jsx("div", { role: "dialog", "aria-modal": "true", "aria-labelledby": "confirm-modal-title", style: {
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.55)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
        }, onClick: onCancel, children: _jsxs("div", { style: {
                background: 'var(--surface, #1a1a1a)',
                color: 'var(--text, #fff)',
                padding: '1.5rem',
                borderRadius: 8,
                minWidth: 320,
                maxWidth: 480,
                boxShadow: '0 12px 32px rgba(0,0,0,0.4)',
            }, onClick: (e) => e.stopPropagation(), children: [_jsx("h3", { id: "confirm-modal-title", style: { marginTop: 0 }, children: title }), body ? _jsx("p", { children: body }) : null, _jsxs("div", { style: { display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', marginTop: '1rem' }, children: [_jsx("button", { type: "button", className: "secondary-button", onClick: onCancel, children: cancelLabel }), _jsx("button", { ref: confirmRef, type: "button", className: variant === 'danger' ? 'danger-button' : 'primary-button', onClick: onConfirm, children: confirmLabel })] })] }) }));
}
