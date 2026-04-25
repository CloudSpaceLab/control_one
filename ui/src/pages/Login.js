import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { buildAuthorizationUrl } from '../lib/oidc';
import { isOidcConfigured } from '../config/oidc';
import { useAuth } from '../providers/AuthProvider';
export function Login() {
    const { signIn, loading, error, isAuthenticated } = useAuth();
    const [token, setToken] = useState('');
    const [localError, setLocalError] = useState(null);
    const [ssoError, setSsoError] = useState(null);
    const [ssoLoading, setSsoLoading] = useState(false);
    const navigate = useNavigate();
    const location = useLocation();
    const returnTo = useMemo(() => {
        const state = location.state;
        return state?.from;
    }, [location.state]);
    useEffect(() => {
        if (isAuthenticated) {
            navigate('/', { replace: true });
        }
    }, [isAuthenticated, navigate]);
    const handleSubmit = async (event) => {
        event.preventDefault();
        try {
            const trimmed = token.trim();
            if (!trimmed) {
                throw new Error('Token is required');
            }
            setLocalError(null);
            await signIn(trimmed);
        }
        catch (err) {
            setLocalError(err instanceof Error ? err.message : 'Unable to sign in');
        }
    };
    const handleSso = async () => {
        try {
            setSsoError(null);
            setSsoLoading(true);
            const url = await buildAuthorizationUrl(returnTo);
            window.location.assign(url);
        }
        catch (err) {
            setSsoError(err instanceof Error ? err.message : 'Unable to start sign-in');
            setSsoLoading(false);
        }
    };
    if (isAuthenticated) {
        return _jsx(Navigate, { to: "/", replace: true });
    }
    const oidcEnabled = isOidcConfigured();
    return (_jsxs("section", { className: "login-card", children: [_jsx("h2", { children: "Sign in" }), _jsx("p", { children: "Authenticate with your organization-issued identity provider to access the control plane." }), oidcEnabled ? (_jsxs("div", { className: "login-card__section", children: [_jsx("button", { type: "button", className: "primary", onClick: handleSso, disabled: ssoLoading, children: ssoLoading ? 'Redirecting…' : 'Continue with Single Sign-On' }), ssoError ? _jsx("span", { className: "form-error", children: ssoError }) : null] })) : null, _jsxs("div", { className: "login-card__section", children: [_jsx("h3", { children: "Developer token" }), _jsxs("form", { onSubmit: handleSubmit, children: [_jsx("label", { htmlFor: "token", children: "Bearer token" }), _jsx("input", { id: "token", name: "token", type: "text", placeholder: "Paste JWT here", value: token, onChange: (event) => setToken(event.target.value), disabled: loading, required: true }), localError ? _jsx("span", { className: "form-error", children: localError }) : null, error ? _jsx("span", { className: "form-error", children: error }) : null, _jsx("button", { type: "submit", disabled: loading, children: loading ? 'Signing in…' : 'Continue' })] })] })] }));
}
