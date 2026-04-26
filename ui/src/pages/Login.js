import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { buildAuthorizationUrl } from '../lib/oidc';
import { isOidcConfigured } from '../config/oidc';
import { useAuth } from '../providers/AuthProvider';
import { APIClient } from '../lib/api';
export function Login() {
    const { signIn, loading, error, isAuthenticated } = useAuth();
    const [email, setEmail] = useState('');
    const [password, setPassword] = useState('');
    const [emailError, setEmailError] = useState(null);
    const [emailLoading, setEmailLoading] = useState(false);
    const [showAdvanced, setShowAdvanced] = useState(false);
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
            navigate(returnTo ?? '/', { replace: true });
        }
    }, [isAuthenticated, navigate, returnTo]);
    const handleEmailSubmit = async (e) => {
        e.preventDefault();
        setEmailError(null);
        if (!email.trim() || !password) {
            setEmailError('Email and password required');
            return;
        }
        try {
            setEmailLoading(true);
            const client = new APIClient();
            const resp = await client.loginWithPassword(email.trim(), password);
            await signIn(resp.token);
        }
        catch (err) {
            setEmailError(err instanceof Error ? err.message : 'Sign in failed');
        }
        finally {
            setEmailLoading(false);
        }
    };
    const handleTokenSubmit = async (event) => {
        event.preventDefault();
        try {
            const trimmed = token.trim();
            if (!trimmed)
                throw new Error('Token is required');
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
        return _jsx(Navigate, { to: returnTo ?? '/', replace: true });
    }
    const oidcEnabled = isOidcConfigured();
    return (_jsxs("div", { className: "login-page", children: [_jsxs("div", { className: "login-left", children: [_jsx("div", { className: "login-left__grid", "aria-hidden": "true" }), _jsxs("div", { className: "login-brand", children: [_jsx("span", { className: "login-brand__mark", "aria-hidden": "true", children: "\u25CE" }), _jsx("span", { className: "login-brand__name", children: "Control One" })] }), _jsxs("div", { className: "login-hero", children: [_jsxs("h1", { children: ["Find risk.", _jsx("br", {}), "Fix it.", _jsx("br", {}), _jsx("em", { children: "Prove it." })] }), _jsx("p", { children: "Unified compliance, threat detection, just-in-time access, and infrastructure provisioning across every node in your fleet." })] }), _jsxs("div", { className: "login-features", children: [_jsxs("div", { className: "login-feature", children: [_jsx("div", { className: "login-feature__icon", children: "\uD83D\uDEE1" }), _jsxs("div", { className: "login-feature__text", children: [_jsx("strong", { children: "Continuous compliance" }), _jsx("span", { children: "SOC 2, ISO 27001, CIS benchmarks \u2014 automated evidence collection." })] })] }), _jsxs("div", { className: "login-feature", children: [_jsx("div", { className: "login-feature__icon", children: "\u26A1" }), _jsxs("div", { className: "login-feature__text", children: [_jsx("strong", { children: "Real-time threat detection" }), _jsx("span", { children: "Log, port, and anomaly rules with sub-second enforcement." })] })] }), _jsxs("div", { className: "login-feature", children: [_jsx("div", { className: "login-feature__icon", children: "\uD83D\uDD11" }), _jsxs("div", { className: "login-feature__text", children: [_jsx("strong", { children: "Zero standing privilege" }), _jsx("span", { children: "JIT access with full session recording and automatic expiry." })] })] })] })] }), _jsxs("div", { className: "login-right", children: [_jsxs("div", { className: "login-mobile-brand", children: [_jsx("span", { className: "login-brand__mark", "aria-hidden": "true", children: "\u25CE" }), _jsx("span", { className: "login-brand__name", children: "Control One" })] }), _jsxs("div", { className: "login-card", children: [_jsxs("div", { children: [_jsx("h2", { children: "Sign in" }), _jsx("p", { children: "Welcome back. Sign in to your operator console." })] }), _jsxs("form", { onSubmit: handleEmailSubmit, children: [_jsxs("div", { className: "login-field", children: [_jsx("label", { htmlFor: "email", children: "Email" }), _jsx("input", { id: "email", type: "email", autoComplete: "email", placeholder: "you@company.com", value: email, onChange: (e) => setEmail(e.target.value), disabled: emailLoading, required: true })] }), _jsxs("div", { className: "login-field", children: [_jsx("label", { htmlFor: "password", children: "Password" }), _jsx("input", { id: "password", type: "password", autoComplete: "current-password", placeholder: "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022", value: password, onChange: (e) => setPassword(e.target.value), disabled: emailLoading, required: true })] }), emailError ? _jsx("span", { className: "form-error", children: emailError }) : null, _jsxs("div", { className: "login-submit", children: [_jsx("button", { type: "submit", className: "primary-button", disabled: emailLoading, children: emailLoading ? 'Signing in…' : 'Sign in' }), _jsx("a", { href: "https://control-one.cloudspacetechs.com/", className: "login-back", children: "\u2190 Back to home" })] })] }), _jsxs("button", { type: "button", className: "login-advanced-toggle", onClick: () => setShowAdvanced((v) => !v), children: [showAdvanced ? '↑ Hide' : '↓ More', " sign-in options"] }), showAdvanced ? (_jsxs(_Fragment, { children: [oidcEnabled ? (_jsxs("div", { className: "login-card__section", children: [_jsx("h3", { children: "Single Sign-On" }), _jsx("button", { type: "button", className: "primary-button", onClick: handleSso, disabled: ssoLoading, children: ssoLoading ? 'Redirecting…' : 'Continue with SSO' }), ssoError ? _jsx("span", { className: "form-error", children: ssoError }) : null] })) : null, _jsxs("div", { className: "login-card__section", children: [_jsx("h3", { children: "Developer bearer token" }), _jsxs("form", { onSubmit: handleTokenSubmit, children: [_jsx("label", { htmlFor: "token", children: "Bearer token" }), _jsx("input", { id: "token", name: "token", type: "text", placeholder: "Paste JWT or static token", value: token, onChange: (event) => setToken(event.target.value), disabled: loading, style: { marginTop: '0.4rem' } }), localError ? _jsx("span", { className: "form-error", children: localError }) : null, error ? _jsx("span", { className: "form-error", children: error }) : null, _jsx("button", { type: "submit", className: "primary-button", disabled: loading, style: { marginTop: '0.75rem', width: '100%', justifyContent: 'center' }, children: loading ? 'Signing in…' : 'Continue' })] })] })] })) : null] })] })] }));
}
