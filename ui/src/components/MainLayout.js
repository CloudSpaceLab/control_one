import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import './MainLayout.css';
const NAV_ITEMS = [
    { to: '/', label: 'Dashboard' },
    { to: '/tenants', label: 'Tenants' },
    { to: '/nodes', label: 'Nodes' },
    { to: '/jobs', label: 'Jobs' },
];
export function MainLayout() {
    const { signOut } = useAuth();
    const location = useLocation();
    return (_jsxs("div", { className: "app-shell", children: [_jsxs("header", { className: "app-header", children: [_jsx("h1", { children: "Control One" }), _jsx("nav", { children: _jsx("ul", { children: NAV_ITEMS.map((item) => (_jsx("li", { className: location.pathname === item.to ? 'active' : '', children: _jsx(Link, { to: item.to, children: item.label }) }, item.to))) }) }), _jsx("button", { type: "button", onClick: signOut, className: "signout-button", children: "Sign out" })] }), _jsx("main", { className: "app-main", children: _jsx(Outlet, {}) })] }));
}
