import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { useCancelJob } from '../hooks/useCancelJob';
import { DemandForm } from '../components/DemandForm';
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
    return (_jsxs("div", { className: "focused-content", children: [_jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\u2699\uFE0F Job Management" }), _jsx("p", { className: "focused-section-subtitle", children: "Monitor and manage provisioning and compliance jobs across your infrastructure." })] }), _jsx("div", { className: "focused-section-content", children: _jsxs("div", { className: "stat-grid", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total Jobs" }), _jsx("strong", { children: pagination.total }), _jsx("small", { className: "muted", children: "All time" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Running" }), _jsx("strong", { children: statusSummary.running || 0 }), _jsx("small", { className: "muted", children: "Active jobs" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Worker Status" }), _jsx("strong", { children: workerStatus?.started ? 'Running' : 'Stopped' }), _jsx("small", { className: "muted", children: "Job processor" })] })] }) })] }), _jsx(DemandForm, { title: "Quick Actions", icon: "\u26A1", summary: "Common job operations", children: _jsxs("div", { className: "job-actions", children: [_jsx("button", { type: "button", className: "primary-button", onClick: () => window.location.href = '/templates', children: "Create Job from Template" }), _jsx("button", { type: "button", className: "ghost-button", onClick: () => window.location.href = '/templates', children: "Manage Templates" })] }) }), _jsx(DemandForm, { title: "Filters", icon: "\uD83D\uDD0D", summary: `${rows.length} jobs shown`, children: _jsxs("div", { className: "compact-form", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Tenant" }), _jsxs("select", { value: tenantFilter, onChange: (e) => setTenantFilter(e.target.value), children: [_jsx("option", { value: "", children: "All tenants" }), tenants?.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Status" }), _jsxs("select", { value: statusFilter, onChange: (e) => setStatusFilter(e.target.value), children: [_jsx("option", { value: "", children: "All statuses" }), STATUS_FILTERS.map((status) => (_jsx("option", { value: status, children: statusLabel(status) }, status)))] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Type" }), _jsx("input", { type: "text", value: typeFilter, onChange: (e) => setTypeFilter(e.target.value), placeholder: "e.g. provision" })] })] }) }), _jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDCCB Job Registry" }), _jsx("p", { className: "focused-section-subtitle", children: "Monitor job execution and manage job lifecycle" })] }), _jsx("div", { className: "focused-section-content", children: loading ? (_jsx("p", { className: "muted", children: "Loading jobs\u2026" })) : rows.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No jobs found. Create a job template to get started." }) })) : (_jsx("div", { className: "job-list", children: rows.map((job) => (_jsxs("div", { className: "job-card", children: [_jsxs("header", { children: [_jsx("h3", { children: job.type }), _jsxs("div", { className: "job-actions", children: [jobCancelable(job.status) && (_jsx("button", { type: "button", className: "danger-button", onClick: () => handleCancelJob(job.id), disabled: isCancelling, children: isCancelling ? 'Cancelling…' : 'Cancel' })), _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedJobId(job.id), disabled: isCancelling, children: "Details" })] })] }), _jsxs("dl", { children: [_jsx("dt", { children: "Status" }), _jsx("dd", { children: _jsx("span", { className: `status-pill status-${statusClass(job.status)}`, children: statusLabel(job.status) }) }), _jsx("dt", { children: "Tenant" }), _jsx("dd", { children: job.tenant_id || '—' }), _jsx("dt", { children: "Node" }), _jsx("dd", { children: "\u2014" }), _jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(job.created_at) }), _jsx("dt", { children: "Started" }), _jsx("dd", { children: formatDate(job.started_at) }), _jsx("dt", { children: "Completed" }), _jsx("dd", { children: formatDate(job.finished_at) })] }), job.payload && (_jsxs("div", { className: "job-result", children: [_jsx("dt", { children: "Payload" }), _jsx("dd", { children: _jsx("pre", { children: JSON.stringify(job.payload, null, 2) }) })] }))] }, job.id))) })) })] }), selectedJob && (_jsx(DemandForm, { title: `Job: ${selectedJob.type}`, icon: "\uD83D\uDCCA", summary: `Status: ${statusLabel(selectedJob.status)}`, defaultExpanded: true, children: _jsxs("div", { className: "job-details", children: [_jsxs("dl", { children: [_jsx("dt", { children: "ID" }), _jsx("dd", { children: selectedJob.id }), _jsx("dt", { children: "Type" }), _jsx("dd", { children: selectedJob.type }), _jsx("dt", { children: "Status" }), _jsx("dd", { children: _jsx("span", { className: `status-pill status-${statusClass(selectedJob.status)}`, children: statusLabel(selectedJob.status) }) }), _jsx("dt", { children: "Tenant" }), _jsx("dd", { children: selectedJob.tenant_id || '—' }), _jsx("dt", { children: "Node" }), _jsx("dd", { children: "\u2014" }), _jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(selectedJob.created_at) }), _jsx("dt", { children: "Started" }), _jsx("dd", { children: formatDate(selectedJob.started_at) }), _jsx("dt", { children: "Completed" }), _jsx("dd", { children: formatDate(selectedJob.finished_at) }), selectedJob.payload && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Payload" }), _jsx("dd", { children: _jsx("pre", { children: JSON.stringify(selectedJob.payload, null, 2) }) })] })), selectedJob.events && selectedJob.events.length > 0 && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Events" }), _jsx("dd", { children: _jsx("pre", { children: String(JSON.stringify(selectedJob.events, null, 2)) }) })] }))] }), _jsxs("div", { className: "job-actions", children: [jobCancelable(selectedJob.status) && (_jsx("button", { type: "button", className: "danger-button", onClick: () => handleCancelJob(selectedJob.id), disabled: isCancelling, children: isCancelling ? 'Cancelling…' : 'Cancel Job' })), _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedJobId(null), children: "Close" })] })] }) }))] }));
}
