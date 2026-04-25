import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { exchangeCodeForToken } from '../lib/oidc';
import { useAuth } from '../providers/AuthProvider';
export function AuthCallback() {
    const location = useLocation();
    const navigate = useNavigate();
    const { signIn, isAuthenticated } = useAuth();
    const [processing, setProcessing] = useState(true);
    const [error, setError] = useState(null);
    useEffect(() => {
        const params = new URLSearchParams(location.search);
        const code = params.get('code');
        const state = params.get('state');
        if (!code || !state) {
            setProcessing(false);
            setError('Missing authorization parameters');
            return;
        }
        exchangeCodeForToken(code, state)
            .then(async ({ token, returnTo }) => {
            await signIn(token);
            const destination = returnTo && returnTo.startsWith('/') ? returnTo : '/';
            navigate(destination, { replace: true });
        })
            .catch((err) => {
            setError(err.message);
            setProcessing(false);
        });
    }, [location.search, navigate, signIn]);
    if (isAuthenticated) {
        return _jsx(Navigate, { to: "/", replace: true });
    }
    return (_jsxs("section", { className: "login-card", children: [_jsx("h2", { children: "Completing sign-in" }), processing ? (_jsx("p", { children: "Exchanging authorization code. Please wait\u2026" })) : (_jsxs(_Fragment, { children: [_jsx("p", { children: "We were unable to complete your sign-in." }), error ? _jsx("span", { className: "form-error", children: error }) : null, _jsx("button", { type: "button", onClick: () => navigate('/login', { replace: true }), children: "Return to sign-in" })] }))] }));
}
