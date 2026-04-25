import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useToast } from '../providers/ToastProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { useCancelJob } from '../hooks/useCancelJob';
const STATUS_FILTERS = ['queued', 'running', 'succeeded', 'failed', 'cancelled'];
const POLL_INTERVAL_MS = 6000;
// JOB_CATALOG describes the fields each well-known job type expects so
// operators don't have to hand-edit raw JSON. Custom job types fall back to
// the JSON editor automatically.
const JOB_CATALOG = [
    {
        type: 'provision.apply',
        label: 'Apply provisioning template',
        description: 'Render a template version against a target node.',
        fields: [
            { key: 'plan_id', label: 'Template ID', type: 'text', required: true,
                placeholder: 'uuid of the provisioning template' },
            { key: 'node_id', label: 'Target node ID', type: 'text', required: true },
            { key: 'template_version', label: 'Template version (optional)', type: 'number',
                placeholder: 'leaves blank → current promoted version' },
        ],
    },
    {
        type: 'compliance.scan',
        label: 'Run compliance scan',
        description: 'Trigger a fresh policy scan across one or more nodes.',
        fields: [
            { key: 'node_id', label: 'Node ID (blank = all nodes in tenant)', type: 'text' },
            { key: 'rule_set', label: 'Rule set (optional)', type: 'text',
                placeholder: 'e.g. cis-level-1' },
        ],
    },
    {
        type: 'cluster.provision',
        label: 'Provision cluster',
        description: 'Spin up a new cluster from its role plan.',
        fields: [
            { key: 'cluster_id', label: 'Cluster ID', type: 'text', required: true },
        ],
    },
    {
        type: 'cluster.scale',
        label: 'Scale cluster',
        description: 'Adjust replica counts on an existing cluster.',
        fields: [
            { key: 'cluster_id', label: 'Cluster ID', type: 'text', required: true },
            { key: 'role', label: 'Role to scale', type: 'text', required: true,
                placeholder: 'e.g. worker' },
            { key: 'count', label: 'Target count', type: 'number', required: true },
        ],
    },
];
const JOB_SPECS = JOB_CATALOG.reduce((acc, j) => ({ ...acc, [j.type]: j }), {});
function defaultPayloadFields(jobType) {
    const spec = JOB_SPECS[jobType];
    if (!spec)
        return {};
    const out = {};
    spec.fields.forEach((f) => {
        out[f.key] = '';
    });
    return out;
}
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
    const api = useApiClient();
    const { showToast } = useToast();
    const { data: tenants } = useTenants();
    const [tenantFilter, setTenantFilter] = useState('');
    const [statusFilter, setStatusFilter] = useState('');
    const [typeFilter, setTypeFilter] = useState('');
    const [limit] = useState(20);
    const [offset, setOffset] = useState(0);
    const [jobTypeInput, setJobTypeInput] = useState('provision.apply');
    const [jobTenantId, setJobTenantId] = useState('');
    const [showRawPayload, setShowRawPayload] = useState(false);
    const [payloadFields, setPayloadFields] = useState(defaultPayloadFields('provision.apply'));
    const [jobPayload, setJobPayload] = useState(JSON.stringify(defaultPayloadFields('provision.apply'), null, 2));
    const [maxRetries, setMaxRetries] = useState('3');
    const [submitError, setSubmitError] = useState(null);
    const [submitSuccess, setSubmitSuccess] = useState(null);
    const [submitting, setSubmitting] = useState(false);
    const [selectedJobId, setSelectedJobId] = useState(null);
    const [jobDetail, setJobDetail] = useState(null);
    const [detailError, setDetailError] = useState(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [cancellingJobId, setCancellingJobId] = useState(null);
    const { data: jobs, loading, error, refresh, pagination } = useJobs({
        tenantId: tenantFilter || undefined,
        status: statusFilter || undefined,
        type: typeFilter || undefined,
        limit,
        offset,
        pollIntervalMs: POLL_INTERVAL_MS,
    });
    const { status: workerStatus, loading: workerLoading, error: workerError, refresh: refreshWorker } = useWorkerStatus({ pollIntervalMs: 8000 });
    const { cancelJob } = useCancelJob();
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
    const handleSubmitJob = async (event) => {
        event.preventDefault();
        const type = jobTypeInput.trim();
        if (!type) {
            setSubmitError('Job type is required.');
            setSubmitSuccess(null);
            return;
        }
        let parsedPayload;
        if (jobPayload.trim()) {
            try {
                parsedPayload = JSON.parse(jobPayload);
            }
            catch (err) {
                setSubmitError(`Payload must be valid JSON: ${err.message}`);
                setSubmitSuccess(null);
                return;
            }
        }
        const retriesValue = Number(maxRetries);
        if (maxRetries.trim() && (Number.isNaN(retriesValue) || retriesValue < 0)) {
            setSubmitError('Max retries must be zero or a positive number.');
            setSubmitSuccess(null);
            return;
        }
        setSubmitting(true);
        setSubmitError(null);
        setSubmitSuccess(null);
        try {
            await api.createJob({
                type,
                tenant_id: jobTenantId || undefined,
                payload: parsedPayload,
                max_retries: maxRetries.trim() ? retriesValue : undefined,
            });
            const successMessage = 'Job submitted successfully.';
            setSubmitSuccess(successMessage);
            showToast(successMessage, 'success');
            setJobPayload(`{
  "plan_id": "",
  "tenant_id": "",
  "node_id": "",
  "metadata": {}
}`);
            setJobTenantId('');
            setMaxRetries('3');
            refresh();
        }
        catch (err) {
            if (err instanceof Error) {
                setSubmitError(err.message);
                showToast(err.message, 'error');
            }
            else {
                const fallback = 'Failed to submit job.';
                setSubmitError(fallback);
                showToast(fallback, 'error');
            }
        }
        finally {
            setSubmitting(false);
        }
    };
    const handleCancelJob = async (jobId) => {
        setCancellingJobId(jobId);
        const result = await cancelJob(jobId);
        setCancellingJobId(null);
        if (result) {
            refresh();
            if (selectedJobId === jobId) {
                setJobDetail(result);
            }
        }
    };
    const openJobDetails = async (jobId) => {
        setSelectedJobId(jobId);
        setDetailError(null);
        setDetailLoading(true);
        try {
            const detail = await api.getJob(jobId);
            setJobDetail(detail);
        }
        catch (err) {
            if (err instanceof Error) {
                setDetailError(err.message);
            }
            else {
                setDetailError('Failed to load job details.');
            }
            setJobDetail(null);
        }
        finally {
            setDetailLoading(false);
        }
    };
    return (_jsxs("section", { children: [_jsx("h2", { children: "Jobs" }), _jsx("p", { children: "Background tasks across provisioning and compliance workflows." }), _jsxs("div", { className: "worker-overview", children: [_jsxs("article", { className: "worker-panel", children: [_jsxs("header", { children: [_jsxs("div", { children: [_jsx("p", { className: "muted", children: "Worker backend" }), _jsx("h3", { children: workerStatus?.backend ? workerStatus.backend.toUpperCase() : '—' })] }), _jsx("span", { className: `status-dot ${workerStatus?.started ? 'status-online' : workerLoading ? 'status-pending' : 'status-offline'}`, "aria-label": workerStatus?.started ? 'started' : workerLoading ? 'loading' : 'stopped' })] }), _jsxs("dl", { className: "worker-metrics", children: [_jsxs("div", { children: [_jsx("dt", { children: "Queue depth" }), _jsx("dd", { children: workerLoading ? '…' : workerStatus?.queue_depth ?? '—' })] }), _jsxs("div", { children: [_jsx("dt", { children: "Active workers" }), _jsx("dd", { children: workerLoading ? '…' : workerStatus?.active ?? '—' })] }), _jsxs("div", { children: [_jsx("dt", { children: "Last error" }), _jsx("dd", { children: workerLoading ? '…' : workerStatus?.last_error ?? '—' })] })] }), _jsxs("div", { className: "worker-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: refreshWorker, disabled: workerLoading, children: workerLoading ? 'Refreshing…' : 'Refresh status' }), workerError ? _jsxs("span", { className: "form-error", children: ["Status unavailable: ", workerError] }) : null] })] }), _jsxs("article", { className: "panel compact-summary", children: [_jsx("h3", { children: "Job distribution" }), _jsx("ul", { children: STATUS_FILTERS.map((status) => {
                                    const key = statusClass(status);
                                    return (_jsxs("li", { children: [_jsx("span", { className: `status-pill status-${key}`, children: statusLabel(status) }), _jsx("strong", { children: statusSummary[key] ?? 0 })] }, status));
                                }) })] })] }), _jsxs("form", { className: "panel", onSubmit: handleSubmitJob, children: [_jsx("h3", { children: "Submit job" }), _jsx("label", { htmlFor: "job-type", children: "Job type" }), _jsxs("select", { id: "job-type", value: JOB_SPECS[jobTypeInput] ? jobTypeInput : '__custom__', onChange: (event) => {
                            const next = event.target.value;
                            if (next === '__custom__') {
                                setShowRawPayload(true);
                                return;
                            }
                            setJobTypeInput(next);
                            const fields = defaultPayloadFields(next);
                            setPayloadFields(fields);
                            setJobPayload(JSON.stringify(fields, null, 2));
                            setShowRawPayload(false);
                        }, disabled: submitting, children: [JOB_CATALOG.map((spec) => (_jsxs("option", { value: spec.type, children: [spec.label, " \u2014 ", spec.type] }, spec.type))), _jsx("option", { value: "__custom__", children: "Custom (raw JSON)\u2026" })] }), JOB_SPECS[jobTypeInput] && !showRawPayload ? (_jsx("p", { className: "muted", style: { marginTop: '0.25rem' }, children: JOB_SPECS[jobTypeInput].description })) : null, showRawPayload ? (_jsxs(_Fragment, { children: [_jsx("label", { htmlFor: "job-type-custom", children: "Custom job type" }), _jsx("input", { id: "job-type-custom", type: "text", value: jobTypeInput, onChange: (event) => setJobTypeInput(event.target.value), placeholder: "my.custom.job", disabled: submitting, required: true })] })) : null, _jsx("label", { htmlFor: "job-tenant", children: "Tenant" }), _jsxs("select", { id: "job-tenant", value: jobTenantId, onChange: (event) => setJobTenantId(event.target.value), disabled: submitting, children: [_jsx("option", { value: "", children: "\u2014 Optional \u2014" }), tenants.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] }), _jsx("label", { htmlFor: "job-max-retries", children: "Max retries" }), _jsx("input", { id: "job-max-retries", type: "number", min: 0, value: maxRetries, onChange: (event) => setMaxRetries(event.target.value), disabled: submitting }), !showRawPayload && JOB_SPECS[jobTypeInput] ? (_jsxs("fieldset", { style: { border: '1px solid rgba(255,255,255,0.08)', borderRadius: 8, padding: '0.75rem', marginTop: '0.5rem' }, children: [_jsx("legend", { style: { padding: '0 0.4rem' }, children: "Payload" }), _jsx("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '0.6rem' }, children: JOB_SPECS[jobTypeInput].fields.map((spec) => (_jsxs("label", { style: spec.type === 'textarea' ? { gridColumn: '1 / -1' } : undefined, children: [spec.label, spec.required ? ' *' : '', spec.type === 'textarea' ? (_jsx("textarea", { rows: 3, value: payloadFields[spec.key] ?? '', onChange: (e) => {
                                                const next = { ...payloadFields, [spec.key]: e.target.value };
                                                setPayloadFields(next);
                                                setJobPayload(JSON.stringify(next, null, 2));
                                            }, placeholder: spec.placeholder, disabled: submitting })) : (_jsx("input", { type: spec.type, value: payloadFields[spec.key] ?? '', onChange: (e) => {
                                                const next = { ...payloadFields, [spec.key]: e.target.value };
                                                setPayloadFields(next);
                                                setJobPayload(JSON.stringify(next, null, 2));
                                            }, placeholder: spec.placeholder, disabled: submitting, required: spec.required })), spec.helper ? _jsx("small", { className: "muted", children: spec.helper }) : null] }, spec.key))) }), _jsx("button", { type: "button", className: "secondary-button", style: { marginTop: '0.5rem' }, onClick: () => setShowRawPayload(true), children: "Edit raw JSON" })] })) : (_jsxs(_Fragment, { children: [_jsx("label", { htmlFor: "job-payload", children: "Payload (JSON)" }), _jsx("textarea", { id: "job-payload", rows: 6, value: jobPayload, onChange: (event) => setJobPayload(event.target.value), disabled: submitting }), JOB_SPECS[jobTypeInput] ? (_jsx("button", { type: "button", className: "secondary-button", onClick: () => setShowRawPayload(false), children: "Use visual form" })) : null] })), submitError ? _jsx("p", { className: "form-error", children: submitError }) : null, submitSuccess ? _jsx("p", { className: "form-success", children: submitSuccess }) : null, _jsx("button", { type: "submit", disabled: submitting, children: submitting ? 'Submitting…' : 'Submit job' })] }), _jsxs("div", { className: "toolbar", children: [_jsx("label", { htmlFor: "tenant-filter", children: "Tenant" }), _jsxs("select", { id: "tenant-filter", value: tenantFilter, onChange: (event) => {
                            setTenantFilter(event.target.value);
                            setOffset(0);
                        }, children: [_jsx("option", { value: "", children: "All tenants" }), tenants.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] }), _jsx("label", { htmlFor: "status-filter", children: "Status" }), _jsxs("select", { id: "status-filter", value: statusFilter, onChange: (event) => {
                            setStatusFilter(event.target.value);
                            setOffset(0);
                        }, children: [_jsx("option", { value: "", children: "All statuses" }), STATUS_FILTERS.map((status) => (_jsx("option", { value: status, children: statusLabel(status) }, status)))] }), _jsx("label", { htmlFor: "job-type-filter", children: "Type" }), _jsx("input", { id: "job-type-filter", list: "job-types", placeholder: "All types", value: typeFilter, onChange: (event) => {
                            setTypeFilter(event.target.value);
                            setOffset(0);
                        } }), _jsx("datalist", { id: "job-types", children: knownJobTypes.map((jobType) => (_jsx("option", { value: jobType }, jobType))) })] }), _jsxs("p", { className: "muted", children: ["Auto-refreshing every ", POLL_INTERVAL_MS / 1000, "s."] }), !loading && !error && jobs.length === 0 ? (_jsx("p", { className: "muted", children: "No jobs have been queued yet." })) : null, error ? _jsxs("p", { className: "form-error", children: ["Failed to load jobs: ", error] }) : null, loading ? _jsx("p", { className: "muted", children: "Loading jobs\u2026" }) : null, !loading && jobs.length > 0 ? (_jsxs(_Fragment, { children: [_jsxs("table", { className: "jobs-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Type" }), _jsx("th", { children: "Tenant" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Retries" }), _jsx("th", { children: "Created" }), _jsx("th", { children: "Updated" }), _jsx("th", {})] }) }), _jsx("tbody", { children: jobs.map((job) => {
                                    const tenantName = job.tenant_id ? tenantNames.get(job.tenant_id) : undefined;
                                    const statusKey = statusClass(job.status);
                                    const cancelable = jobCancelable(job.status);
                                    const isCancelling = cancellingJobId === job.id;
                                    return (_jsxs("tr", { children: [_jsx("td", { children: job.id }), _jsx("td", { children: job.type }), _jsx("td", { children: tenantName ?? job.tenant_id ?? '—' }), _jsx("td", { children: _jsx("span", { className: `status-pill status-${statusKey}`, children: statusLabel(job.status) }) }), _jsxs("td", { children: [job.retries, "/", job.max_retries] }), _jsx("td", { children: formatDate(job.created_at) }), _jsx("td", { children: formatDate(job.updated_at) }), _jsx("td", { children: _jsxs("div", { className: "job-row-actions", children: [_jsx("button", { type: "button", onClick: () => openJobDetails(job.id), children: "View" }), cancelable ? (_jsx("button", { type: "button", className: "danger-button", onClick: () => handleCancelJob(job.id), disabled: isCancelling, children: isCancelling ? 'Cancelling…' : 'Cancel' })) : null] }) })] }, job.id));
                                }) })] }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", disabled: !pagination.prevOffset && pagination.prevOffset !== 0, onClick: () => setOffset(pagination.prevOffset ?? 0), children: "Previous" }), _jsxs("span", { children: ["Showing ", jobs.length, " of ", pagination.total, " jobs (page offset ", pagination.offset, ")"] }), _jsx("button", { type: "button", disabled: pagination.nextOffset === null || pagination.nextOffset === undefined, onClick: () => setOffset(pagination.nextOffset ?? offset + limit), children: "Next" })] })] })) : null, selectedJobId ? (_jsxs("aside", { className: "panel job-detail-panel", children: [_jsx("h3", { children: "Job details" }), detailLoading ? _jsx("p", { className: "muted", children: "Loading details\u2026" }) : null, detailError ? _jsx("p", { className: "form-error", children: detailError }) : null, jobDetail ? (_jsxs(_Fragment, { children: [_jsxs("dl", { children: [_jsxs("div", { children: [_jsx("dt", { children: "Job ID" }), _jsx("dd", { children: jobDetail.id })] }), _jsxs("div", { children: [_jsx("dt", { children: "Type" }), _jsx("dd", { children: jobDetail.type })] }), _jsxs("div", { children: [_jsx("dt", { children: "Status" }), _jsx("dd", { children: statusLabel(jobDetail.status) })] }), _jsxs("div", { children: [_jsx("dt", { children: "Tenant" }), _jsx("dd", { children: jobDetail.tenant_id ? tenantNames.get(jobDetail.tenant_id) ?? jobDetail.tenant_id : '—' })] }), _jsxs("div", { children: [_jsx("dt", { children: "Retries" }), _jsxs("dd", { children: [jobDetail.retries, "/", jobDetail.max_retries] })] })] }), jobDetail.payload ? (_jsxs("details", { children: [_jsx("summary", { children: "Payload" }), _jsx("pre", { children: JSON.stringify(jobDetail.payload, null, 2) })] })) : null, jobDetail.events && jobDetail.events.length > 0 ? (_jsxs("details", { open: true, children: [_jsxs("summary", { children: ["Events (", jobDetail.events.length, ")"] }), _jsx("ul", { className: "job-events-list", children: jobDetail.events.map((event) => (_jsxs("li", { children: [_jsx("strong", { children: statusLabel(event.status) }), " \u2013 ", formatDate(event.created_at), event.message ? _jsx("div", { children: event.message }) : null] }, event.id))) })] })) : (_jsx("p", { className: "muted", children: "No events recorded yet." })), _jsxs("div", { className: "detail-actions", children: [jobCancelable(jobDetail.status) ? (_jsx("button", { type: "button", className: "danger-button", onClick: () => handleCancelJob(jobDetail.id), disabled: cancellingJobId === jobDetail.id, children: cancellingJobId === jobDetail.id ? 'Cancelling…' : 'Cancel job' })) : null, _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedJobId(null), children: "Close" })] })] })) : null] })) : null] }));
}
