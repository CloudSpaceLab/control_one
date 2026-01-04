import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useJobs } from '../hooks/useJobs';

const JOB_POLL_INTERVAL = 0;

export function Dashboard(): JSX.Element {
  const navigate = useNavigate();
  const {
    pagination: tenantPagination,
    data: tenantSample,
    loading: tenantsLoading,
  } = useTenants({ limit: 5, offset: 0 });
  const {
    pagination: nodePagination,
    loading: nodesLoading,
  } = useNodes({ limit: 5, offset: 0 });
  const {
    pagination: jobPagination,
    data: recentJobs,
    loading: jobsLoading,
  } = useJobs({ limit: 5, offset: 0, pollIntervalMs: JOB_POLL_INTERVAL });

  const jobStatusSummary = useMemo(() => {
    return recentJobs.reduce<Record<string, number>>((acc, job) => {
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

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Overview</p>
          <h2>Welcome back</h2>
          <p className="subtitle">Monitor posture, trigger workflows, or drill into tenants from here.</p>
        </div>
      </header>

      <div className="stat-grid">
        {stats.map((stat) => (
          <article key={stat.label} className="stat-card">
            <p>{stat.label}</p>
            {stat.loading ? (
              <span className="stat-value pulse">…</span>
            ) : (
              <StatValue value={stat.value} />
            )}
            <small>{stat.trend}</small>
          </article>
        ))}
      </div>

      <div className="dashboard-panels">
        <article className="quick-actions">
          <h3>Quick actions</h3>
          <ul>
            {quickActions.map((action) => (
              <li key={action.title}>
                <div>
                  <strong>{action.title}</strong>
                  <p>{action.copy}</p>
                </div>
                <button type="button" className="primary-button" onClick={action.action}>
                  Go
                </button>
              </li>
            ))}
          </ul>
        </article>

        <article className="recent-activity">
          <h3>Recent jobs</h3>
          {jobsLoading ? (
            <p className="muted">Loading activity…</p>
          ) : recentJobs.length === 0 ? (
            <p className="muted">No jobs have run yet.</p>
          ) : (
            <ul>
              {recentJobs.map((job) => (
                <li key={job.id}>
                  <div>
                    <strong>{job.type}</strong>
                    <small>{new Date(job.created_at).toLocaleString()}</small>
                  </div>
                  <span className={`status-pill status-${job.status.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`}>
                    {job.status}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </article>

        <article className="tenant-preview">
          <h3>Tenants at a glance</h3>
          {tenantsLoading ? (
            <p className="muted">Loading tenants…</p>
          ) : tenantSample.length === 0 ? (
            <p className="muted">No tenants yet. Create one to get started.</p>
          ) : (
            <dl>
              {tenantSample.map((tenant) => (
                <div key={tenant.id}>
                  <dt>{tenant.name}</dt>
                  <dd>{new Date(tenant.created_at).toLocaleDateString()}</dd>
                </div>
              ))}
            </dl>
          )}
        </article>
      </div>
    </section>
  );
}

function StatValue({ value }: { value: number }): JSX.Element {
  const [displayValue, setDisplayValue] = useState(0);

  useEffect(() => {
    let frame: number | undefined;
    let start: number | null = null;
    const duration = 800;
    const initial = displayValue;
    const delta = value - initial;

    const step = (timestamp: number) => {
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

  return <span className="stat-value">{Math.round(displayValue).toLocaleString()}</span>;
}
