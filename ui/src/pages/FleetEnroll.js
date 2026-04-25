import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
// JOB_POLL_MS drives how often we re-fetch the fleet enroll status. 1000ms
// matches the plan's 1s cadence and keeps the UI responsive without hammering
// the API while 50+ hosts are being SSH'd in parallel.
const JOB_POLL_MS = 1000;
// NODE_POLL_MS is the slower cadence used once the SSH-provisioning phase has
// ended and we're watching the per-node state transitions (pending -> active
// or enrollment_failed). 5s matches the 10-minute enrollment-pending timeout.
const NODE_POLL_MS = 5000;
// MAX_NODE_POLL_MS caps how long we keep polling a single node. 10 minutes
// covers the full enrollment_pending timeout + a generous buffer.
const MAX_NODE_POLL_MS = 10 * 60 * 1000;
// parseTargets splits the newline-separated host list into FleetEnrollTargets.
// Blank lines and # comments are ignored. Each non-comment line may be
// `host`, `host:port`, or `user@host[:port]`. The server-side validator has
// the authoritative rules; this is a UX hint.
function parseTargets(raw) {
    return raw
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line && !line.startsWith('#'))
        .map((line) => {
        let user;
        let hostPort = line;
        if (line.includes('@')) {
            const [u, rest] = line.split('@', 2);
            user = u.trim() || undefined;
            hostPort = rest.trim();
        }
        const colonIdx = hostPort.lastIndexOf(':');
        if (colonIdx > 0) {
            const hostPart = hostPort.substring(0, colonIdx);
            const portPart = hostPort.substring(colonIdx + 1);
            const port = Number.parseInt(portPart, 10);
            if (!Number.isNaN(port) && port > 0) {
                return { host: hostPart, port, user };
            }
        }
        return { host: hostPort, user };
    });
}
// encodeSshKey converts a PEM-formatted SSH private key into the base64
// payload the server expects. An already-base64 paste (no BEGIN marker) is
// returned as-is.
function encodeSshKey(raw) {
    const trimmed = raw.trim();
    if (!trimmed) {
        return '';
    }
    if (trimmed.includes('-----BEGIN')) {
        // Browser btoa handles ASCII; PEM is 7-bit ASCII so this is safe.
        return btoa(trimmed);
    }
    return trimmed;
}
export function FleetEnroll() {
    const api = useApiClient();
    const { showToast } = useToast();
    const { error: formError, success: formSuccess, showError, showSuccess, reset: resetFeedback, } = useFormFeedback();
    const { data: tenants } = useTenants();
    const [tenantId, setTenantId] = useState('');
    const [targetsRaw, setTargetsRaw] = useState('');
    const [sshUser, setSshUser] = useState('');
    const [sshKey, setSshKey] = useState('');
    const [sshPassword, setSshPassword] = useState('');
    const [token, setToken] = useState('');
    const [parallel, setParallel] = useState(5);
    const [submitting, setSubmitting] = useState(false);
    const [jobId, setJobId] = useState(null);
    const [jobStatus, setJobStatus] = useState(null);
    const [pollError, setPollError] = useState(null);
    // Per-host node gate state. Keyed by SSH host so we can merge enrollment
    // results (which produce node ids) with node-state polls.
    const [nodeStates, setNodeStates] = useState({});
    const parsedTargets = useMemo(() => parseTargets(targetsRaw), [targetsRaw]);
    // jobStatusRef mirrors jobStatus so the polling loop can short-circuit on
    // a terminal status without re-registering the interval every tick.
    const jobStatusRef = useRef(null);
    useEffect(() => {
        jobStatusRef.current = jobStatus;
    }, [jobStatus]);
    // ── job polling ───────────────────────────────────────────────────────
    useEffect(() => {
        if (!jobId) {
            return undefined;
        }
        let cancelled = false;
        const terminal = new Set(['succeeded', 'failed', 'cancelled']);
        const fetchStatus = async () => {
            try {
                const status = await api.getFleetEnrollStatus(jobId);
                if (!cancelled) {
                    setJobStatus(status);
                    setPollError(null);
                }
            }
            catch (err) {
                if (!cancelled) {
                    const message = err instanceof Error ? err.message : 'Failed to poll fleet job';
                    setPollError(message);
                }
            }
        };
        fetchStatus();
        const timer = setInterval(() => {
            if (jobStatusRef.current && terminal.has(jobStatusRef.current.status)) {
                clearInterval(timer);
                return;
            }
            fetchStatus();
        }, JOB_POLL_MS);
        return () => {
            cancelled = true;
            clearInterval(timer);
        };
    }, [api, jobId]);
    // When the fleet job reports per-host results, ensure we start tracking
    // node-gate state for every successful enrollment.
    useEffect(() => {
        if (!jobStatus) {
            return;
        }
        const now = Date.now();
        setNodeStates((prev) => {
            const next = { ...prev };
            for (const result of jobStatus.results) {
                if (!result.success || !result.node_id) {
                    continue;
                }
                if (!next[result.host]) {
                    next[result.host] = {
                        nodeId: result.node_id,
                        host: result.host,
                        state: 'enrollment_pending',
                        startedAt: now,
                    };
                }
                else if (!next[result.host].nodeId) {
                    next[result.host] = { ...next[result.host], nodeId: result.node_id };
                }
            }
            return next;
        });
    }, [jobStatus]);
    // ── node gate polling ─────────────────────────────────────────────────
    const nodeTimersRef = useRef(new Map());
    useEffect(() => {
        const activeTimers = nodeTimersRef.current;
        const entries = Object.values(nodeStates);
        for (const gate of entries) {
            if (!gate.nodeId) {
                continue;
            }
            // Skip nodes in terminal gate states or aged past the timeout.
            const terminal = gate.state === 'active' || gate.state === 'enrollment_failed' || gate.state === 'retired';
            const expired = Date.now() - gate.startedAt > MAX_NODE_POLL_MS;
            if (terminal || expired) {
                const existing = activeTimers.get(gate.host);
                if (existing) {
                    clearInterval(existing);
                    activeTimers.delete(gate.host);
                }
                continue;
            }
            if (activeTimers.has(gate.host)) {
                continue;
            }
            const fetchNode = async () => {
                try {
                    const node = (await api.getNode(gate.nodeId));
                    setNodeStates((prev) => {
                        const existing = prev[gate.host];
                        if (!existing) {
                            return prev;
                        }
                        return {
                            ...prev,
                            [gate.host]: {
                                ...existing,
                                state: String(node.state),
                                lastSeenAt: node.last_seen_at,
                                firstScanAt: node.first_scan_at,
                                updatedAt: node.updated_at,
                                error: undefined,
                            },
                        };
                    });
                }
                catch (err) {
                    const message = err instanceof Error ? err.message : 'Failed to poll node';
                    setNodeStates((prev) => {
                        const existing = prev[gate.host];
                        if (!existing) {
                            return prev;
                        }
                        return { ...prev, [gate.host]: { ...existing, error: message } };
                    });
                }
            };
            fetchNode();
            const timer = setInterval(fetchNode, NODE_POLL_MS);
            activeTimers.set(gate.host, timer);
        }
        return () => {
            // no-op on re-render; teardown happens on unmount below
        };
    }, [api, nodeStates]);
    useEffect(() => {
        // Snapshot the ref on effect setup so the cleanup doesn't reach into a
        // mutated ref after unmount (react-hooks/exhaustive-deps warns otherwise).
        const timers = nodeTimersRef.current;
        return () => {
            for (const timer of timers.values()) {
                clearInterval(timer);
            }
            timers.clear();
        };
    }, []);
    // ── submit handler ────────────────────────────────────────────────────
    const handleSubmit = async (event) => {
        event.preventDefault();
        resetFeedback();
        if (parsedTargets.length === 0) {
            showError('Add at least one host');
            return;
        }
        if (!token.trim()) {
            showError('Enrollment token is required');
            return;
        }
        const hasPerTargetUsers = parsedTargets.every((t) => !!t.user?.trim());
        if (!sshUser.trim() && !hasPerTargetUsers) {
            showError('SSH user is required (or include user@host in each target line)');
            return;
        }
        if (!sshKey.trim() && !sshPassword.trim()) {
            showError('Either an SSH private key or a password is required');
            return;
        }
        const payload = {
            targets: parsedTargets,
            token: token.trim(),
            parallel,
        };
        if (sshUser.trim()) {
            payload.ssh_user = sshUser.trim();
        }
        if (sshKey.trim()) {
            payload.ssh_key = encodeSshKey(sshKey);
        }
        if (sshPassword.trim()) {
            payload.ssh_password = sshPassword;
        }
        try {
            setSubmitting(true);
            const response = await api.startFleetEnroll(payload);
            setJobId(response.job_id);
            setJobStatus(null);
            setNodeStates({});
            showSuccess(`Fleet job ${response.job_id} queued for ${parsedTargets.length} target(s)`);
            showToast(`Fleet enrollment started — ${parsedTargets.length} hosts`, 'success');
            if (tenantId) {
                // tenant id is captured purely for UX / downstream linking today;
                // the fleet endpoint infers the tenant from the enrollment token.
            }
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Fleet enrollment failed';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setSubmitting(false);
        }
    };
    // ── render helpers ────────────────────────────────────────────────────
    function statusBadge(status) {
        const cls = status.toLowerCase().replace(/[^a-z0-9]+/g, '-');
        return _jsx("span", { className: `status-pill status-${cls}`, children: status });
    }
    function formatDuration(ms) {
        if (!ms)
            return '—';
        if (ms < 1000)
            return `${ms}ms`;
        return `${(ms / 1000).toFixed(1)}s`;
    }
    function renderNodeGate(host) {
        const gate = nodeStates[host];
        if (!gate?.nodeId) {
            return _jsx("span", { className: "muted", children: "\u2014" });
        }
        const icon = gate.state === 'active'
            ? 'OK'
            : gate.state === 'enrollment_failed'
                ? 'FAIL'
                : 'WAIT';
        return (_jsxs("span", { className: `status-pill status-${gate.state.replace(/_/g, '-')}`, title: gate.error ?? gate.state, children: [icon, " ", gate.state] }));
    }
    const jobTerminal = jobStatus?.status === 'succeeded' || jobStatus?.status === 'failed';
    const anyStillPending = Object.values(nodeStates).some((g) => g.state === 'enrollment_pending');
    const showReadyNotice = jobTerminal && !anyStillPending && Object.keys(nodeStates).length > 0;
    return (_jsxs("section", { className: "dashboard-section", "aria-labelledby": "fleet-enroll-heading", children: [_jsx("header", { className: "dashboard-header", children: _jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Onboarding" }), _jsx("h2", { id: "fleet-enroll-heading", children: "Bulk enrol hosts" }), _jsx("p", { className: "subtitle", children: "Onboard many hosts over SSH at once. Live progress per target." })] }) }), _jsxs("article", { className: "card", children: [_jsx("h3", { children: "1. Targets" }), _jsxs("form", { onSubmit: handleSubmit, "aria-label": "fleet enrollment form", children: [_jsxs("div", { className: "form-grid", children: [_jsxs("label", { className: "form-field form-field-full", children: [_jsxs("span", { children: ["Hosts (one per line, ", _jsx("code", { children: "host" }), ", ", _jsx("code", { children: "host:port" }), ", or ", _jsx("code", { children: "user@host:port" }), ")"] }), _jsx("textarea", { rows: 6, value: targetsRaw, onChange: (e) => setTargetsRaw(e.target.value), placeholder: `10.0.1.5\nadmin@10.0.1.6:22\n# comments allowed`, "aria-label": "targets" }), _jsxs("small", { children: [parsedTargets.length, " target(s) parsed"] })] }), _jsxs("label", { className: "form-field", children: [_jsx("span", { children: "Default SSH user" }), _jsx("input", { type: "text", value: sshUser, onChange: (e) => setSshUser(e.target.value), autoComplete: "username", "aria-label": "ssh user" })] }), _jsxs("label", { className: "form-field", children: [_jsx("span", { children: "Parallelism" }), _jsx("input", { type: "number", min: 1, max: 50, value: parallel, onChange: (e) => setParallel(Math.max(1, Number.parseInt(e.target.value, 10) || 1)), "aria-label": "parallelism" })] }), _jsxs("label", { className: "form-field form-field-full", children: [_jsx("span", { children: "SSH private key (PEM)" }), _jsx("textarea", { rows: 4, value: sshKey, onChange: (e) => setSshKey(e.target.value), placeholder: `-----BEGIN OPENSSH PRIVATE KEY-----\n...`, "aria-label": "ssh private key", autoComplete: "off" }), _jsx("small", { children: "Key is held in memory only \u2014 never persisted." })] }), _jsxs("label", { className: "form-field", children: [_jsx("span", { children: "SSH password (fallback)" }), _jsx("input", { type: "password", value: sshPassword, onChange: (e) => setSshPassword(e.target.value), autoComplete: "new-password", "aria-label": "ssh password" })] }), _jsxs("label", { className: "form-field", children: [_jsx("span", { children: "Enrollment token" }), _jsx("input", { type: "text", value: token, onChange: (e) => setToken(e.target.value), placeholder: "cot_\u2026", autoComplete: "off", "aria-label": "enrollment token" })] }), _jsxs("label", { className: "form-field", children: [_jsx("span", { children: "Tenant (optional, for reference)" }), _jsxs("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "tenant", children: [_jsx("option", { value: "", children: "\u2014 none \u2014" }), tenants.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id)))] })] })] }), formError ? _jsx("p", { className: "form-error", role: "alert", children: formError }) : null, formSuccess ? _jsx("p", { className: "form-success", role: "status", children: formSuccess }) : null, _jsx("div", { className: "form-actions", children: _jsx("button", { type: "submit", className: "primary-button", disabled: submitting, children: submitting ? 'Starting…' : 'Start fleet enrollment' }) })] })] }), jobId ? (_jsxs("article", { className: "card", children: [_jsx("h3", { children: "2. Per-host progress" }), _jsxs("p", { className: "muted", children: ["Job ", _jsx("code", { children: jobId }), " \u2014", ' ', jobStatus ? statusBadge(jobStatus.status) : 'queued'] }), pollError ? _jsx("p", { className: "form-error", children: pollError }) : null, _jsxs("table", { className: "data-table", "aria-label": "per-host enrollment progress", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Host" }), _jsx("th", { children: "Port" }), _jsx("th", { children: "SSH" }), _jsx("th", { children: "Duration" }), _jsx("th", { children: "Node" }), _jsx("th", { children: "Gate" }), _jsx("th", { children: "Error" })] }) }), _jsxs("tbody", { children: [(jobStatus?.results ?? []).map((result) => (_jsxs("tr", { children: [_jsx("td", { children: result.host }), _jsx("td", { children: result.port || '—' }), _jsx("td", { children: result.success ? (_jsx("span", { className: "status-pill status-succeeded", children: "OK" })) : (_jsx("span", { className: "status-pill status-failed", children: "FAIL" })) }), _jsx("td", { children: formatDuration(result.duration_ms) }), _jsx("td", { children: result.node_id ? (_jsxs("a", { href: `/nodes?id=${result.node_id}`, children: [result.node_id.slice(0, 8), "\u2026"] })) : ('—') }), _jsx("td", { children: renderNodeGate(result.host) }), _jsx("td", { className: "muted", title: result.error_message ?? '', children: result.error_message ? result.error_message.slice(0, 80) : '—' })] }, result.id))), !jobStatus || jobStatus.results.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 7, className: "muted", children: "Awaiting first results\u2026" }) })) : null] })] }), showReadyNotice ? (_jsx("p", { className: "form-success", role: "status", children: "All hosts passed the enrollment gate." })) : null] })) : null] }));
}
