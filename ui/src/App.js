import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Navigate, Route, Routes } from 'react-router-dom';
import { MainLayout } from './components/MainLayout';
import { Dashboard } from './pages/Dashboard';
import { Jobs } from './pages/Jobs';
import { Nodes } from './pages/Nodes';
import { Tenants } from './pages/Tenants';
import { Login } from './pages/Login';
import { useAuth } from './providers/AuthProvider';
export function App() {
    const { isAuthenticated } = useAuth();
    return (_jsxs(Routes, { children: [_jsx(Route, { path: "/login", element: _jsx(Login, {}) }), _jsxs(Route, { path: "/", element: isAuthenticated ? _jsx(MainLayout, {}) : _jsx(Navigate, { to: "/login", replace: true }), children: [_jsx(Route, { index: true, element: _jsx(Dashboard, {}) }), _jsx(Route, { path: "tenants", element: _jsx(Tenants, {}) }), _jsx(Route, { path: "nodes", element: _jsx(Nodes, {}) }), _jsx(Route, { path: "jobs", element: _jsx(Jobs, {}) })] }), _jsx(Route, { path: "*", element: _jsx(Navigate, { to: isAuthenticated ? '/' : '/login', replace: true }) })] }));
}
