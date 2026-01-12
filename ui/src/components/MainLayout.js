import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import { useTheme } from '../providers/ThemeProvider';
import './MainLayout.css';
const NAV_ITEMS = [
    { to: '/', label: 'Dashboard', icon: '📊', section: 'main' },
    { to: '/setup', label: 'Setup Wizard', icon: '🚀', section: 'main' },
    { to: '/tenants', label: 'Tenants', icon: '🏢', section: 'infrastructure' },
    { to: '/nodes', label: 'Nodes', icon: '🖥️', section: 'infrastructure' },
    { to: '/jobs', label: 'Jobs', icon: '⚙️', section: 'infrastructure' },
    { to: '/templates', label: 'Templates', icon: '📋', section: 'infrastructure' },
    { to: '/compliance', label: 'Compliance', icon: '✅', section: 'security' },
    { to: '/audit', label: 'Audit Log', icon: '📝', section: 'security' },
    { to: '/users', label: 'Users & Roles', icon: '👥', section: 'security' },
    { to: '/telemetry', label: 'Telemetry', icon: '📈', section: 'monitoring' },
    { to: '/secrets', label: 'Secrets', icon: '🔐', section: 'security' },
    { to: '/settings', label: 'Settings', icon: '⚙️', section: 'admin' },
];
const NAV_SECTIONS = [
    { id: 'main', title: 'Main' },
    { id: 'infrastructure', title: 'Infrastructure' },
    { id: 'security', title: 'Security' },
    { id: 'monitoring', title: 'Monitoring' },
    { id: 'admin', title: 'Admin' },
];
export function MainLayout() {
    const { signOut } = useAuth();
    const { theme, toggleTheme } = useTheme();
    const location = useLocation();
    const [navOpen, setNavOpen] = useState(false);
    return (_jsxs("div", { className: "app-shell", children: [_jsxs("aside", { className: `side-nav ${navOpen ? 'open' : ''}`, children: [_jsxs("div", { className: "brand", children: [_jsx("span", { className: "brand-mark", children: "\u25CE" }), _jsxs("div", { children: [_jsx("strong", { children: "Control One" }), _jsx("small", { children: "Operator" })] })] }), _jsx("nav", { children: NAV_SECTIONS.map((section) => (_jsxs("div", { className: "nav-section", children: [_jsx("div", { className: "nav-section-title", children: section.title }), _jsx("ul", { children: NAV_ITEMS.filter((item) => item.section === section.id).map((item) => {
                                        const isActive = item.to === '/' ? location.pathname === item.to : location.pathname.startsWith(item.to);
                                        return (_jsx("li", { className: isActive ? 'active' : '', children: _jsxs(Link, { to: item.to, children: [_jsx("span", { className: "nav-icon", children: item.icon }), item.label] }) }, item.to));
                                    }) })] }, section.id))) }), _jsx("footer", { children: _jsx("p", { children: "Secure \u2022 Multi-tenant" }) })] }), navOpen ? _jsx("div", { className: "nav-backdrop", onClick: () => setNavOpen(false) }) : null, _jsxs("div", { className: "content-area", children: [_jsxs("header", { className: "content-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Unified control plane" }), _jsx("h1", { children: "Operator Console" }), _jsx("p", { className: "subtitle", children: "Provision infrastructure, enforce policy, and monitor node posture." })] }), _jsxs("div", { style: { display: 'flex', gap: '0.75rem', alignItems: 'center' }, children: [_jsx("button", { type: "button", onClick: toggleTheme, className: "theme-toggle", "aria-label": `Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`, title: `Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`, children: theme === 'dark' ? '☀️' : '🌙' }), _jsx("button", { type: "button", onClick: signOut, className: "signout-button", children: "Sign out" })] }), _jsxs("button", { type: "button", className: "nav-toggle", "aria-label": "Toggle navigation", onClick: () => setNavOpen((open) => !open), children: [_jsx("span", {}), _jsx("span", {}), _jsx("span", {})] })] }), _jsx("main", { className: "app-main", children: _jsx(Outlet, {}) })] })] }));
}
