import { FormEvent, useMemo, useState } from 'react';
import { SectionHeader, Panel, KpiTile, StatusTag, DataTable } from '../components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
import type { Job, JobStatus } from '../lib/api';
import { useApiClient } from '../hooks/useApiClient';
import { useToast } from '../providers/ToastProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { useCancelJob } from '../hooks/useCancelJob';
import type { ColumnDef } from '@tanstack/react-table';
import { Cpu } from 'lucide-react';
import type { StateTone } from '../components/kit';

const STATUS_FILTERS: JobStatus[] = ['queued', 'running', 'succeeded', 'failed', 'cancelled'];
const POLL_INTERVAL_MS = 6000;

interface JobFieldSpec {
  key: string;
  label: string;
  type: 'text' | 'number' | 'textarea';
  placeholder?: string;
  helper?: string;
  required?: boolean;
}

interface JobTypeSpec {
  type: string;
  label: string;
  description: string;
  fields: JobFieldSpec[];
}

// JOB_CATALOG describes the fields each well-known job type expects so
// operators don't have to hand-edit raw JSON. Custom job types fall back to
// the JSON editor automatically.
const JOB_CATALOG: JobTypeSpec[] = [
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

const JOB_SPECS: Record<string, JobTypeSpec> = JOB_CATALOG.reduce(
  (acc, j) => ({ ...acc, [j.type]: j }),
  {} as Record<string, JobTypeSpec>,
);

function defaultPayloadFields(jobType: string): Record<string, string> {
  const spec = JOB_SPECS[jobType];
  if (!spec) return {};
  const out: Record<string, string> = {};
  spec.fields.forEach((f) => {
    out[f.key] = '';
  });
  return out;
}

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function statusClass(status: string): string {
  return status.toLowerCase().replace(/[^a-z0-9]+/g, '-');
}

function statusLabel(status: string): string {
  return status.replace(/_/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function summarizeStatuses(jobs: Job[]): Record<string, number> {
  return jobs.reduce<Record<string, number>>((acc, job) => {
    const key = statusClass(job.status);
    acc[key] = (acc[key] ?? 0) + 1;
    return acc;
  }, {});
}

function jobCancelable(status: string): boolean {
  return ['queued', 'running'].includes(status.toLowerCase());
}

function jobStatusTone(status: string): StateTone {
  const s = status.toLowerCase();
  if (s === 'succeeded') return 'healthy';
  if (s === 'failed') return 'critical';
  if (s === 'running') return 'warning';
  if (s === 'queued') return 'info';
  if (s === 'cancelled') return 'unknown';
  return 'info';
}

export function Jobs(): JSX.Element {
  const api = useApiClient();
  const { showToast } = useToast();
  const { data: tenants } = useTenants();
  const [tenantFilter, setTenantFilter] = useState<string>('');
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [typeFilter, setTypeFilter] = useState<string>('');
  const [limit] = useState(20);
  const [offset, setOffset] = useState(0);
  const [jobTypeInput, setJobTypeInput] = useState('provision.apply');
  const [jobTenantId, setJobTenantId] = useState('');
  const [showRawPayload, setShowRawPayload] = useState(false);
  const [payloadFields, setPayloadFields] = useState<Record<string, string>>(
    defaultPayloadFields('provision.apply'),
  );
  const [jobPayload, setJobPayload] = useState<string>(
    JSON.stringify(defaultPayloadFields('provision.apply'), null, 2),
  );
  const [maxRetries, setMaxRetries] = useState('3');
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitSuccess, setSubmitSuccess] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [selectedJobId, setSelectedJobId] = useState<string | null>(null);
  const [jobDetail, setJobDetail] = useState<Job | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [cancellingJobId, setCancellingJobId] = useState<string | null>(null);

  const { data: jobs, loading, error, refresh, pagination } = useJobs({
    tenantId: tenantFilter || undefined,
    status: statusFilter || undefined,
    type: typeFilter || undefined,
    limit,
    offset,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  const { status: workerStatus, loading: workerLoading, error: workerError, refresh: refreshWorker } =
    useWorkerStatus({ pollIntervalMs: 8000 });
  const { cancelJob } = useCancelJob();

  const tenantNames = useMemo(() => {
    const entries = new Map<string, string>();
    for (const tenant of tenants) {
      entries.set(tenant.id, tenant.name);
    }
    return entries;
  }, [tenants]);

  const knownJobTypes = useMemo(() => {
    const unique = new Set<string>();
    for (const job of jobs) {
      if (job.type) {
        unique.add(job.type);
      }
    }
    return Array.from(unique).sort((a, b) => a.localeCompare(b));
  }, [jobs]);

  const statusSummary = useMemo(() => summarizeStatuses(jobs), [jobs]);

  const handleSubmitJob = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const type = jobTypeInput.trim();
    if (!type) {
      setSubmitError('Job type is required.');
      setSubmitSuccess(null);
      return;
    }

    let parsedPayload: unknown;
    if (jobPayload.trim()) {
      try {
        parsedPayload = JSON.parse(jobPayload);
      } catch (err) {
        setSubmitError(`Payload must be valid JSON: ${(err as Error).message}`);
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
    } catch (err) {
      if (err instanceof Error) {
        setSubmitError(err.message);
        showToast(err.message, 'error');
      } else {
        const fallback = 'Failed to submit job.';
        setSubmitError(fallback);
        showToast(fallback, 'error');
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleCancelJob = async (jobId: string) => {
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

  const openJobDetails = async (jobId: string) => {
    setSelectedJobId(jobId);
    setDetailError(null);
    setDetailLoading(true);
    try {
      const detail = await api.getJob(jobId);
      setJobDetail(detail);
    } catch (err) {
      if (err instanceof Error) {
        setDetailError(err.message);
      } else {
        setDetailError('Failed to load job details.');
      }
      setJobDetail(null);
    } finally {
      setDetailLoading(false);
    }
  };

  const jobColumns: ColumnDef<Job>[] = [
    {
      header: 'ID',
      accessorKey: 'id',
      cell: ({ row }) => (
        <code className="font-mono text-xs text-text-secondary">
          {row.original.id.slice(0, 8)}…
        </code>
      ),
    },
    {
      header: 'Type',
      accessorKey: 'type',
      cell: ({ row }) => <span className="text-foreground">{row.original.type}</span>,
    },
    {
      header: 'Tenant',
      accessorKey: 'tenant_id',
      cell: ({ row }) => (
        <span className="text-text-secondary">
          {row.original.tenant_id
            ? tenantNames.get(row.original.tenant_id) ?? row.original.tenant_id
            : '—'}
        </span>
      ),
    },
    {
      header: 'Status',
      accessorKey: 'status',
      cell: ({ row }) => (
        <StatusTag tone={jobStatusTone(row.original.status)}>
          {statusLabel(row.original.status)}
        </StatusTag>
      ),
    },
    {
      header: 'Retries',
      id: 'retries',
      cell: ({ row }) => (
        <span className="text-text-secondary">
          {row.original.retries}/{row.original.max_retries}
        </span>
      ),
    },
    {
      header: 'Created',
      accessorKey: 'created_at',
      cell: ({ row }) => (
        <span className="text-text-secondary">{formatDate(row.original.created_at)}</span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const cancelable = jobCancelable(row.original.status);
        const isCancelling = cancellingJobId === row.original.id;
        return (
          <div className="flex items-center gap-1.5">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => openJobDetails(row.original.id)}
            >
              View
            </Button>
            {cancelable ? (
              <Button
                type="button"
                variant="danger"
                size="sm"
                onClick={() => handleCancelJob(row.original.id)}
                disabled={isCancelling}
              >
                {isCancelling ? 'Cancelling…' : 'Cancel'}
              </Button>
            ) : null}
          </div>
        );
      },
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="AUTOMATION · JOBS"
        title="Jobs"
        description="Background tasks across provisioning and compliance workflows."
      />

      {/* Worker status + distribution */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Panel
          padding="md"
          eyebrow="WORKER"
          title="Worker backend"
          actions={
            <Button type="button" variant="secondary" size="sm" onClick={refreshWorker} disabled={workerLoading}>
              {workerLoading ? 'Refreshing…' : 'Refresh'}
            </Button>
          }
        >
          <div className="flex items-center gap-3">
            <div className={
              workerStatus?.started
                ? 'h-2.5 w-2.5 rounded-full bg-state-healthy'
                : workerLoading
                  ? 'h-2.5 w-2.5 rounded-full bg-state-warning'
                  : 'h-2.5 w-2.5 rounded-full bg-state-critical'
            } aria-label={workerStatus?.started ? 'started' : workerLoading ? 'loading' : 'stopped'} />
            <KpiTile
              label="Backend"
              value={workerStatus?.backend?.toUpperCase() ?? '—'}
              tone="brand"
              icon={<Cpu />}
              hint={`Queue: ${workerStatus?.queue_depth ?? '—'} · Active: ${workerStatus?.active ?? '—'}`}
              size="sm"
              className="flex-1"
            />
          </div>
          {workerError ? <span className="text-sm text-state-critical">Status unavailable: {workerError}</span> : null}
        </Panel>

        <Panel padding="md" eyebrow="DISTRIBUTION" title="Job counts">
          <div className="flex flex-wrap gap-2">
            {STATUS_FILTERS.map((status) => {
              const key = statusClass(status);
              return (
                <div key={status} className="flex items-center gap-1.5">
                  <StatusTag tone={jobStatusTone(status)}>{statusLabel(status)}</StatusTag>
                  <span className="font-mono text-sm font-semibold text-foreground">
                    {statusSummary[key] ?? 0}
                  </span>
                </div>
              );
            })}
          </div>
        </Panel>
      </div>

      {/* Submit job */}
      <Panel padding="md" eyebrow="SUBMIT JOB" title="Dispatch a job" toneAccent="brand">
        <form onSubmit={handleSubmitJob} className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="job-type">Job type</Label>
            <select
              id="job-type"
              className="h-9 w-full rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:cursor-not-allowed disabled:opacity-50"
              value={JOB_SPECS[jobTypeInput] ? jobTypeInput : '__custom__'}
              onChange={(event) => {
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
              }}
              disabled={submitting}
            >
              {JOB_CATALOG.map((spec) => (
                <option key={spec.type} value={spec.type}>
                  {spec.label} — {spec.type}
                </option>
              ))}
              <option value="__custom__">Custom (raw JSON)…</option>
            </select>
            {JOB_SPECS[jobTypeInput] && !showRawPayload ? (
              <p className="text-xs text-text-muted">{JOB_SPECS[jobTypeInput].description}</p>
            ) : null}
          </div>

          {showRawPayload ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="job-type-custom">Custom job type</Label>
              <Input
                id="job-type-custom"
                type="text"
                value={jobTypeInput}
                onChange={(event) => setJobTypeInput(event.target.value)}
                placeholder="my.custom.job"
                disabled={submitting}
                required
              />
            </div>
          ) : null}

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="job-tenant">Tenant</Label>
              <select
                id="job-tenant"
                className="h-9 w-full rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:cursor-not-allowed disabled:opacity-50"
                value={jobTenantId}
                onChange={(event) => setJobTenantId(event.target.value)}
                disabled={submitting}
              >
                <option value="">— Optional —</option>
                {tenants.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="job-max-retries">Max retries</Label>
              <Input
                id="job-max-retries"
                type="number"
                min={0}
                value={maxRetries}
                onChange={(event) => setMaxRetries(event.target.value)}
                disabled={submitting}
              />
            </div>
          </div>

          {!showRawPayload && JOB_SPECS[jobTypeInput] ? (
            <div className="rounded-md border border-border-subtle bg-surface p-4">
              <p className="mb-3 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Payload</p>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {JOB_SPECS[jobTypeInput].fields.map((spec) => (
                  <div
                    key={spec.key}
                    className={`flex flex-col gap-1.5 ${spec.type === 'textarea' ? 'sm:col-span-2' : ''}`}
                  >
                    <Label htmlFor={`payload-${spec.key}`}>
                      {spec.label}{spec.required ? ' *' : ''}
                    </Label>
                    {spec.type === 'textarea' ? (
                      <textarea
                        id={`payload-${spec.key}`}
                        className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                        rows={3}
                        value={payloadFields[spec.key] ?? ''}
                        onChange={(e) => {
                          const next = { ...payloadFields, [spec.key]: e.target.value };
                          setPayloadFields(next);
                          setJobPayload(JSON.stringify(next, null, 2));
                        }}
                        placeholder={spec.placeholder}
                        disabled={submitting}
                      />
                    ) : (
                      <Input
                        id={`payload-${spec.key}`}
                        type={spec.type}
                        value={payloadFields[spec.key] ?? ''}
                        onChange={(e) => {
                          const next = { ...payloadFields, [spec.key]: e.target.value };
                          setPayloadFields(next);
                          setJobPayload(JSON.stringify(next, null, 2));
                        }}
                        placeholder={spec.placeholder}
                        disabled={submitting}
                        required={spec.required}
                      />
                    )}
                    {spec.helper ? <p className="text-xs text-text-muted">{spec.helper}</p> : null}
                  </div>
                ))}
              </div>
              <div className="mt-3">
                <Button type="button" variant="secondary" size="sm" onClick={() => setShowRawPayload(true)}>
                  Edit raw JSON
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="job-payload">Payload (JSON)</Label>
              <textarea
                id="job-payload"
                className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                rows={6}
                value={jobPayload}
                onChange={(event) => setJobPayload(event.target.value)}
                disabled={submitting}
              />
              {JOB_SPECS[jobTypeInput] ? (
                <Button type="button" variant="secondary" size="sm" onClick={() => setShowRawPayload(false)}>
                  Use visual form
                </Button>
              ) : null}
            </div>
          )}

          {submitError ? <p className="text-sm text-state-critical" role="alert">{submitError}</p> : null}
          {submitSuccess ? <p className="text-sm text-state-healthy" role="status">{submitSuccess}</p> : null}

          <div className="flex items-center gap-2 pt-2">
            <Button type="submit" variant="primary" disabled={submitting}>
              {submitting ? 'Submitting…' : 'Submit job'}
            </Button>
          </div>
        </form>
      </Panel>

      {/* Jobs list */}
      <Panel
        padding="md"
        eyebrow="JOBS"
        title="Job queue"
      >
        {/* Filter row */}
        <div className="flex flex-wrap items-center gap-3">
          <select
            id="tenant-filter"
            aria-label="Filter by tenant"
            className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
            value={tenantFilter}
            onChange={(event) => {
              setTenantFilter(event.target.value);
              setOffset(0);
            }}
          >
            <option value="">All tenants</option>
            {tenants.map((tenant) => (
              <option key={tenant.id} value={tenant.id}>
                {tenant.name}
              </option>
            ))}
          </select>

          <select
            id="status-filter"
            aria-label="Filter by status"
            className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
            value={statusFilter}
            onChange={(event) => {
              setStatusFilter(event.target.value);
              setOffset(0);
            }}
          >
            <option value="">All statuses</option>
            {STATUS_FILTERS.map((status) => (
              <option key={status} value={status}>
                {statusLabel(status)}
              </option>
            ))}
          </select>

          <div className="relative flex-1">
            <Input
              id="job-type-filter"
              list="job-types"
              placeholder="All types"
              value={typeFilter}
              onChange={(event) => {
                setTypeFilter(event.target.value);
                setOffset(0);
              }}
              className="h-9"
            />
            <datalist id="job-types">
              {knownJobTypes.map((jobType) => (
                <option key={jobType} value={jobType} />
              ))}
            </datalist>
          </div>
        </div>

        <p className="text-xs text-text-muted">Auto-refreshing every {POLL_INTERVAL_MS / 1000}s.</p>

        {error ? <p className="text-sm text-state-critical" role="alert">Failed to load jobs: {error}</p> : null}

        <DataTable
          columns={jobColumns}
          rows={jobs}
          loading={loading}
          rowKey={(row) => row.id}
        />

        <div className="flex items-center justify-between gap-4 pt-2 text-sm text-text-muted">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={!pagination.prevOffset && pagination.prevOffset !== 0}
            onClick={() => setOffset(pagination.prevOffset ?? 0)}
          >
            ← Previous
          </Button>
          <span>
            Showing {jobs.length} of {pagination.total} jobs
          </span>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
            onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
          >
            Next →
          </Button>
        </div>
      </Panel>

      {/* Job detail aside */}
      {selectedJobId ? (
        <aside className="fixed inset-y-0 right-0 z-50 flex w-[min(560px,90vw)] flex-col gap-5 overflow-y-auto border-l border-border-subtle bg-elevated p-6 shadow-2xl">
          <header className="flex items-start justify-between gap-4">
            <div>
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">JOB</p>
              <h3 className="mt-0.5 font-display text-lg font-semibold text-foreground">
                {jobDetail?.type ?? 'Loading…'}
              </h3>
            </div>
            <Button variant="ghost" size="sm" onClick={() => setSelectedJobId(null)}>✕</Button>
          </header>

          <hr className="border-border-subtle" />

          {detailLoading ? <p className="text-text-muted">Loading details…</p> : null}
          {detailError ? <p className="text-sm text-state-critical" role="alert">{detailError}</p> : null}

          {jobDetail ? (
            <>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Job ID</dt>
                  <dd><code className="font-mono text-xs text-text-secondary">{jobDetail.id}</code></dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Type</dt>
                  <dd className="text-foreground">{jobDetail.type}</dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Status</dt>
                  <dd>
                    <StatusTag tone={jobStatusTone(jobDetail.status)}>
                      {statusLabel(jobDetail.status)}
                    </StatusTag>
                  </dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Tenant</dt>
                  <dd className="text-foreground">
                    {jobDetail.tenant_id
                      ? tenantNames.get(jobDetail.tenant_id) ?? jobDetail.tenant_id
                      : '—'}
                  </dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Retries</dt>
                  <dd className="text-foreground">{jobDetail.retries}/{jobDetail.max_retries}</dd>
                </div>
              </dl>

              {jobDetail.payload ? (
                <>
                  <hr className="border-border-subtle" />
                  <details>
                    <summary className="cursor-pointer font-mono text-[0.65rem] uppercase tracking-wider text-text-muted select-none">
                      Payload
                    </summary>
                    <pre className="mt-2 rounded-md border border-border-subtle bg-surface p-3 font-mono text-xs text-foreground overflow-x-auto whitespace-pre-wrap">
                      {JSON.stringify(jobDetail.payload, null, 2)}
                    </pre>
                  </details>
                </>
              ) : null}

              {jobDetail.events && jobDetail.events.length > 0 ? (
                <>
                  <hr className="border-border-subtle" />
                  <div className="flex flex-col gap-2">
                    <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Events ({jobDetail.events.length})
                    </p>
                    <ul className="flex flex-col gap-2">
                      {jobDetail.events.map((event) => (
                        <li key={event.id} className="rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm">
                          <div className="flex items-center gap-2">
                            <StatusTag tone={jobStatusTone(event.status)}>
                              {statusLabel(event.status)}
                            </StatusTag>
                            <span className="text-xs text-text-muted">{formatDate(event.created_at)}</span>
                          </div>
                          {event.message ? (
                            <p className="mt-1 text-xs text-text-secondary">{event.message}</p>
                          ) : null}
                        </li>
                      ))}
                    </ul>
                  </div>
                </>
              ) : (
                <p className="text-sm text-text-muted">No events recorded yet.</p>
              )}

              <hr className="border-border-subtle" />

              <div className="flex items-center gap-2">
                {jobCancelable(jobDetail.status) ? (
                  <Button
                    type="button"
                    variant="danger"
                    onClick={() => handleCancelJob(jobDetail.id)}
                    disabled={cancellingJobId === jobDetail.id}
                  >
                    {cancellingJobId === jobDetail.id ? 'Cancelling…' : 'Cancel job'}
                  </Button>
                ) : null}
                <Button type="button" variant="ghost" onClick={() => setSelectedJobId(null)}>
                  Close
                </Button>
              </div>
            </>
          ) : null}
        </aside>
      ) : null}
    </div>
  );
}
