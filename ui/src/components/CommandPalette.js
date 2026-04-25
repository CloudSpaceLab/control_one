import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import './CommandPalette.css';
const COMMANDS = [
    // Visibility
    { id: 'go.dashboard', group: 'Go to', label: 'Dashboard', hint: 'Overview', to: '/' },
    { id: 'go.alerts', group: 'Go to', label: 'Alerts', hint: 'Inbox', to: '/alerts' },
    { id: 'go.reports', group: 'Go to', label: 'Reports', hint: 'CSV exports', to: '/reports' },
    // Posture
    { id: 'go.compliance', group: 'Go to', label: 'Compliance', hint: 'Posture', to: '/compliance' },
    { id: 'go.audit', group: 'Go to', label: 'Audit Log', hint: 'Trail', to: '/audit' },
    // Detect
    { id: 'go.rules', group: 'Go to', label: 'Rules', hint: 'Detection', to: '/rules' },
    { id: 'go.builder', group: 'Go to', label: 'Rule builder', hint: 'Visual builder tab', to: '/rules', keywords: ['build', 'visual', 'drag', 'simulate'] },
    { id: 'go.recs', group: 'Go to', label: 'Recommendations', hint: 'Suggestions', to: '/recommendations' },
    { id: 'go.threat', group: 'Go to', label: 'Threat sources', hint: 'Abuse IP feeds', to: '/threat-feeds', keywords: ['threat', 'feed', 'abuse', 'ip', 'spamhaus', 'firehol', 'tor'] },
    // Access
    { id: 'go.access', group: 'Go to', label: 'Access', hint: 'JIT requests', to: '/access' },
    { id: 'go.sessions', group: 'Go to', label: 'Session replay', hint: 'Recorded SSH/RDP', to: '/sessions', keywords: ['replay', 'recording', 'transcript'] },
    // Infrastructure
    { id: 'go.nodes', group: 'Go to', label: 'Nodes', to: '/nodes' },
    { id: 'go.fleet', group: 'Go to', label: 'Fleet Enroll', to: '/fleet-enroll' },
    { id: 'go.hyper', group: 'Go to', label: 'Hypervisors', to: '/hypervisors' },
    { id: 'go.templates', group: 'Go to', label: 'Templates', to: '/templates' },
    { id: 'go.jobs', group: 'Go to', label: 'Jobs', to: '/jobs' },
    // Configuration
    { id: 'go.tenants', group: 'Go to', label: 'Tenants', to: '/tenants' },
    { id: 'go.users', group: 'Go to', label: 'Users & Roles', to: '/users' },
    { id: 'go.secrets', group: 'Go to', label: 'Secrets', to: '/secrets' },
    { id: 'go.settings', group: 'Go to', label: 'Settings', to: '/settings' },
    { id: 'go.telemetry', group: 'Go to', label: 'Telemetry', to: '/telemetry' },
    { id: 'go.bundle', group: 'Go to', label: 'Offline Bundle', to: '/offline-bundle' },
];
// CommandPalette is the Cmd+K (Ctrl+K on Windows/Linux) launcher. It searches
// across navigation targets and surfaces the most-used jumps without making
// the user navigate the side nav. Keyboard-only by design — Esc closes,
// Up/Down move, Enter activates, Tab cycles groups.
export function CommandPalette() {
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState('');
    const [active, setActive] = useState(0);
    const inputRef = useRef(null);
    const navigate = useNavigate();
    useEffect(() => {
        const handler = (e) => {
            const isToggle = (e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey);
            if (isToggle) {
                e.preventDefault();
                setOpen((v) => !v);
                return;
            }
            if (open && e.key === 'Escape') {
                setOpen(false);
            }
        };
        window.addEventListener('keydown', handler);
        return () => window.removeEventListener('keydown', handler);
    }, [open]);
    useEffect(() => {
        if (open) {
            setQuery('');
            setActive(0);
            requestAnimationFrame(() => inputRef.current?.focus());
        }
    }, [open]);
    const matches = useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q)
            return COMMANDS;
        return COMMANDS.filter((c) => {
            const hay = [c.label, c.hint ?? '', c.group, ...(c.keywords ?? [])].join(' ').toLowerCase();
            return hay.includes(q);
        });
    }, [query]);
    const grouped = useMemo(() => {
        const m = new Map();
        matches.forEach((c) => {
            const list = m.get(c.group) ?? [];
            list.push(c);
            m.set(c.group, list);
        });
        return Array.from(m.entries());
    }, [matches]);
    if (!open)
        return null;
    const run = (c) => {
        if (c.to)
            navigate(c.to);
        if (c.action)
            c.action();
        setOpen(false);
    };
    const onKeyDown = (e) => {
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            setActive((i) => Math.min(i + 1, matches.length - 1));
        }
        else if (e.key === 'ArrowUp') {
            e.preventDefault();
            setActive((i) => Math.max(i - 1, 0));
        }
        else if (e.key === 'Enter' && matches[active]) {
            e.preventDefault();
            run(matches[active]);
        }
    };
    let cursor = 0;
    return (_jsx("div", { className: "co-palette__backdrop", role: "dialog", "aria-modal": "true", "aria-label": "Command palette", onClick: () => setOpen(false), children: _jsxs("div", { className: "co-palette", onClick: (e) => e.stopPropagation(), children: [_jsx("input", { ref: inputRef, className: "co-palette__input", placeholder: "Search pages, actions\u2026 (Esc to close)", value: query, onChange: (e) => {
                        setQuery(e.target.value);
                        setActive(0);
                    }, onKeyDown: onKeyDown, "aria-label": "Command search" }), _jsx("div", { className: "co-palette__list", role: "listbox", children: grouped.length === 0 ? (_jsx("div", { className: "co-palette__empty", children: "No matches." })) : (grouped.map(([group, items]) => (_jsxs("div", { className: "co-palette__group", children: [_jsx("div", { className: "co-palette__group-label", children: group }), items.map((c) => {
                                const idx = cursor++;
                                const isActive = idx === active;
                                return (_jsxs("button", { type: "button", role: "option", "aria-selected": isActive, className: `co-palette__item${isActive ? ' co-palette__item--active' : ''}`, onMouseEnter: () => setActive(idx), onClick: () => run(c), children: [_jsx("span", { children: c.label }), c.hint ? _jsx("small", { children: c.hint }) : null] }, c.id));
                            })] }, group)))) }), _jsxs("div", { className: "co-palette__footer", children: [_jsx("kbd", { children: "\u2191" }), _jsx("kbd", { children: "\u2193" }), " navigate \u00B7 ", _jsx("kbd", { children: "Enter" }), " open \u00B7 ", _jsx("kbd", { children: "Esc" }), " close \u00B7 ", _jsx("kbd", { children: "\u2318K" }), " toggle"] })] }) }));
}
