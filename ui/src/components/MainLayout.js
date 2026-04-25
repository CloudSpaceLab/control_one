import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import { useTheme } from '../providers/ThemeProvider';
import { CommandPalette } from './CommandPalette';
import './MainLayout.css';
// Grouped IA: 7 sections, each with a clear workflow. Inside each group items
// are ordered by frequency, not alphabetically — execs hit "Posture" then
// scan to "Compliance"; sysadmins hit "Infrastructure" then "Nodes".
const NAV_GROUPS = [
    {
        label: 'Visibility',
        items: [
            { to: '/', label: 'Dashboard' },
            { to: '/alerts', label: 'Alerts' },
            { to: '/reports', label: 'Reports' },
        ],
    },
    {
        label: 'Posture',
        items: [
            { to: '/compliance', label: 'Compliance' },
            { to: '/audit', label: 'Audit log' },
            { to: '/telemetry', label: 'Telemetry' },
        ],
    },
    {
        label: 'Detect & respond',
        items: [
            { to: '/rules', label: 'Rules', roles: ['admin', 'operator'] },
            { to: '/threat-feeds', label: 'Threat sources', roles: ['admin', 'operator'] },
            { to: '/recommendations', label: 'Recommendations' },
        ],
    },
    {
        label: 'Access',
        items: [
            { to: '/access', label: 'Just-in-time access' },
            { to: '/sessions', label: 'Session replay' },
            { to: '/users', label: 'Users & roles', roles: ['admin'] },
        ],
    },
    {
        label: 'Infrastructure',
        items: [
            { to: '/nodes', label: 'Nodes' },
            { to: '/fleet-enroll', label: 'Fleet enrol', roles: ['admin', 'operator'] },
            { to: '/hypervisors', label: 'Hypervisors', roles: ['admin'] },
            { to: '/templates', label: 'Templates', roles: ['admin', 'operator'] },
        ],
    },
    {
        label: 'Automation',
        items: [
            { to: '/jobs', label: 'Jobs' },
            { to: '/offline-bundle', label: 'Offline bundle', roles: ['admin', 'operator'] },
        ],
    },
    {
        label: 'Configuration',
        items: [
            { to: '/tenants', label: 'Tenants', roles: ['admin'] },
            { to: '/secrets', label: 'Secrets', roles: ['admin'] },
            { to: '/settings', label: 'Settings', roles: ['admin'] },
        ],
    },
];
function filterGroups(groups, userRoles) {
    const isAdmin = userRoles.includes('admin');
    return groups
        .map((g) => ({
        label: g.label,
        items: g.items.filter((item) => {
            if (!item.roles || item.roles.length === 0)
                return true;
            if (isAdmin)
                return true;
            return item.roles.some((r) => userRoles.includes(r));
        }),
    }))
        .filter((g) => g.items.length > 0);
}
export function MainLayout() {
    const { signOut, profile } = useAuth();
    const { theme, toggleTheme } = useTheme();
    const location = useLocation();
    const [navOpen, setNavOpen] = useState(false);
    const userRoles = profile?.roles ?? [];
    const groups = filterGroups(NAV_GROUPS, userRoles);
    return (_jsxs("div", { className: "app-shell", children: [_jsx(CommandPalette, {}), _jsxs("aside", { className: `side-nav ${navOpen ? 'open' : ''}`, children: [_jsxs("div", { className: "brand", children: [_jsx("span", { className: "brand-mark", children: "\u25CE" }), _jsxs("div", { children: [_jsx("strong", { children: "Control One" }), _jsx("small", { children: "Operator console" })] })] }), _jsx("nav", { "aria-label": "Primary", children: groups.map((group) => (_jsxs("div", { className: "nav-group", children: [_jsx("div", { className: "nav-group__label", children: group.label }), _jsx("ul", { children: group.items.map((item) => {
                                        const isActive = item.to === '/'
                                            ? location.pathname === item.to
                                            : location.pathname.startsWith(item.to);
                                        return (_jsx("li", { className: isActive ? 'active' : '', children: _jsx(Link, { to: item.to, onClick: () => setNavOpen(false), children: item.label }) }, item.to));
                                    }) })] }, group.label))) }), _jsx("footer", { children: _jsxs("button", { type: "button", className: "cmdk-hint", "aria-label": "Open command palette", onClick: () => {
                                window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }));
                            }, children: [_jsx("span", { children: "Quick search" }), _jsx("kbd", { children: "\u2318K" })] }) })] }), navOpen ? _jsx("div", { className: "nav-backdrop", onClick: () => setNavOpen(false) }) : null, _jsxs("div", { className: "content-area", children: [_jsxs("header", { className: "content-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: profile?.user?.email ?? 'Signed in' }), _jsx("h1", { children: "Control One" }), _jsx("p", { className: "subtitle", children: "Find risk. Fix it. Prove it." })] }), _jsxs("div", { style: { display: 'flex', gap: '0.75rem', alignItems: 'center' }, children: [_jsx("button", { type: "button", onClick: toggleTheme, className: "theme-toggle", "aria-label": `Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`, title: `Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`, children: theme === 'dark' ? '☀️' : '🌙' }), _jsx("button", { type: "button", onClick: signOut, className: "signout-button", children: "Sign out" })] }), _jsxs("button", { type: "button", className: "nav-toggle", "aria-label": "Toggle navigation", onClick: () => setNavOpen((open) => !open), children: [_jsx("span", {}), _jsx("span", {}), _jsx("span", {})] })] }), _jsx("main", { className: "app-main", children: _jsx(Outlet, {}) })] })] }));
}
