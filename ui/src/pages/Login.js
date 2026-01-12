import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { buildAuthorizationUrl } from '../lib/oidc';
import { isOidcConfigured } from '../config/oidc';
import { useAuth } from '../providers/AuthProvider';
import { useTenants } from '../hooks/useTenants';
const DEFAULT_ADMIN_CREDENTIALS = {
    username: 'admin',
    password: 'admin123'
};
export function Login() {
    const { signIn, loading, error, isAuthenticated } = useAuth();
    const { pagination: tenantPagination, loading: tenantsLoading } = useTenants({ limit: 1, offset: 0 });
    const [token, setToken] = useState('demo-admin-token');
    const [credentials, setCredentials] = useState({ username: '', password: '' });
    const [localError, setLocalError] = useState(null);
    const [ssoError, setSsoError] = useState(null);
    const [ssoLoading, setSsoLoading] = useState(false);
    const [loginMethod, setLoginMethod] = useState('credentials');
    const navigate = useNavigate();
    const location = useLocation();
    const returnTo = useMemo(() => {
        const state = location.state;
        return state?.from;
    }, [location.state]);
    useEffect(() => {
        if (isAuthenticated && !tenantsLoading) {
            // If no tenants exist, redirect to setup wizard
            if (tenantPagination.total === 0) {
                navigate('/setup', { replace: true });
            }
            else {
                navigate(returnTo || '/', { replace: true });
            }
        }
    }, [isAuthenticated, navigate, returnTo, tenantPagination.total, tenantsLoading]);
    const handleTokenSubmit = async (event) => {
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
    const handleCredentialsSubmit = async (event) => {
        event.preventDefault();
        try {
            if (!credentials.username.trim() || !credentials.password.trim()) {
                throw new Error('Username and password are required');
            }
            // Check against default admin credentials
            if (credentials.username === DEFAULT_ADMIN_CREDENTIALS.username &&
                credentials.password === DEFAULT_ADMIN_CREDENTIALS.password) {
                setLocalError(null);
                // Sign in with a mock admin token
                await signIn('demo-admin-token');
            }
            else {
                throw new Error('Invalid credentials. Use admin/admin123 for default access.');
            }
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
    return (_jsx("div", { className: "auth-container", children: _jsxs("div", { className: "auth-card", children: [_jsx("div", { className: "auth-header", children: _jsxs("div", { className: "auth-brand", children: [_jsx("div", { className: "auth-logo", children: _jsx("span", { children: "C1" }) }), _jsxs("div", { children: [_jsx("h1", { children: "Control One" }), _jsx("p", { children: "Enterprise Control Plane" })] })] }) }), _jsxs("div", { className: "auth-content", children: [_jsxs("div", { className: "auth-welcome", children: [_jsx("h2", { children: "Welcome back" }), _jsx("p", { children: "Sign in to access your control plane dashboard" })] }), _jsx("div", { className: "auth-methods", children: _jsxs("div", { className: "method-group", children: [_jsx("label", { className: "method-label", children: "Authentication Method" }), _jsxs("div", { className: "radio-group", children: [_jsxs("label", { className: "radio-item", children: [_jsx("input", { type: "radio", name: "loginMethod", value: "credentials", checked: loginMethod === 'credentials', onChange: () => setLoginMethod('credentials') }), _jsxs("span", { className: "radio-text", children: [_jsx("strong", { children: "Username & Password" }), _jsx("small", { children: "Use your admin credentials" })] })] }), _jsxs("label", { className: "radio-item", children: [_jsx("input", { type: "radio", name: "loginMethod", value: "token", checked: loginMethod === 'token', onChange: () => setLoginMethod('token') }), _jsxs("span", { className: "radio-text", children: [_jsx("strong", { children: "Bearer Token" }), _jsx("small", { children: "Use your API token" })] })] })] })] }) }), oidcEnabled && (_jsx("div", { className: "auth-divider", children: _jsx("span", { children: "OR" }) })), oidcEnabled && (_jsxs("div", { className: "auth-sso", children: [_jsxs("button", { type: "button", className: "sso-button", onClick: handleSso, disabled: ssoLoading, children: [_jsx("div", { className: "sso-icon", children: _jsx("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", children: _jsx("path", { d: "M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3" }) }) }), _jsx("span", { children: ssoLoading ? 'Connecting to SSO…' : 'Continue with SSO' })] }), ssoError ? _jsx("div", { className: "auth-error", children: ssoError }) : null] })), loginMethod === 'credentials' && (_jsxs("div", { className: "auth-form", children: [_jsxs("form", { onSubmit: handleCredentialsSubmit, children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "username", children: "Username" }), _jsx("input", { id: "username", name: "username", type: "text", placeholder: "Enter your username", value: credentials.username, onChange: (event) => setCredentials(prev => ({ ...prev, username: event.target.value })), disabled: loading, required: true, autoComplete: "username" })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "password", children: "Password" }), _jsx("input", { id: "password", name: "password", type: "password", placeholder: "Enter your password", value: credentials.password, onChange: (event) => setCredentials(prev => ({ ...prev, password: event.target.value })), disabled: loading, required: true, autoComplete: "current-password" })] }), (localError || error) && (_jsx("div", { className: "auth-error", children: localError || error })), _jsx("button", { type: "submit", className: "auth-button", disabled: loading, children: loading ? (_jsxs(_Fragment, { children: [_jsx("div", { className: "spinner" }), "Signing in\u2026"] })) : ('Sign In') })] }), _jsx("div", { className: "auth-hint", children: _jsx("small", { children: "Default credentials: admin / admin123" }) })] })), loginMethod === 'token' && (_jsx("div", { className: "auth-form", children: _jsxs("form", { onSubmit: handleTokenSubmit, children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "token", children: "Bearer Token" }), _jsx("textarea", { id: "token", name: "token", placeholder: "Paste your JWT token here", value: token, onChange: (event) => setToken(event.target.value), disabled: loading, required: true, rows: 4, className: "token-textarea" })] }), (localError || error) && (_jsx("div", { className: "auth-error", children: localError || error })), _jsx("button", { type: "submit", className: "auth-button", disabled: loading, children: loading ? (_jsxs(_Fragment, { children: [_jsx("div", { className: "spinner" }), "Authenticating\u2026"] })) : ('Authenticate') })] }) }))] })] }) }));
}
