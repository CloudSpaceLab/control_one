import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useAuth } from '../providers/AuthProvider';
export function Login() {
    const { signIn } = useAuth();
    const [token, setToken] = useState('');
    const [error, setError] = useState(null);
    const handleSubmit = (event) => {
        event.preventDefault();
        try {
            const trimmed = token.trim();
            if (!trimmed) {
                throw new Error('Token is required');
            }
            signIn(trimmed);
            setError(null);
        }
        catch (err) {
            if (err instanceof Error) {
                setError(err.message);
            }
            else {
                setError('Unable to sign in');
            }
        }
    };
    return (_jsxs("section", { className: "login-card", children: [_jsx("h2", { children: "Sign in" }), _jsx("p", { children: "Authenticate with your organization-issued OIDC token to access the control plane." }), _jsxs("form", { onSubmit: handleSubmit, children: [_jsx("label", { htmlFor: "token", children: "Bearer token" }), _jsx("input", { id: "token", name: "token", type: "text", placeholder: "Paste JWT here", value: token, onChange: (event) => setToken(event.target.value), required: true }), error ? _jsx("span", { className: "form-error", children: error }) : null, _jsx("button", { type: "submit", children: "Continue" })] })] }));
}
