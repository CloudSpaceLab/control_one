import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { useCancelJob } from '../hooks/useCancelJob';
import { EnterpriseLayout, ExecutiveOverview, ManagementPanel, ActionZone, ContentGrid } from '../components/EnterpriseLayout';
import '../components/EnterpriseLayout.css';
const STATUS_FILTERS = ['queued', 'running', 'succeeded', 'failed', 'cancelled'];
function formatDate(value) {
    if (!value) {
        return '—';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return value;
    }
    return date.toLocaleString();
}
function statusClass(status) {
    return status.toLowerCase().replace(/[^a-z0-9]+/g, '-');
}
function statusLabel(status) {
    return status.replace(/_/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase());
}
function summarizeStatuses(jobs) {
    return jobs.reduce((acc, job) => {
        const key = statusClass(job.status);
        acc[key] = (acc[key] ?? 0) + 1;
        return acc;
    }, {});
}
function jobCancelable(status) {
    return ['queued', 'running'].includes(status.toLowerCase());
}
function safeStringify(data) {
    if (data === null || data === undefined) {
        return '';
    }
    try {
        return JSON.stringify(data, null, 2);
    }
    catch {
        return String(data);
    }
}
function renderPayload(payload) {
    return _jsx("pre", { children: safeStringify(payload) });
}
export function Jobs() {
    const { showToast } = useToast();
    const { data: tenants } = useTenants();
    const [tenantFilter, setTenantFilter] = useState('');
    const [statusFilter, setStatusFilter] = useState('');
    const [typeFilter, setTypeFilter] = useState('');
    const [limit] = useState(20);
    const [selectedJobId, setSelectedJobId] = useState(null);
    const [isCancelling, setIsCancelling] = useState(false);
    const templateOptions = {
        tenant_id: tenantFilter.trim() || undefined,
        status: statusFilter.trim() || undefined,
        type: typeFilter.trim() || undefined,
        limit,
    };
    const { data: jobs, pagination, loading, refresh } = useJobs(templateOptions);
    const { status: workerStatus } = useWorkerStatus();
    const { cancelJob } = useCancelJob();
    const rows = useMemo(() => jobs, [jobs]);
    const selectedJob = useMemo(() => rows.find((job) => job.id === selectedJobId) ?? null, [rows, selectedJobId]);
    const statusSummary = useMemo(() => summarizeStatuses(rows), [rows]);
    const handleCancelJob = async (jobId) => {
        if (!jobCancelable(selectedJob?.status || '')) {
            showToast('This job cannot be cancelled.', 'error');
            return;
        }
        setIsCancelling(true);
        try {
            await cancelJob(jobId);
            showToast('Job cancelled successfully.', 'success');
            refresh();
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to cancel job.';
            showToast(message, 'error');
        }
        finally {
            setIsCancelling(false);
        }
    };
    return (_jsxs(EnterpriseLayout, { variant: "management", children: [_jsxs(ExecutiveOverview, { title: "\u2699\uFE0F Job Management", subtitle: "Monitor and manage provisioning and compliance jobs across your infrastructure", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total Jobs" }), _jsx("strong", { children: pagination.total }), _jsx("small", { className: "muted", children: "All time" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Running" }), _jsx("strong", { children: statusSummary.running || 0 }), _jsx("small", { className: "muted", children: "Active jobs" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Worker Status" }), _jsx("strong", { children: workerStatus?.started ? 'Running' : 'Stopped' }), _jsx("small", { className: "muted", children: "Job processor" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Success Rate" }), _jsxs("strong", { children: [pagination.total > 0 ? Math.round(((statusSummary.succeeded || 0) / pagination.total) * 100) : 0, "%"] }), _jsx("small", { className: "muted", children: "Last 30 days" })] })] }), _jsxs("div", { className: "management-dashboard", children: [_jsxs("div", { className: "management-main", children: [_jsx(ManagementPanel, { title: "Quick Actions", icon: "\u26A1", subtitle: "Common job operations", position: "primary", children: _jsxs(ActionZone, { alignment: "left", variant: "primary", children: [_jsx("button", { type: "button", className: "primary-button", onClick: () => window.location.href = '/templates', children: "\uD83D\uDE80 Create Job from Template" }), _jsx("button", { type: "button", className: "ghost-button", onClick: () => window.location.href = '/templates', children: "\uD83D\uDCCB Manage Templates" })] }) }), _jsx(ManagementPanel, { title: "\uD83D\uDCCB Job Registry", subtitle: "Monitor job execution and manage job lifecycle", position: "primary", children: loading ? (_jsx("p", { className: "muted", children: "Loading jobs\u2026" })) : rows.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No jobs found. Create a job template to get started." }) })) : (_jsx("div", { className: "job-list", children: rows.map((job) => (_jsxs("div", { className: "job-card", children: [_jsxs("header", { children: [_jsx("h3", { children: job.type }), _jsxs(ActionZone, { alignment: "right", variant: "secondary", children: [jobCancelable(job.status) && (_jsx("button", { type: "button", className: "danger-button", onClick: () => handleCancelJob(job.id), disabled: isCancelling, children: isCancelling ? 'Cancelling…' : 'Cancel' })), _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedJobId(job.id), disabled: isCancelling, children: "Details" })] })] }), _jsxs("dl", { children: [_jsx("dt", { children: "Status" }), _jsx("dd", { children: _jsx("span", { className: `status-pill status-${statusClass(job.status)}`, children: statusLabel(job.status) }) }), _jsx("dt", { children: "Tenant" }), _jsx("dd", { children: job.tenant_id || '—' }), _jsx("dt", { children: "Node" }), _jsx("dd", { children: "\u2014" }), _jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(job.created_at) }), _jsx("dt", { children: "Started" }), _jsx("dd", { children: formatDate(job.started_at) }), _jsx("dt", { children: "Completed" }), _jsx("dd", { children: formatDate(job.finished_at) })] }), job.payload != null && (_jsxs("div", { className: "job-result", children: [_jsx("h4", { children: "Payload" }), renderPayload(job.payload)] }))] }, job.id))) })) })] }), _jsxs("div", { className: "management-sidebar", children: [_jsx(ManagementPanel, { title: "Filters", icon: "\uD83D\uDD0D", subtitle: `${rows.length} jobs shown`, position: "secondary", children: _jsxs(ContentGrid, { columns: 1, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Tenant" }), _jsxs("select", { value: tenantFilter, onChange: (e) => setTenantFilter(e.target.value), children: [_jsx("option", { value: "", children: "All tenants" }), tenants?.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Status" }), _jsxs("select", { value: statusFilter, onChange: (e) => setStatusFilter(e.target.value), children: [_jsx("option", { value: "", children: "All statuses" }), STATUS_FILTERS.map((status) => (_jsx("option", { value: status, children: statusLabel(status) }, status)))] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Type" }), _jsx("input", { type: "text", value: typeFilter, onChange: (e) => setTypeFilter(e.target.value), placeholder: "e.g. provision" })] })] }) }), selectedJob && (_jsx(ManagementPanel, { title: `Job: ${selectedJob.type}`, icon: "\uD83D\uDCCA", subtitle: `Status: ${statusLabel(selectedJob.status)}`, position: "secondary", children: _jsxs("div", { className: "job-details", children: [_jsxs("dl", { children: [_jsx("dt", { children: "ID" }), _jsx("dd", { children: selectedJob.id }), _jsx("dt", { children: "Type" }), _jsx("dd", { children: selectedJob.type }), _jsx("dt", { children: "Status" }), _jsx("dd", { children: _jsx("span", { className: `status-pill status-${statusClass(selectedJob.status)}`, children: statusLabel(selectedJob.status) }) }), _jsx("dt", { children: "Tenant" }), _jsx("dd", { children: selectedJob.tenant_id || '—' }), _jsx("dt", { children: "Node" }), _jsx("dd", { children: "\u2014" }), _jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(selectedJob.created_at) }), _jsx("dt", { children: "Started" }), _jsx("dd", { children: formatDate(selectedJob.started_at) }), _jsx("dt", { children: "Completed" }), _jsx("dd", { children: formatDate(selectedJob.finished_at) }), selectedJob.payload != null && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Payload" }), _jsx("dd", { children: renderPayload(selectedJob.payload) })] })), selectedJob.events && selectedJob.events.length > 0 && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Events" }), _jsx("dd", { children: renderPayload(selectedJob.events) })] }))] }), _jsxs(ActionZone, { alignment: "right", variant: "primary", children: [jobCancelable(selectedJob.status) && (_jsx("button", { type: "button", className: "danger-button", onClick: () => handleCancelJob(selectedJob.id), disabled: isCancelling, children: isCancelling ? 'Cancelling…' : 'Cancel Job' })), _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedJobId(null), children: "Close" })] })] }) }))] })] })] }));
}
