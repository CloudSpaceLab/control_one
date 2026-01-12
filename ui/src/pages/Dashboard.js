import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useJobs } from '../hooks/useJobs';
import { DemandForm } from '../components/DemandForm';
const JOB_POLL_INTERVAL = 0;
export function Dashboard() {
    const navigate = useNavigate();
    const { pagination: tenantPagination, data: tenantSample, loading: tenantsLoading, } = useTenants({ limit: 5, offset: 0 });
    const { pagination: nodePagination, loading: nodesLoading, } = useNodes({ limit: 5, offset: 0 });
    const { pagination: jobPagination, data: recentJobs, loading: jobsLoading, } = useJobs({ limit: 5, offset: 0, pollIntervalMs: JOB_POLL_INTERVAL });
    const jobStatusSummary = useMemo(() => {
        return recentJobs.reduce((acc, job) => {
            const key = job.status.toLowerCase();
            acc[key] = (acc[key] ?? 0) + 1;
            return acc;
        }, {});
    }, [recentJobs]);
    const stats = [
        {
            label: 'Tenants',
            value: tenantPagination.total || 0,
            loading: tenantsLoading,
            trend: tenantPagination.total > 0 ? '+ steady' : '—',
        },
        {
            label: 'Managed Nodes',
            value: nodePagination.total || 0,
            loading: nodesLoading,
            trend: nodePagination.total > 0 ? 'mesh healthy' : '—',
        },
        {
            label: 'Jobs queued',
            value: jobPagination.total || 0,
            loading: jobsLoading,
            trend: jobStatusSummary.running ? `${jobStatusSummary.running} running` : 'idle',
        },
        {
            label: 'Successful jobs',
            value: jobStatusSummary.succeeded ?? 0,
            loading: jobsLoading,
            trend: jobStatusSummary.failed ? `${jobStatusSummary.failed} failed` : 'clean slate',
        },
    ];
    const quickActions = [
        {
            title: 'Register node',
            copy: 'Bootstrap a new agent and enroll it into a tenant boundary.',
            action: () => navigate('/nodes'),
        },
        {
            title: 'Create tenant',
            copy: 'Spin up a workspace for a new environment or customer.',
            action: () => navigate('/tenants'),
        },
        {
            title: 'Launch job',
            copy: 'Kick off a provisioning or compliance job against your fleet.',
            action: () => navigate('/jobs'),
        },
    ];
    return (_jsxs("div", { className: "focused-content", children: [tenantPagination.total === 0 && (_jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDE80 Get Started with Control One" }), _jsx("p", { className: "focused-section-subtitle", children: "Your control plane is ready! Let's set up your first tenant and configure your infrastructure." })] }), _jsx("div", { className: "focused-section-content", children: _jsxs("div", { className: "setup-prompt-actions", children: [_jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/setup'), children: "Launch Setup Wizard" }), _jsx("button", { type: "button", className: "ghost-button", onClick: () => navigate('/tenants'), children: "Manual Setup" })] }) })] })), _jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDCCA System Overview" }), _jsx("p", { className: "focused-section-subtitle", children: "Monitor posture, trigger workflows, or drill into tenants from here." })] }), _jsx("div", { className: "focused-section-content", children: _jsx("div", { className: "stat-grid", children: stats.map((stat) => (_jsxs("article", { className: "stat-card", children: [_jsx("p", { children: stat.label }), stat.loading ? (_jsx("span", { className: "stat-value pulse", children: "\u2026" })) : (_jsx(StatValue, { value: stat.value })), _jsx("small", { children: stat.trend })] }, stat.label))) }) })] }), _jsx(DemandForm, { title: "Quick Actions", icon: "\u26A1", summary: "Common tasks to get you started", children: _jsx("ul", { children: quickActions.map((action) => (_jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: action.title }), _jsx("p", { children: action.copy })] }), _jsx("button", { type: "button", className: "primary-button", onClick: action.action, children: "Go" })] }, action.title))) }) }), _jsx(DemandForm, { title: "Recent Activity", icon: "\uD83D\uDCC8", summary: `${recentJobs.length} recent jobs`, children: jobsLoading ? (_jsx("p", { className: "muted", children: "Loading activity\u2026" })) : recentJobs.length === 0 ? (_jsx("p", { className: "muted", children: "No jobs have run yet." })) : (_jsx("ul", { children: recentJobs.map((job) => (_jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: job.type }), _jsx("small", { children: new Date(job.created_at).toLocaleString() })] }), _jsx("span", { className: `status-pill status-${job.status.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`, children: job.status })] }, job.id))) })) }), _jsx(DemandForm, { title: "Tenants at a Glance", icon: "\uD83C\uDFE2", summary: `${tenantPagination.total} total tenants`, children: tenantsLoading ? (_jsx("p", { className: "muted", children: "Loading tenants\u2026" })) : tenantSample.length === 0 ? (_jsx("p", { className: "muted", children: "No tenants yet. Create one to get started." })) : (_jsx("dl", { children: tenantSample.map((tenant) => (_jsxs("div", { children: [_jsx("dt", { children: tenant.name }), _jsx("dd", { children: new Date(tenant.created_at).toLocaleDateString() })] }, tenant.id))) })) })] }));
}
function StatValue({ value }) {
    const [displayValue, setDisplayValue] = useState(0);
    useEffect(() => {
        let frame;
        let start = null;
        const duration = 800;
        const initial = displayValue;
        const delta = value - initial;
        const step = (timestamp) => {
            if (start === null) {
                start = timestamp;
            }
            const progress = Math.min((timestamp - start) / duration, 1);
            setDisplayValue(initial + delta * progress);
            if (progress < 1) {
                frame = requestAnimationFrame(step);
            }
        };
        frame = requestAnimationFrame(step);
        return () => {
            if (frame) {
                cancelAnimationFrame(frame);
            }
        };
    }, [value]);
    return _jsx("span", { className: "stat-value", children: Math.round(displayValue).toLocaleString() });
}
