import { useMemo, useState } from 'react';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
import type { Job, JobStatus } from '../lib/api';

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

export function Jobs(): JSX.Element {
  const { data: tenants } = useTenants();
  const [tenantFilter, setTenantFilter] = useState<string>('');
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [typeFilter, setTypeFilter] = useState<string>('');

  const { data: jobs, loading, error } = useJobs({
    tenantId: tenantFilter || undefined,
    status: statusFilter || undefined,
    type: typeFilter || undefined,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

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

  return (
    <section>
      <h2>Jobs</h2>
      <p>Background tasks across provisioning and compliance workflows.</p>

      <div className="toolbar">
        <label htmlFor="tenant-filter">Tenant</label>
        <select
          id="tenant-filter"
          value={tenantFilter}
          onChange={(event) => setTenantFilter(event.target.value)}
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
          onChange={(event) => setStatusFilter(event.target.value)}
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
          onChange={(event) => setTypeFilter(event.target.value)}
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
          <div className="jobs-summary">
            {STATUS_FILTERS.map((status) => {
              const key = statusClass(status);
              return (
                <article key={status} className="jobs-summary-card">
                  <span className={`status-pill status-${key}`}>{statusLabel(status)}</span>
                  <strong>{statusSummary[key] ?? 0}</strong>
                  <small className="muted">total</small>
                </article>
              );
            })}
          </div>

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
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => {
                const tenantName = job.tenant_id ? tenantNames.get(job.tenant_id) : undefined;
                const statusKey = statusClass(job.status);
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
                  </tr>
                );
              })}
            </tbody>
          </table>
        </>
      ) : null}
    </section>
  );
}
