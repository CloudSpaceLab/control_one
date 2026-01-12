import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useJobs } from '../hooks/useJobs';
import { EnterpriseLayout, ExecutiveOverview, ManagementPanel, ActionZone, ContentGrid } from '../components/EnterpriseLayout';
import '../components/EnterpriseLayout.css';
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
    return (_jsxs(EnterpriseLayout, { variant: "dashboard", children: [tenantPagination.total === 0 && (_jsx(ManagementPanel, { title: "\uD83D\uDE80 Get Started with Control One", subtitle: "Your control plane is ready! Let's set up your first tenant and configure your infrastructure.", position: "primary", children: _jsxs(ActionZone, { alignment: "center", variant: "primary", children: [_jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/setup'), children: "Launch Setup Wizard" }), _jsx("button", { type: "button", className: "ghost-button", onClick: () => navigate('/tenants'), children: "Manual Setup" })] }) })), _jsx(ExecutiveOverview, { title: "\uD83D\uDCCA Executive Dashboard", subtitle: "Real-time system posture and performance metrics", children: stats.map((stat) => (_jsxs("article", { className: "stat-card", children: [_jsx("p", { children: stat.label }), stat.loading ? (_jsx("span", { className: "stat-value pulse", children: "\u2026" })) : (_jsx(StatValue, { value: stat.value })), _jsx("small", { children: stat.trend })] }, stat.label))) }), _jsxs("div", { className: "executive-main", children: [_jsx(ManagementPanel, { title: "\u26A1 Quick Actions", subtitle: "Common tasks to get you started", position: "primary", children: _jsx(ContentGrid, { columns: 1, gap: "md", children: quickActions.map((action) => (_jsxs("div", { className: "quick-action-item", children: [_jsxs("div", { className: "quick-action-content", children: [_jsx("strong", { children: action.title }), _jsx("p", { children: action.copy })] }), _jsx(ActionZone, { alignment: "right", variant: "secondary", children: _jsx("button", { type: "button", className: "primary-button", onClick: action.action, children: "Launch" }) })] }, action.title))) }) }), _jsx(ManagementPanel, { title: "\uD83C\uDFE5 System Health", subtitle: "Overall system status and performance", position: "secondary", children: _jsxs(ContentGrid, { columns: 2, gap: "md", children: [_jsxs("div", { className: "health-item", children: [_jsx("div", { className: "health-indicator health-good" }), _jsxs("div", { className: "health-content", children: [_jsx("strong", { children: "Control Plane" }), _jsx("small", { children: "Operational" })] })] }), _jsxs("div", { className: "health-item", children: [_jsx("div", { className: "health-indicator health-good" }), _jsxs("div", { className: "health-content", children: [_jsx("strong", { children: "Database" }), _jsx("small", { children: "Connected" })] })] }), _jsxs("div", { className: "health-item", children: [_jsx("div", { className: "health-indicator health-warning" }), _jsxs("div", { className: "health-content", children: [_jsx("strong", { children: "Node Mesh" }), _jsx("small", { children: nodePagination.total > 0 ? 'Active' : 'No nodes' })] })] }), _jsxs("div", { className: "health-item", children: [_jsx("div", { className: "health-indicator health-good" }), _jsxs("div", { className: "health-content", children: [_jsx("strong", { children: "Job Queue" }), _jsx("small", { children: jobStatusSummary.running > 0 ? 'Processing' : 'Idle' })] })] })] }) })] }), _jsxs("div", { className: "executive-sidebar", children: [_jsx(ManagementPanel, { title: "\uD83D\uDCC8 Recent Activity", subtitle: `${recentJobs.length} recent jobs`, position: "tertiary", children: jobsLoading ? (_jsx("p", { className: "muted", children: "Loading activity\u2026" })) : recentJobs.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No jobs have run yet." }) })) : (_jsx("div", { className: "activity-list", children: recentJobs.map((job) => (_jsxs("div", { className: "activity-item", children: [_jsxs("div", { className: "activity-content", children: [_jsx("strong", { children: job.type }), _jsx("small", { children: new Date(job.created_at).toLocaleString() })] }), _jsx("span", { className: `status-pill status-${job.status.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`, children: job.status })] }, job.id))) })) }), _jsx(ManagementPanel, { title: "\uD83C\uDFE2 Tenants at a Glance", subtitle: `${tenantPagination.total} total tenants`, position: "tertiary", children: tenantsLoading ? (_jsx("p", { className: "muted", children: "Loading tenants\u2026" })) : tenantSample.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No tenants yet. Create one to get started." }) })) : (_jsx("div", { className: "tenant-summary-list", children: tenantSample.map((tenant) => (_jsx("div", { className: "tenant-summary-item", children: _jsxs("div", { className: "tenant-summary-content", children: [_jsx("strong", { children: tenant.name }), _jsx("small", { children: new Date(tenant.created_at).toLocaleDateString() })] }) }, tenant.id))) })) })] })] }));
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
