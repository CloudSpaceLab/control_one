import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
const STATUS_FILTERS = ['queued', 'running', 'succeeded', 'failed', 'cancelled'];
const POLL_INTERVAL_MS = 6000;
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
export function Jobs() {
    const { data: tenants } = useTenants();
    const [tenantFilter, setTenantFilter] = useState('');
    const [statusFilter, setStatusFilter] = useState('');
    const [typeFilter, setTypeFilter] = useState('');
    const { data: jobs, loading, error } = useJobs({
        tenantId: tenantFilter || undefined,
        status: statusFilter || undefined,
        type: typeFilter || undefined,
        pollIntervalMs: POLL_INTERVAL_MS,
    });
    const tenantNames = useMemo(() => {
        const entries = new Map();
        for (const tenant of tenants) {
            entries.set(tenant.id, tenant.name);
        }
        return entries;
    }, [tenants]);
    const knownJobTypes = useMemo(() => {
        const unique = new Set();
        for (const job of jobs) {
            if (job.type) {
                unique.add(job.type);
            }
        }
        return Array.from(unique).sort((a, b) => a.localeCompare(b));
    }, [jobs]);
    const statusSummary = useMemo(() => summarizeStatuses(jobs), [jobs]);
    return (_jsxs("section", { children: [_jsx("h2", { children: "Jobs" }), _jsx("p", { children: "Background tasks across provisioning and compliance workflows." }), _jsxs("div", { className: "toolbar", children: [_jsx("label", { htmlFor: "tenant-filter", children: "Tenant" }), _jsxs("select", { id: "tenant-filter", value: tenantFilter, onChange: (event) => setTenantFilter(event.target.value), children: [_jsx("option", { value: "", children: "All tenants" }), tenants.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] }), _jsx("label", { htmlFor: "status-filter", children: "Status" }), _jsxs("select", { id: "status-filter", value: statusFilter, onChange: (event) => setStatusFilter(event.target.value), children: [_jsx("option", { value: "", children: "All statuses" }), STATUS_FILTERS.map((status) => (_jsx("option", { value: status, children: statusLabel(status) }, status)))] }), _jsx("label", { htmlFor: "job-type-filter", children: "Type" }), _jsx("input", { id: "job-type-filter", list: "job-types", placeholder: "All types", value: typeFilter, onChange: (event) => setTypeFilter(event.target.value) }), _jsx("datalist", { id: "job-types", children: knownJobTypes.map((jobType) => (_jsx("option", { value: jobType }, jobType))) })] }), _jsxs("p", { className: "muted", children: ["Auto-refreshing every ", POLL_INTERVAL_MS / 1000, "s."] }), !loading && !error && jobs.length === 0 ? (_jsx("p", { className: "muted", children: "No jobs have been queued yet." })) : null, error ? _jsxs("p", { className: "form-error", children: ["Failed to load jobs: ", error] }) : null, loading ? _jsx("p", { className: "muted", children: "Loading jobs\u2026" }) : null, !loading && jobs.length > 0 ? (_jsxs(_Fragment, { children: [_jsx("div", { className: "jobs-summary", children: STATUS_FILTERS.map((status) => {
                            const key = statusClass(status);
                            return (_jsxs("article", { className: "jobs-summary-card", children: [_jsx("span", { className: `status-pill status-${key}`, children: statusLabel(status) }), _jsx("strong", { children: statusSummary[key] ?? 0 }), _jsx("small", { className: "muted", children: "total" })] }, status));
                        }) }), _jsxs("table", { className: "jobs-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Type" }), _jsx("th", { children: "Tenant" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Retries" }), _jsx("th", { children: "Created" }), _jsx("th", { children: "Updated" })] }) }), _jsx("tbody", { children: jobs.map((job) => {
                                    const tenantName = job.tenant_id ? tenantNames.get(job.tenant_id) : undefined;
                                    const statusKey = statusClass(job.status);
                                    return (_jsxs("tr", { children: [_jsx("td", { children: job.id }), _jsx("td", { children: job.type }), _jsx("td", { children: tenantName ?? job.tenant_id ?? '—' }), _jsx("td", { children: _jsx("span", { className: `status-pill status-${statusKey}`, children: statusLabel(job.status) }) }), _jsxs("td", { children: [job.retries, "/", job.max_retries] }), _jsx("td", { children: formatDate(job.created_at) }), _jsx("td", { children: formatDate(job.updated_at) })] }, job.id));
                                }) })] })] })) : null] }));
}
