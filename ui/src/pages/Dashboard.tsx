import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useJobs } from '../hooks/useJobs';
import { 
  EnterpriseLayout, 
  ExecutiveOverview, 
  ManagementPanel, 
  ActionZone,
  ContentGrid 
} from '../components/EnterpriseLayout';
import './EnterpriseLayout.css';

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
    <EnterpriseLayout variant="dashboard">
      {/* Setup Prompt */}
      {tenantPagination.total === 0 && (
        <ManagementPanel 
          title="🚀 Get Started with Control One"
          subtitle="Your control plane is ready! Let's set up your first tenant and configure your infrastructure."
          position="primary"
        >
          <ActionZone alignment="center" variant="primary">
            <button type="button" className="primary-button" onClick={() => navigate('/setup')}>
              Launch Setup Wizard
            </button>
            <button type="button" className="ghost-button" onClick={() => navigate('/tenants')}>
              Manual Setup
            </button>
          </ActionZone>
        </ManagementPanel>
      )}

      {/* Executive KPI Overview */}
      <ExecutiveOverview 
        title="📊 Executive Dashboard"
        subtitle="Real-time system posture and performance metrics"
      >
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
      </ExecutiveOverview>

      {/* Executive Dashboard Layout */}
      <div className="executive-main">
        {/* Quick Actions Panel */}
        <ManagementPanel 
          title="⚡ Quick Actions"
          subtitle="Common tasks to get you started"
          position="primary"
        >
          <ContentGrid columns={1} gap="md">
            {quickActions.map((action) => (
              <div key={action.title} className="quick-action-item">
                <div className="quick-action-content">
                  <strong>{action.title}</strong>
                  <p>{action.copy}</p>
                </div>
                <ActionZone alignment="right" variant="secondary">
                  <button type="button" className="primary-button" onClick={action.action}>
                    Launch
                  </button>
                </ActionZone>
              </div>
            ))}
          </ContentGrid>
        </ManagementPanel>

        {/* System Health Panel */}
        <ManagementPanel 
          title="🏥 System Health"
          subtitle="Overall system status and performance"
          position="secondary"
        >
          <ContentGrid columns={2} gap="md">
            <div className="health-item">
              <div className="health-indicator health-good"></div>
              <div className="health-content">
                <strong>Control Plane</strong>
                <small>Operational</small>
              </div>
            </div>
            <div className="health-item">
              <div className="health-indicator health-good"></div>
              <div className="health-content">
                <strong>Database</strong>
                <small>Connected</small>
              </div>
            </div>
            <div className="health-item">
              <div className="health-indicator health-warning"></div>
              <div className="health-content">
                <strong>Node Mesh</strong>
                <small>{nodePagination.total > 0 ? 'Active' : 'No nodes'}</small>
              </div>
            </div>
            <div className="health-item">
              <div className="health-indicator health-good"></div>
              <div className="health-content">
                <strong>Job Queue</strong>
                <small>{jobStatusSummary.running > 0 ? 'Processing' : 'Idle'}</small>
              </div>
            </div>
          </ContentGrid>
        </ManagementPanel>
      </div>

      {/* Sidebar Content */}
      <div className="executive-sidebar">
        {/* Recent Activity */}
        <ManagementPanel 
          title="📈 Recent Activity"
          subtitle={`${recentJobs.length} recent jobs`}
          position="tertiary"
        >
          {jobsLoading ? (
            <p className="muted">Loading activity…</p>
          ) : recentJobs.length === 0 ? (
            <div className="empty-state">
              <p>No jobs have run yet.</p>
            </div>
          ) : (
            <div className="activity-list">
              {recentJobs.map((job) => (
                <div key={job.id} className="activity-item">
                  <div className="activity-content">
                    <strong>{job.type}</strong>
                    <small>{new Date(job.created_at).toLocaleString()}</small>
                  </div>
                  <span className={`status-pill status-${job.status.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`}>
                    {job.status}
                  </span>
                </div>
              ))}
            </div>
          )}
        </ManagementPanel>

        {/* Tenants Overview */}
        <ManagementPanel 
          title="🏢 Tenants at a Glance"
          subtitle={`${tenantPagination.total} total tenants`}
          position="tertiary"
        >
          {tenantsLoading ? (
            <p className="muted">Loading tenants…</p>
          ) : tenantSample.length === 0 ? (
            <div className="empty-state">
              <p>No tenants yet. Create one to get started.</p>
            </div>
          ) : (
            <div className="tenant-summary-list">
              {tenantSample.map((tenant) => (
                <div key={tenant.id} className="tenant-summary-item">
                  <div className="tenant-summary-content">
                    <strong>{tenant.name}</strong>
                    <small>{new Date(tenant.created_at).toLocaleDateString()}</small>
                  </div>
                </div>
              ))}
            </div>
          )}
        </ManagementPanel>
      </div>
    </EnterpriseLayout>
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
