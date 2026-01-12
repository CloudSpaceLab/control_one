import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
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
    return (_jsxs("section", { className: "login-card", children: [_jsx("h2", { children: "Sign in" }), _jsx("p", { children: "Authenticate to access the control plane." }), _jsx("div", { className: `login-card__section`, children: _jsxs("div", { className: `login-method-toggle ${loginMethod === 'token' ? 'token-active' : ''}`, children: [_jsx("button", { type: "button", className: `method-button ${loginMethod === 'credentials' ? 'active' : ''}`, onClick: () => setLoginMethod('credentials'), children: "Username/Password" }), _jsx("button", { type: "button", className: `method-button ${loginMethod === 'token' ? 'active' : ''}`, onClick: () => setLoginMethod('token'), children: "Bearer Token" })] }) }), oidcEnabled && (_jsxs("div", { className: "login-card__section", children: [_jsx("h3", { children: "Single Sign-On" }), _jsx("button", { type: "button", className: "primary", onClick: handleSso, disabled: ssoLoading, children: ssoLoading ? 'Redirecting…' : 'Continue with Single Sign-On' }), ssoError ? _jsx("span", { className: "form-error", children: ssoError }) : null] })), loginMethod === 'credentials' && (_jsxs("div", { className: "login-card__section", children: [_jsx("h3", { children: "Admin Credentials" }), _jsxs("form", { onSubmit: handleCredentialsSubmit, children: [_jsx("label", { htmlFor: "username", children: "Username" }), _jsx("input", { id: "username", name: "username", type: "text", placeholder: "admin", value: credentials.username, onChange: (event) => setCredentials(prev => ({ ...prev, username: event.target.value })), disabled: loading, required: true }), _jsx("label", { htmlFor: "password", children: "Password" }), _jsx("input", { id: "password", name: "password", type: "password", placeholder: "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022", value: credentials.password, onChange: (event) => setCredentials(prev => ({ ...prev, password: event.target.value })), disabled: loading, required: true }), localError ? _jsx("span", { className: "form-error", children: localError }) : null, error ? _jsx("span", { className: "form-error", children: error }) : null, _jsx("button", { type: "submit", className: "primary-button", disabled: loading, children: loading ? 'Signing in…' : 'Continue' })] }), _jsx("small", { className: "login-hint", children: "Default: admin / admin123" })] })), loginMethod === 'token' && (_jsxs("div", { className: "login-card__section", children: [_jsx("h3", { children: "Developer Token" }), _jsxs("form", { onSubmit: handleTokenSubmit, children: [_jsx("label", { htmlFor: "token", children: "Bearer token" }), _jsx("input", { id: "token", name: "token", type: "text", placeholder: "Paste JWT here", value: token, onChange: (event) => setToken(event.target.value), disabled: loading, required: true }), localError ? _jsx("span", { className: "form-error", children: localError }) : null, error ? _jsx("span", { className: "form-error", children: error }) : null, _jsx("button", { type: "submit", className: "primary-button", disabled: loading, children: loading ? 'Signing in…' : 'Continue' })] })] }))] }));
}
