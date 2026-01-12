import { useMemo, useState } from 'react';
import { useJobs } from '../hooks/useJobs';
import { useTenants } from '../hooks/useTenants';
import type { Job, JobStatus } from '../lib/api';
import { useToast } from '../providers/ToastProvider';
import { useWorkerStatus } from '../hooks/useWorkerStatus';
import { useCancelJob } from '../hooks/useCancelJob';
import { 
  EnterpriseLayout, 
  ExecutiveOverview, 
  ManagementPanel, 
  ActionZone,
  ContentGrid 
} from '../components/EnterpriseLayout';
import '../components/EnterpriseLayout.css';

const STATUS_FILTERS: JobStatus[] = ['queued', 'running', 'succeeded', 'failed', 'cancelled'];

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

function safeStringify(data: unknown): string {
  if (data === null || data === undefined) {
    return '';
  }
  try {
    return JSON.stringify(data, null, 2);
  } catch {
    return String(data);
  }
}

function renderPayload(payload: unknown): JSX.Element {
  return <pre>{safeStringify(payload)}</pre>;
}

export function Jobs(): JSX.Element {
  const { showToast } = useToast();
  const { data: tenants } = useTenants();
  const [tenantFilter, setTenantFilter] = useState<string>('');
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [typeFilter, setTypeFilter] = useState<string>('');
  const [limit] = useState(20);
  const [selectedJobId, setSelectedJobId] = useState<string | null>(null);
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
  const selectedJob = useMemo(
    () => rows.find((job) => job.id === selectedJobId) ?? null,
    [rows, selectedJobId],
  );

  const statusSummary = useMemo(() => summarizeStatuses(rows), [rows]);

  const handleCancelJob = async (jobId: string) => {
    if (!jobCancelable(selectedJob?.status || '')) {
      showToast('This job cannot be cancelled.', 'error');
      return;
    }

    setIsCancelling(true);
    try {
      await cancelJob(jobId);
      showToast('Job cancelled successfully.', 'success');
      refresh();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to cancel job.';
      showToast(message, 'error');
    } finally {
      setIsCancelling(false);
    }
  };

  return (
    <EnterpriseLayout variant="management">
      {/* Executive Overview */}
      <ExecutiveOverview 
        title="⚙️ Job Management"
        subtitle="Monitor and manage provisioning and compliance jobs across your infrastructure"
      >
        <article className="stat-card">
          <span className="muted">Total Jobs</span>
          <strong>{pagination.total}</strong>
          <small className="muted">All time</small>
        </article>
        <article className="stat-card">
          <span className="muted">Running</span>
          <strong>{statusSummary.running || 0}</strong>
          <small className="muted">Active jobs</small>
        </article>
        <article className="stat-card">
          <span className="muted">Worker Status</span>
          <strong>{workerStatus?.started ? 'Running' : 'Stopped'}</strong>
          <small className="muted">Job processor</small>
        </article>
        <article className="stat-card">
          <span className="muted">Success Rate</span>
          <strong>{pagination.total > 0 ? Math.round(((statusSummary.succeeded || 0) / pagination.total) * 100) : 0}%</strong>
          <small className="muted">Last 30 days</small>
        </article>
      </ExecutiveOverview>

      <div className="management-dashboard">
        {/* Main Content Area */}
        <div className="management-main">
          {/* Quick Actions */}
          <ManagementPanel 
            title="Quick Actions"
            icon="⚡"
            subtitle="Common job operations"
            position="primary"
          >
            <ActionZone alignment="left" variant="primary">
              <button type="button" className="primary-button" onClick={() => window.location.href = '/templates'}>
                🚀 Create Job from Template
              </button>
              <button type="button" className="ghost-button" onClick={() => window.location.href = '/templates'}>
                📋 Manage Templates
              </button>
            </ActionZone>
          </ManagementPanel>

          {/* Job List */}
          <ManagementPanel 
            title="📋 Job Registry"
            subtitle="Monitor job execution and manage job lifecycle"
            position="primary"
          >
            {loading ? (
              <p className="muted">Loading jobs…</p>
            ) : rows.length === 0 ? (
              <div className="empty-state">
                <p>No jobs found. Create a job template to get started.</p>
              </div>
            ) : (
              <div className="job-list">
                {rows.map((job) => (
                  <div key={job.id} className="job-card">
                    <header>
                      <h3>{job.type}</h3>
                      <ActionZone alignment="right" variant="secondary">
                        {jobCancelable(job.status) && (
                          <button
                            type="button"
                            className="danger-button"
                            onClick={() => handleCancelJob(job.id)}
                            disabled={isCancelling}
                          >
                            {isCancelling ? 'Cancelling…' : 'Cancel'}
                          </button>
                        )}
                        <button
                          type="button"
                          className="ghost-button"
                          onClick={() => setSelectedJobId(job.id)}
                          disabled={isCancelling}
                        >
                          Details
                        </button>
                      </ActionZone>
                    </header>
                    <dl>
                      <dt>Status</dt>
                      <dd>
                        <span className={`status-pill status-${statusClass(job.status)}`}>
                          {statusLabel(job.status)}
                        </span>
                      </dd>
                      <dt>Tenant</dt>
                      <dd>{job.tenant_id || '—'}</dd>
                      <dt>Node</dt>
                      <dd>—</dd>
                      <dt>Created</dt>
                      <dd>{formatDate(job.created_at)}</dd>
                      <dt>Started</dt>
                      <dd>{formatDate(job.started_at)}</dd>
                      <dt>Completed</dt>
                      <dd>{formatDate(job.finished_at)}</dd>
                    </dl>
                    {job.payload != null && (
                      <div className="job-result">
                        <h4>Payload</h4>
                        {renderPayload(job.payload)}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </ManagementPanel>
        </div>

        {/* Sidebar */}
        <div className="management-sidebar">
          {/* Filters */}
          <ManagementPanel 
            title="Filters"
            icon="🔍"
            subtitle={`${rows.length} jobs shown`}
            position="secondary"
          >
            <ContentGrid columns={1} gap="md">
              <div className="form-field">
                <label>Tenant</label>
                <select
                  value={tenantFilter}
                  onChange={(e) => setTenantFilter(e.target.value)}
                >
                  <option value="">All tenants</option>
                  {tenants?.map((tenant) => (
                    <option key={tenant.id} value={tenant.id}>
                      {tenant.name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="form-field">
                <label>Status</label>
                <select
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value)}
                >
                  <option value="">All statuses</option>
                  {STATUS_FILTERS.map((status) => (
                    <option key={status} value={status}>
                      {statusLabel(status)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="form-field">
                <label>Type</label>
                <input
                  type="text"
                  value={typeFilter}
                  onChange={(e) => setTypeFilter(e.target.value)}
                  placeholder="e.g. provision"
                />
              </div>
            </ContentGrid>
          </ManagementPanel>

          {/* Job Details */}
          {selectedJob && (
            <ManagementPanel 
              title={`Job: ${selectedJob.type}`}
              icon="📊"
              subtitle={`Status: ${statusLabel(selectedJob.status)}`}
              position="secondary"
            >
              <div className="job-details">
                <dl>
                  <dt>ID</dt>
                  <dd>{selectedJob.id}</dd>
                  <dt>Type</dt>
                  <dd>{selectedJob.type}</dd>
                  <dt>Status</dt>
                  <dd>
                    <span className={`status-pill status-${statusClass(selectedJob.status)}`}>
                      {statusLabel(selectedJob.status)}
                    </span>
                  </dd>
                  <dt>Tenant</dt>
                  <dd>{selectedJob.tenant_id || '—'}</dd>
                  <dt>Node</dt>
                  <dd>—</dd>
                  <dt>Created</dt>
                  <dd>{formatDate(selectedJob.created_at)}</dd>
                  <dt>Started</dt>
                  <dd>{formatDate(selectedJob.started_at)}</dd>
                  <dt>Completed</dt>
                  <dd>{formatDate(selectedJob.finished_at)}</dd>
                  {selectedJob.payload != null && (
                    <>
                      <dt>Payload</dt>
                      <dd>
                        {renderPayload(selectedJob.payload)}
                      </dd>
                    </>
                  )}
                  {selectedJob.events && selectedJob.events.length > 0 && (
                    <>
                      <dt>Events</dt>
                      <dd>
                        {renderPayload(selectedJob.events)}
                      </dd>
                    </>
                  )}
                </dl>
                <ActionZone alignment="right" variant="primary">
                  {jobCancelable(selectedJob.status) && (
                    <button
                      type="button"
                      className="danger-button"
                      onClick={() => handleCancelJob(selectedJob.id)}
                      disabled={isCancelling}
                    >
                      {isCancelling ? 'Cancelling…' : 'Cancel Job'}
                    </button>
                  )}
                  <button
                    type="button"
                    className="ghost-button"
                    onClick={() => setSelectedJobId(null)}
                  >
                    Close
                  </button>
                </ActionZone>
              </div>
            </ManagementPanel>
          )}
        </div>
      </div>
    </EnterpriseLayout>
  );
}