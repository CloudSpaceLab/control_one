import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Navigate, Route, Routes } from 'react-router-dom';
import { MainLayout } from './components/MainLayout';
import { Dashboard } from './pages/Dashboard';
import { Jobs } from './pages/Jobs';
import { Templates } from './pages/Templates';
import { Nodes } from './pages/Nodes';
import { Tenants } from './pages/Tenants';
import { Compliance } from './pages/Compliance';
import { Audit } from './pages/Audit';
import { Users } from './pages/Users';
import { Telemetry } from './pages/Telemetry';
import { Settings } from './pages/Settings';
import { Secrets } from './pages/Secrets';
import { Login } from './pages/Login';
import { AuthCallback } from './pages/AuthCallback';
import { Setup } from './pages/Setup';
import { useAuth } from './providers/AuthProvider';
export function App() {
    const { isAuthenticated } = useAuth();
    return (_jsxs(Routes, { children: [_jsx(Route, { path: "/login", element: _jsx(Login, {}) }), _jsx(Route, { path: "/auth/callback", element: _jsx(AuthCallback, {}) }), _jsxs(Route, { path: "/", element: isAuthenticated ? _jsx(MainLayout, {}) : _jsx(Navigate, { to: "/login", replace: true }), children: [_jsx(Route, { index: true, element: _jsx(Dashboard, {}) }), _jsx(Route, { path: "setup", element: _jsx(Setup, {}) }), _jsx(Route, { path: "tenants", element: _jsx(Tenants, {}) }), _jsx(Route, { path: "nodes", element: _jsx(Nodes, {}) }), _jsx(Route, { path: "jobs", element: _jsx(Jobs, {}) }), _jsx(Route, { path: "templates", element: _jsx(Templates, {}) }), _jsx(Route, { path: "compliance", element: _jsx(Compliance, {}) }), _jsx(Route, { path: "audit", element: _jsx(Audit, {}) }), _jsx(Route, { path: "users", element: _jsx(Users, {}) }), _jsx(Route, { path: "telemetry", element: _jsx(Telemetry, {}) }), _jsx(Route, { path: "secrets", element: _jsx(Secrets, {}) }), _jsx(Route, { path: "settings", element: _jsx(Settings, {}) })] }), _jsx(Route, { path: "*", element: _jsx(Navigate, { to: isAuthenticated ? '/' : '/login', replace: true }) })] }));
}
