import { useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useCluster } from '../hooks/useCluster';
import { useApiClient } from '../hooks/useApiClient';
import { useToast } from '../providers/ToastProvider';
import type { ClusterMemberHealth, ClusterRolloutDetail } from '../lib/api';
import { clusterStateClass, clusterStateLabel } from './Clusters';
import './Clusters.css';

function formatHeartbeatAge(seconds?: number): string {
  if (seconds == null) {
    return 'never';
  }
  if (seconds < 60) {
    return `${seconds}s ago`;
  }
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}m ago`;
  }
  if (seconds < 86_400) {
    return `${Math.floor(seconds / 3600)}h ago`;
  }
  return `${Math.floor(seconds / 86_400)}d ago`;
}

function memberHealthClass(member: ClusterMemberHealth): string {
  if (member.healthy) {
    return 'topology-node topology-node--healthy';
  }
  if (member.state === 'active') {
    return 'topology-node topology-node--degraded';
  }
  return 'topology-node topology-node--unhealthy';
}

// TopologyDiagram renders a grouped-by-role grid of circular nodes. Colour is
// driven by the /health per-member bool (healthy/degraded/unhealthy). A v1
// SVG tree is overkill — a flex grid reads clearly and doesn't pull a new dep.
function TopologyDiagram({ members }: { members: ClusterMemberHealth[] }): JSX.Element {
  const byRole = useMemo(() => {
    const groups = new Map<string, ClusterMemberHealth[]>();
    members.forEach((member) => {
      const key = member.role || 'unassigned';
      if (!groups.has(key)) {
        groups.set(key, []);
      }
      groups.get(key)?.push(member);
    });
    return Array.from(groups.entries()).sort((a, b) => a[0].localeCompare(b[0]));
  }, [members]);

  if (byRole.length === 0) {
    return <p className="muted">No cluster members yet.</p>;
  }

  return (
    <div className="cluster-topology" data-testid="cluster-topology">
      {byRole.map(([role, roleMembers]) => (
        <div key={role} className="topology-row">
          <div className="topology-role-label">{role}</div>
          <div className="topology-nodes">
            {roleMembers.map((member) => (
              <div
                key={member.node_id}
                className={memberHealthClass(member)}
                title={`${member.hostname || member.node_id}\nstate: ${member.state}${member.reason ? `\n${member.reason}` : ''}`}
              >
                <span className="topology-node-ordinal">{member.position}</span>
                <span className="topology-node-hostname">
                  {member.hostname || member.node_id.slice(0, 8)}
                </span>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function MemberTable({ members }: { members: ClusterMemberHealth[] }): JSX.Element {
  if (members.length === 0) {
    return <p className="muted">No members.</p>;
  }
  return (
    <table className="clusters-table" data-testid="cluster-member-table">
      <thead>
        <tr>
          <th>Hostname</th>
          <th>Role</th>
          <th>Position</th>
          <th>State</th>
          <th>Heartbeat</th>
          <th>Compliance</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {members.map((member) => (
          <tr key={member.node_id}>
            <td className="mono">{member.hostname || member.node_id.slice(0, 8)}</td>
            <td>{member.role}</td>
            <td>{member.position}</td>
            <td>{member.state}</td>
            <td>{formatHeartbeatAge(member.heartbeat_age_seconds)}</td>
            <td>
              {member.compliance_healthy === undefined
                ? '—'
                : member.compliance_healthy
                  ? 'passing'
                  : 'failing'}
            </td>
            <td>
              <span
                className={
                  member.healthy
                    ? 'cluster-badge cluster-badge--healthy'
                    : 'cluster-badge cluster-badge--unhealthy'
                }
              >
                {member.healthy ? 'Healthy' : (member.reason ?? 'Unhealthy')}
              </span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function RolloutProgressBar({
  rollout,
}: {
  rollout: ClusterRolloutDetail | null;
}): JSX.Element | null {
  if (!rollout) {
    return null;
  }
  const totalWaves = Math.max(rollout.waves.length, rollout.current_wave + 1, 1);
  const completedWaves = rollout.waves.filter((wave) => wave.state === 'healthy').length;
  const percent = Math.min(100, Math.round((completedWaves / totalWaves) * 100));

  return (
    <div className="panel cluster-rollout-panel" data-testid="cluster-rollout-panel">
      <div className="cluster-rollout-header">
        <h3>Rollout progress</h3>
        <span className={`cluster-badge cluster-badge--${rollout.state}`}>{rollout.state}</span>
      </div>
      <div className="cluster-rollout-progress" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}>
        <div className="cluster-rollout-progress-fill" style={{ width: `${percent}%` }} />
      </div>
      <small className="muted">
        {completedWaves} of {totalWaves} waves healthy · current wave {rollout.current_wave}
      </small>
      {rollout.waves.length > 0 ? (
        <table className="clusters-table cluster-waves-table">
          <thead>
            <tr>
              <th>Wave</th>
              <th>State</th>
              <th>Members</th>
              <th>Started</th>
            </tr>
          </thead>
          <tbody>
            {rollout.waves.map((wave) => (
              <tr key={wave.id}>
                <td>{wave.wave_number}</td>
                <td>{wave.state}</td>
                <td>{wave.member_ids.length}</td>
                <td>{new Date(wave.started_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </div>
  );
}

function DrainConfirmModal({
  open,
  fromSize,
  toSize,
  onCancel,
  onConfirm,
  submitting,
}: {
  open: boolean;
  fromSize: number;
  toSize: number;
  onCancel: () => void;
  onConfirm: () => void;
  submitting: boolean;
}): JSX.Element | null {
  if (!open) {
    return null;
  }
  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" data-testid="drain-confirm-modal">
      <div className="modal-panel">
        <h3>Confirm scale down</h3>
        <p>
          Scaling from <strong>{fromSize}</strong> to <strong>{toSize}</strong> will drain and
          destroy <strong>{fromSize - toSize}</strong> member(s). This action is queued as a
          cluster.scale job and cannot be reversed without a new provision.
        </p>
        <div className="detail-actions">
          <button type="button" className="ghost-button" onClick={onCancel} disabled={submitting}>
            Cancel
          </button>
          <button type="button" className="danger-button" onClick={onConfirm} disabled={submitting}>
            {submitting ? 'Queuing…' : 'Drain and scale down'}
          </button>
        </div>
      </div>
    </div>
  );
}

export function ClusterDetail(): JSX.Element {
  const params = useParams<{ clusterId: string }>();
  const clusterId = params.clusterId;
  const { cluster, health, rollout, loading, error, reload } = useCluster(clusterId);
  const api = useApiClient();
  const { showToast } = useToast();

  const [desiredSize, setDesiredSize] = useState<number>(0);
  const [submitting, setSubmitting] = useState(false);
  const [drainOpen, setDrainOpen] = useState(false);

  useEffect(() => {
    if (cluster) {
      setDesiredSize(cluster.desired_size);
    }
  }, [cluster]);

  const healthState = health?.state;
  const healthBadgeClass = clusterStateClass(healthState);

  const applyScale = async (target: number) => {
    if (!clusterId || !cluster) {
      return;
    }
    setSubmitting(true);
    try {
      await api.updateCluster(clusterId, { desired_size: target });
      showToast(`Cluster scaled to ${target}`, 'success');
      setDrainOpen(false);
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to scale cluster';
      showToast(message, 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const handleScaleSubmit = async () => {
    if (!cluster) {
      return;
    }
    if (desiredSize === cluster.desired_size) {
      showToast('Desired size unchanged', 'info');
      return;
    }
    if (desiredSize < cluster.desired_size) {
      setDrainOpen(true);
      return;
    }
    await applyScale(desiredSize);
  };

  if (!clusterId) {
    return <p className="form-error">Missing cluster id.</p>;
  }

  return (
    <section className="cluster-detail-page">
      <div className="page-header">
        <div>
          <p className="eyebrow">
            <Link to="/clusters" className="cluster-link">
              ← All clusters
            </Link>
          </p>
          <h2>{cluster?.name ?? 'Cluster'}</h2>
          <p className="muted">
            {cluster ? `${cluster.provider} · ${cluster.failure_domain_strategy}` : '—'}
          </p>
        </div>
        {healthState ? (
          <span className={healthBadgeClass} data-testid="cluster-health-badge">
            {clusterStateLabel(healthState)}
          </span>
        ) : null}
      </div>

      {loading ? <p className="muted">Loading cluster&hellip;</p> : null}
      {error ? <p className="form-error">{error}</p> : null}

      {cluster && health ? (
        <>
          <div className="stat-card-grid">
            <article className="stat-card">
              <span className="muted">Members</span>
              <strong>
                {health.healthy_count} / {health.total_count}
              </strong>
              <small className="muted">healthy</small>
            </article>
            <article className="stat-card">
              <span className="muted">Quorum</span>
              <strong>{health.quorum}</strong>
              <small className="muted">{health.quorum_met ? 'met' : 'not met'}</small>
            </article>
            <article className="stat-card">
              <span className="muted">Desired size</span>
              <strong>{cluster.desired_size}</strong>
              <small className="muted">lifecycle state: {cluster.state}</small>
            </article>
            <article className="stat-card">
              <span className="muted">Provider</span>
              <strong>{cluster.provider}</strong>
              <small className="muted">{cluster.failure_domain_strategy}</small>
            </article>
          </div>

          <div className="panel cluster-scale-panel" data-testid="cluster-scale-panel">
            <div className="cluster-rollout-header">
              <h3>Scale cluster</h3>
              <span className="muted">current: {cluster.desired_size}</span>
            </div>
            <label htmlFor="cluster-scale-slider">Desired size</label>
            <div className="cluster-scale-slider">
              <input
                id="cluster-scale-slider"
                type="range"
                min={0}
                max={Math.max(cluster.desired_size * 2, 10)}
                value={desiredSize}
                onChange={(event) => setDesiredSize(Number(event.target.value))}
                disabled={submitting}
                data-testid="cluster-scale-slider"
              />
              <input
                type="number"
                min={0}
                value={desiredSize}
                onChange={(event) => setDesiredSize(Number(event.target.value))}
                disabled={submitting}
                data-testid="cluster-scale-input"
                aria-label="Desired size numeric"
              />
            </div>
            <div className="detail-actions">
              <button
                type="button"
                className="primary-button"
                onClick={handleScaleSubmit}
                disabled={submitting || desiredSize === cluster.desired_size}
                data-testid="cluster-scale-submit"
              >
                {submitting ? 'Queuing…' : 'Apply'}
              </button>
            </div>
          </div>

          <div className="panel">
            <h3>Topology</h3>
            <TopologyDiagram members={health.members} />
          </div>

          <div className="panel">
            <h3>Members</h3>
            <MemberTable members={health.members} />
          </div>

          {rollout ? <RolloutProgressBar rollout={rollout} /> : null}
        </>
      ) : null}

      <DrainConfirmModal
        open={drainOpen}
        fromSize={cluster?.desired_size ?? 0}
        toSize={desiredSize}
        onCancel={() => setDrainOpen(false)}
        onConfirm={() => applyScale(desiredSize)}
        submitting={submitting}
      />
    </section>
  );
}
