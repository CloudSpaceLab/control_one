import { FormEvent, useMemo, useState } from 'react';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
import type { Job, JobStatus } from '../lib/api';
import { useApiClient } from '../hooks/useApiClient';
import { useToast } from '../providers/ToastProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { useCancelJob } from '../hooks/useCancelJob';

const STATUS_FILTERS: JobStatus[] = ['queued', 'running', 'succeeded', 'failed', 'cancelled'];
const POLL_INTERVAL_MS = 6000;

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
  const [jobPayload, setJobPayload] = useState(`{
  "plan_id": "",
  "tenant_id": "",
  "node_id": "",
  "metadata": {}
}`);
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

  return (
    <section>
      <h2>Jobs</h2>
      <p>Background tasks across provisioning and compliance workflows.</p>

      <div className="worker-overview">
        <article className="worker-panel">
          <header>
            <div>
              <p className="muted">Worker backend</p>
              <h3>{workerStatus?.backend ? workerStatus.backend.toUpperCase() : '—'}</h3>
            </div>
            <span
              className={`status-dot ${
                workerStatus?.started ? 'status-online' : workerLoading ? 'status-pending' : 'status-offline'
              }`}
              aria-label={workerStatus?.started ? 'started' : workerLoading ? 'loading' : 'stopped'}
            />
          </header>
          <dl className="worker-metrics">
            <div>
              <dt>Queue depth</dt>
              <dd>{workerLoading ? '…' : workerStatus?.queue_depth ?? '—'}</dd>
            </div>
            <div>
              <dt>Active workers</dt>
              <dd>{workerLoading ? '…' : workerStatus?.active ?? '—'}</dd>
            </div>
            <div>
              <dt>Last error</dt>
              <dd>{workerLoading ? '…' : workerStatus?.last_error ?? '—'}</dd>
            </div>
          </dl>
          <div className="worker-actions">
            <button type="button" className="ghost-button" onClick={refreshWorker} disabled={workerLoading}>
              {workerLoading ? 'Refreshing…' : 'Refresh status'}
            </button>
            {workerError ? <span className="form-error">Status unavailable: {workerError}</span> : null}
          </div>
        </article>
        <article className="panel compact-summary">
          <h3>Job distribution</h3>
          <ul>
            {STATUS_FILTERS.map((status) => {
              const key = statusClass(status);
              return (
                <li key={status}>
                  <span className={`status-pill status-${key}`}>{statusLabel(status)}</span>
                  <strong>{statusSummary[key] ?? 0}</strong>
                </li>
              );
            })}
          </ul>
        </article>
      </div>

      <form className="panel" onSubmit={handleSubmitJob}>
        <h3>Submit job</h3>
        <label htmlFor="job-type">Job type</label>
        <input
          id="job-type"
          type="text"
          value={jobTypeInput}
          onChange={(event) => setJobTypeInput(event.target.value)}
          placeholder="provision.apply"
          disabled={submitting}
          required
        />

        <label htmlFor="job-tenant">Tenant</label>
        <select
          id="job-tenant"
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

        <label htmlFor="job-max-retries">Max retries</label>
        <input
          id="job-max-retries"
          type="number"
          min={0}
          value={maxRetries}
          onChange={(event) => setMaxRetries(event.target.value)}
          disabled={submitting}
        />

        <label htmlFor="job-payload">Payload (JSON)</label>
        <textarea
          id="job-payload"
          rows={6}
          value={jobPayload}
          onChange={(event) => setJobPayload(event.target.value)}
          disabled={submitting}
        />

        {submitError ? <p className="form-error">{submitError}</p> : null}
        {submitSuccess ? <p className="form-success">{submitSuccess}</p> : null}

        <button type="submit" disabled={submitting}>
          {submitting ? 'Submitting…' : 'Submit job'}
        </button>
      </form>

      <div className="toolbar">
        <label htmlFor="tenant-filter">Tenant</label>
        <select
          id="tenant-filter"
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

        <label htmlFor="status-filter">Status</label>
        <select
          id="status-filter"
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

        <label htmlFor="job-type-filter">Type</label>
        <input
          id="job-type-filter"
          list="job-types"
          placeholder="All types"
          value={typeFilter}
          onChange={(event) => {
            setTypeFilter(event.target.value);
            setOffset(0);
          }}
        />
        <datalist id="job-types">
          {knownJobTypes.map((jobType) => (
            <option key={jobType} value={jobType} />
          ))}
        </datalist>
      </div>

      <p className="muted">Auto-refreshing every {POLL_INTERVAL_MS / 1000}s.</p>

      {!loading && !error && jobs.length === 0 ? (
        <p className="muted">No jobs have been queued yet.</p>
      ) : null}
      {error ? <p className="form-error">Failed to load jobs: {error}</p> : null}
      {loading ? <p className="muted">Loading jobs&hellip;</p> : null}

      {!loading && jobs.length > 0 ? (
        <>
          <table className="jobs-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Type</th>
                <th>Tenant</th>
                <th>Status</th>
                <th>Retries</th>
                <th>Created</th>
                <th>Updated</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => {
                const tenantName = job.tenant_id ? tenantNames.get(job.tenant_id) : undefined;
                const statusKey = statusClass(job.status);
                const cancelable = jobCancelable(job.status);
                const isCancelling = cancellingJobId === job.id;
                return (
                  <tr key={job.id}>
                    <td>{job.id}</td>
                    <td>{job.type}</td>
                    <td>{tenantName ?? job.tenant_id ?? '—'}</td>
                    <td>
                      <span className={`status-pill status-${statusKey}`}>{statusLabel(job.status)}</span>
                    </td>
                    <td>
                      {job.retries}/{job.max_retries}
                    </td>
                    <td>{formatDate(job.created_at)}</td>
                    <td>{formatDate(job.updated_at)}</td>
                    <td>
                      <div className="job-row-actions">
                        <button type="button" onClick={() => openJobDetails(job.id)}>
                          View
                        </button>
                        {cancelable ? (
                          <button
                            type="button"
                            className="danger-button"
                            onClick={() => handleCancelJob(job.id)}
                            disabled={isCancelling}
                          >
                            {isCancelling ? 'Cancelling…' : 'Cancel'}
                          </button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <div className="pagination">
            <button
              type="button"
              disabled={!pagination.prevOffset && pagination.prevOffset !== 0}
              onClick={() => setOffset(pagination.prevOffset ?? 0)}
            >
              Previous
            </button>
            <span>
              Showing {jobs.length} of {pagination.total} jobs (page offset {pagination.offset})
            </span>
            <button
              type="button"
              disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
              onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
            >
              Next
            </button>
          </div>
        </>
      ) : null}
      {selectedJobId ? (
        <aside className="panel job-detail-panel">
          <h3>Job details</h3>
          {detailLoading ? <p className="muted">Loading details…</p> : null}
          {detailError ? <p className="form-error">{detailError}</p> : null}
          {jobDetail ? (
            <>
              <dl>
                <div>
                  <dt>Job ID</dt>
                  <dd>{jobDetail.id}</dd>
                </div>
                <div>
                  <dt>Type</dt>
                  <dd>{jobDetail.type}</dd>
                </div>
                <div>
                  <dt>Status</dt>
                  <dd>{statusLabel(jobDetail.status)}</dd>
                </div>
                <div>
                  <dt>Tenant</dt>
                  <dd>{jobDetail.tenant_id ? tenantNames.get(jobDetail.tenant_id) ?? jobDetail.tenant_id : '—'}</dd>
                </div>
                <div>
                  <dt>Retries</dt>
                  <dd>
                    {jobDetail.retries}/{jobDetail.max_retries}
                  </dd>
                </div>
              </dl>
              {jobDetail.payload ? (
                <details>
                  <summary>Payload</summary>
                  <pre>{JSON.stringify(jobDetail.payload, null, 2)}</pre>
                </details>
              ) : null}
              {jobDetail.events && jobDetail.events.length > 0 ? (
                <details open>
                  <summary>Events ({jobDetail.events.length})</summary>
                  <ul className="job-events-list">
                    {jobDetail.events.map((event) => (
                      <li key={event.id}>
                        <strong>{statusLabel(event.status)}</strong> – {formatDate(event.created_at)}
                        {event.message ? <div>{event.message}</div> : null}
                      </li>
                    ))}
                  </ul>
                </details>
              ) : (
                <p className="muted">No events recorded yet.</p>
              )}
              <div className="detail-actions">
                {jobCancelable(jobDetail.status) ? (
                  <button
                    type="button"
                    className="danger-button"
                    onClick={() => handleCancelJob(jobDetail.id)}
                    disabled={cancellingJobId === jobDetail.id}
                  >
                    {cancellingJobId === jobDetail.id ? 'Cancelling…' : 'Cancel job'}
                  </button>
                ) : null}
                <button type="button" className="ghost-button" onClick={() => setSelectedJobId(null)}>
                  Close
                </button>
              </div>
            </>
          ) : null}
        </aside>
      ) : null}
    </section>
  );
}
