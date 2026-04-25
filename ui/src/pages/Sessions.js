import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { Badge, stateToVariant } from '../components/Badge';
import { EmptyState } from '../components/EmptyState';
import { SessionReplay } from '../components/SessionReplay';
export function Sessions() {
    const client = useApiClient();
    const [sessions, setSessions] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [openSession, setOpenSession] = useState(null);
    const refresh = useCallback(async () => {
        setLoading(true);
        try {
            const resp = await client.listSessions({ limit: 50, offset: 0 });
            setSessions(resp.data);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
        finally {
            setLoading(false);
        }
    }, [client]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Privileged sessions" }), _jsx("h2", { children: "Recorded SSH & RDP sessions" }), _jsx("p", { className: "subtitle", children: "Replay any privileged session to verify what happened. Search commands, scrub timeline, export transcript for incident review." })] }), _jsx("button", { type: "button", className: "secondary-button", onClick: refresh, disabled: loading, children: loading ? 'Loading…' : 'Refresh' })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, openSession ? (_jsx(SessionReplay, { sessionId: openSession, onClose: () => setOpenSession(null) })) : sessions.length === 0 && !loading ? (_jsx(EmptyState, { title: "No recorded sessions yet", description: "Sessions appear here when an operator connects through the Control One bastion. Wire up access requests in /access to start capturing replays." })) : (_jsxs("table", { className: "data-table", style: { width: '100%' }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Started" }), _jsx("th", { children: "Type" }), _jsx("th", { children: "Node" }), _jsx("th", { children: "User" }), _jsx("th", { children: "Duration" }), _jsx("th", { children: "State" }), _jsx("th", {})] }) }), _jsx("tbody", { children: sessions.map((s) => (_jsxs("tr", { children: [_jsx("td", { children: new Date(s.started_at).toLocaleString() }), _jsx("td", { children: s.session_type }), _jsx("td", { children: _jsx("code", { children: s.node_id.slice(0, 8) }) }), _jsx("td", { children: s.user_id ? _jsx("code", { children: s.user_id.slice(0, 8) }) : '—' }), _jsx("td", { children: s.duration_seconds ? `${Math.round(s.duration_seconds)}s` : 'live' }), _jsx("td", { children: _jsx(Badge, { variant: stateToVariant(s.status), size: "sm", children: s.status }) }), _jsx("td", { children: _jsx("button", { type: "button", className: "primary-button", onClick: () => setOpenSession(s.id), disabled: !s.artifact_path, title: s.artifact_path ? 'Replay session' : 'No artifact stored', children: "Replay" }) })] }, s.id))) })] }))] }));
}
