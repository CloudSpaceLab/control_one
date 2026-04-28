import { FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { SectionHeader } from '../components/kit';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import type {
  FleetEnrollRequest,
  FleetEnrollResult,
  FleetEnrollStatus,
  FleetEnrollTarget,
  NodeSummary,
} from '../lib/api';

// JOB_POLL_MS drives how often we re-fetch the fleet enroll status. 1000ms
// matches the plan's 1s cadence and keeps the UI responsive without hammering
// the API while 50+ hosts are being SSH'd in parallel.
const JOB_POLL_MS = 1000;

// NODE_POLL_MS is the slower cadence used once the SSH-provisioning phase has
// ended and we're watching the per-node state transitions (pending -> active
// or enrollment_failed). 5s matches the 10-minute enrollment-pending timeout.
const NODE_POLL_MS = 5000;

// MAX_NODE_POLL_MS caps how long we keep polling a single node. 10 minutes
// covers the full enrollment_pending timeout + a generous buffer.
const MAX_NODE_POLL_MS = 10 * 60 * 1000;

// parseTargets splits the newline-separated host list into FleetEnrollTargets.
// Blank lines and # comments are ignored. Each non-comment line may be
// `host`, `host:port`, or `user@host[:port]`. The server-side validator has
// the authoritative rules; this is a UX hint.
function parseTargets(raw: string): FleetEnrollTarget[] {
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'))
    .map((line) => {
      let user: string | undefined;
      let hostPort = line;
      if (line.includes('@')) {
        const [u, rest] = line.split('@', 2);
        user = u.trim() || undefined;
        hostPort = rest.trim();
      }
      const colonIdx = hostPort.lastIndexOf(':');
      if (colonIdx > 0) {
        const hostPart = hostPort.substring(0, colonIdx);
        const portPart = hostPort.substring(colonIdx + 1);
        const port = Number.parseInt(portPart, 10);
        if (!Number.isNaN(port) && port > 0) {
          return { host: hostPart, port, user };
        }
      }
      return { host: hostPort, user };
    });
}

// encodeSshKey converts a PEM-formatted SSH private key into the base64
// payload the server expects. An already-base64 paste (no BEGIN marker) is
// returned as-is.
function encodeSshKey(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) {
    return '';
  }
  if (trimmed.includes('-----BEGIN')) {
    // Browser btoa handles ASCII; PEM is 7-bit ASCII so this is safe.
    return btoa(trimmed);
  }
  return trimmed;
}

interface NodeGateState {
  nodeId: string;
  host: string;
  state: string;
  lastSeenAt?: string;
  firstScanAt?: string;
  updatedAt?: string;
  error?: string;
  // startedAt is when we began polling; used to time out NODE polling.
  startedAt: number;
}

export function FleetEnroll(): JSX.Element {
  const api = useApiClient();
  const { showToast } = useToast();
  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { data: tenants } = useTenants();

  const [tenantId, setTenantId] = useState<string>('');
  const [targetsRaw, setTargetsRaw] = useState<string>('');
  const [sshUser, setSshUser] = useState<string>('');
  const [sshKey, setSshKey] = useState<string>('');
  const [sshPassword, setSshPassword] = useState<string>('');
  const [token, setToken] = useState<string>('');
  const [parallel, setParallel] = useState<number>(5);
  const [submitting, setSubmitting] = useState<boolean>(false);

  const [jobId, setJobId] = useState<string | null>(null);
  const [jobStatus, setJobStatus] = useState<FleetEnrollStatus | null>(null);
  const [pollError, setPollError] = useState<string | null>(null);

  // Per-host node gate state. Keyed by SSH host so we can merge enrollment
  // results (which produce node ids) with node-state polls.
  const [nodeStates, setNodeStates] = useState<Record<string, NodeGateState>>({});

  const parsedTargets = useMemo(() => parseTargets(targetsRaw), [targetsRaw]);

  // jobStatusRef mirrors jobStatus so the polling loop can short-circuit on
  // a terminal status without re-registering the interval every tick.
  const jobStatusRef = useRef<FleetEnrollStatus | null>(null);
  useEffect(() => {
    jobStatusRef.current = jobStatus;
  }, [jobStatus]);

  // ── job polling ───────────────────────────────────────────────────────
  useEffect(() => {
    if (!jobId) {
      return undefined;
    }

    let cancelled = false;
    const terminal = new Set(['succeeded', 'failed', 'cancelled']);

    const fetchStatus = async () => {
      try {
        const status = await api.getFleetEnrollStatus(jobId);
        if (!cancelled) {
          setJobStatus(status);
          setPollError(null);
        }
      } catch (err) {
        if (!cancelled) {
          const message = err instanceof Error ? err.message : 'Failed to poll fleet job';
          setPollError(message);
        }
      }
    };

    fetchStatus();
    const timer = setInterval(() => {
      if (jobStatusRef.current && terminal.has(jobStatusRef.current.status)) {
        clearInterval(timer);
        return;
      }
      fetchStatus();
    }, JOB_POLL_MS);

    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [api, jobId]);

  // When the fleet job reports per-host results, ensure we start tracking
  // node-gate state for every successful enrollment.
  useEffect(() => {
    if (!jobStatus) {
      return;
    }
    const now = Date.now();
    setNodeStates((prev) => {
      const next = { ...prev };
      for (const result of jobStatus.results) {
        if (!result.success || !result.node_id) {
          continue;
        }
        if (!next[result.host]) {
          next[result.host] = {
            nodeId: result.node_id,
            host: result.host,
            state: 'enrollment_pending',
            startedAt: now,
          };
        } else if (!next[result.host].nodeId) {
          next[result.host] = { ...next[result.host], nodeId: result.node_id };
        }
      }
      return next;
    });
  }, [jobStatus]);

  // ── node gate polling ─────────────────────────────────────────────────
  const nodeTimersRef = useRef<Map<string, ReturnType<typeof setInterval>>>(new Map());

  useEffect(() => {
    const activeTimers = nodeTimersRef.current;
    const entries = Object.values(nodeStates);

    for (const gate of entries) {
      if (!gate.nodeId) {
        continue;
      }
      // Skip nodes in terminal gate states or aged past the timeout.
      const terminal =
        gate.state === 'active' || gate.state === 'enrollment_failed' || gate.state === 'retired';
      const expired = Date.now() - gate.startedAt > MAX_NODE_POLL_MS;
      if (terminal || expired) {
        const existing = activeTimers.get(gate.host);
        if (existing) {
          clearInterval(existing);
          activeTimers.delete(gate.host);
        }
        continue;
      }
      if (activeTimers.has(gate.host)) {
        continue;
      }
      const fetchNode = async () => {
        try {
          const node = (await api.getNode(gate.nodeId)) as NodeSummary;
          setNodeStates((prev) => {
            const existing = prev[gate.host];
            if (!existing) {
              return prev;
            }
            return {
              ...prev,
              [gate.host]: {
                ...existing,
                state: String(node.state),
                lastSeenAt: node.last_seen_at,
                firstScanAt: node.first_scan_at,
                updatedAt: node.updated_at,
                error: undefined,
              },
            };
          });
        } catch (err) {
          const message = err instanceof Error ? err.message : 'Failed to poll node';
          setNodeStates((prev) => {
            const existing = prev[gate.host];
            if (!existing) {
              return prev;
            }
            return { ...prev, [gate.host]: { ...existing, error: message } };
          });
        }
      };
      fetchNode();
      const timer = setInterval(fetchNode, NODE_POLL_MS);
      activeTimers.set(gate.host, timer);
    }

    return () => {
      // no-op on re-render; teardown happens on unmount below
    };
  }, [api, nodeStates]);

  useEffect(() => {
    // Snapshot the ref on effect setup so the cleanup doesn't reach into a
    // mutated ref after unmount (react-hooks/exhaustive-deps warns otherwise).
    const timers = nodeTimersRef.current;
    return () => {
      for (const timer of timers.values()) {
        clearInterval(timer);
      }
      timers.clear();
    };
  }, []);

  // ── submit handler ────────────────────────────────────────────────────
  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    resetFeedback();

    if (parsedTargets.length === 0) {
      showError('Add at least one host');
      return;
    }
    if (!token.trim()) {
      showError('Enrollment token is required');
      return;
    }
    const hasPerTargetUsers = parsedTargets.every((t) => !!t.user?.trim());
    if (!sshUser.trim() && !hasPerTargetUsers) {
      showError('SSH user is required (or include user@host in each target line)');
      return;
    }
    if (!sshKey.trim() && !sshPassword.trim()) {
      showError('Either an SSH private key or a password is required');
      return;
    }

    const payload: FleetEnrollRequest = {
      targets: parsedTargets,
      token: token.trim(),
      parallel,
    };
    if (sshUser.trim()) {
      payload.ssh_user = sshUser.trim();
    }
    if (sshKey.trim()) {
      payload.ssh_key = encodeSshKey(sshKey);
    }
    if (sshPassword.trim()) {
      payload.ssh_password = sshPassword;
    }

    try {
      setSubmitting(true);
      const response = await api.startFleetEnroll(payload);
      setJobId(response.job_id);
      setJobStatus(null);
      setNodeStates({});
      showSuccess(`Fleet job ${response.job_id} queued for ${parsedTargets.length} target(s)`);
      showToast(`Fleet enrollment started — ${parsedTargets.length} hosts`, 'success');
      if (tenantId) {
        // tenant id is captured purely for UX / downstream linking today;
        // the fleet endpoint infers the tenant from the enrollment token.
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Fleet enrollment failed';
      showError(message);
      showToast(message, 'error');
    } finally {
      setSubmitting(false);
    }
  };

  // ── render helpers ────────────────────────────────────────────────────
  function statusBadge(status: string): JSX.Element {
    const cls = status.toLowerCase().replace(/[^a-z0-9]+/g, '-');
    return <span className={`status-pill status-${cls}`}>{status}</span>;
  }

  function formatDuration(ms?: number): string {
    if (!ms) return '—';
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function renderNodeGate(host: string): JSX.Element {
    const gate = nodeStates[host];
    if (!gate?.nodeId) {
      return <span className="muted">—</span>;
    }
    const icon =
      gate.state === 'active'
        ? 'OK'
        : gate.state === 'enrollment_failed'
          ? 'FAIL'
          : 'WAIT';
    return (
      <span
        className={`status-pill status-${gate.state.replace(/_/g, '-')}`}
        title={gate.error ?? gate.state}
      >
        {icon} {gate.state}
      </span>
    );
  }

  const jobTerminal = jobStatus?.status === 'succeeded' || jobStatus?.status === 'failed';
  const anyStillPending = Object.values(nodeStates).some(
    (g) => g.state === 'enrollment_pending',
  );
  const showReadyNotice = jobTerminal && !anyStillPending && Object.keys(nodeStates).length > 0;

  return (
    <div className="flex flex-col gap-5" aria-labelledby="fleet-enroll-heading">
      <SectionHeader
        eyebrow="INFRASTRUCTURE · ONBOARDING"
        title="Bulk enrol hosts"
        description="Onboard many hosts over SSH at once. Live progress per target."
      />

      <article className="card">
        <h3>1. Targets</h3>
        <form onSubmit={handleSubmit} aria-label="fleet enrollment form">
          <div className="form-grid">
            <label className="form-field form-field-full">
              <span>Hosts (one per line, <code>host</code>, <code>host:port</code>, or <code>user@host:port</code>)</span>
              <textarea
                rows={6}
                value={targetsRaw}
                onChange={(e) => setTargetsRaw(e.target.value)}
                placeholder={`10.0.1.5\nadmin@10.0.1.6:22\n# comments allowed`}
                aria-label="targets"
              />
              <small>{parsedTargets.length} target(s) parsed</small>
            </label>

            <label className="form-field">
              <span>Default SSH user</span>
              <input
                type="text"
                value={sshUser}
                onChange={(e) => setSshUser(e.target.value)}
                autoComplete="username"
                aria-label="ssh user"
              />
            </label>

            <label className="form-field">
              <span>Parallelism</span>
              <input
                type="number"
                min={1}
                max={50}
                value={parallel}
                onChange={(e) => setParallel(Math.max(1, Number.parseInt(e.target.value, 10) || 1))}
                aria-label="parallelism"
              />
            </label>

            <label className="form-field form-field-full">
              <span>SSH private key (PEM)</span>
              <textarea
                rows={4}
                value={sshKey}
                onChange={(e) => setSshKey(e.target.value)}
                placeholder={`-----BEGIN OPENSSH PRIVATE KEY-----\n...`}
                aria-label="ssh private key"
                autoComplete="off"
              />
              <small>Key is held in memory only — never persisted.</small>
            </label>

            <label className="form-field">
              <span>SSH password (fallback)</span>
              <input
                type="password"
                value={sshPassword}
                onChange={(e) => setSshPassword(e.target.value)}
                autoComplete="new-password"
                aria-label="ssh password"
              />
            </label>

            <label className="form-field">
              <span>Enrollment token</span>
              <input
                type="text"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="cot_…"
                autoComplete="off"
                aria-label="enrollment token"
              />
            </label>

            <label className="form-field">
              <span>Tenant (optional, for reference)</span>
              <select
                value={tenantId}
                onChange={(e) => setTenantId(e.target.value)}
                aria-label="tenant"
              >
                <option value="">— none —</option>
                {tenants.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {formError ? <p className="form-error" role="alert">{formError}</p> : null}
          {formSuccess ? <p className="form-success" role="status">{formSuccess}</p> : null}

          <div className="form-actions">
            <button
              type="submit"
              className="primary-button"
              disabled={submitting}
            >
              {submitting ? 'Starting…' : 'Start fleet enrollment'}
            </button>
          </div>
        </form>
      </article>

      {jobId ? (
        <article className="card">
          <h3>2. Per-host progress</h3>
          <p className="muted">
            Job <code>{jobId}</code> —{' '}
            {jobStatus ? statusBadge(jobStatus.status) : 'queued'}
          </p>
          {pollError ? <p className="form-error">{pollError}</p> : null}

          <table className="data-table" aria-label="per-host enrollment progress">
            <thead>
              <tr>
                <th>Host</th>
                <th>Port</th>
                <th>SSH</th>
                <th>Duration</th>
                <th>Node</th>
                <th>Gate</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              {(jobStatus?.results ?? []).map((result: FleetEnrollResult) => (
                <tr key={result.id}>
                  <td>{result.host}</td>
                  <td>{result.port || '—'}</td>
                  <td>
                    {result.success ? (
                      <span className="status-pill status-succeeded">OK</span>
                    ) : (
                      <span className="status-pill status-failed">FAIL</span>
                    )}
                  </td>
                  <td>{formatDuration(result.duration_ms)}</td>
                  <td>
                    {result.node_id ? (
                      <a href={`/nodes?id=${result.node_id}`}>{result.node_id.slice(0, 8)}…</a>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td>{renderNodeGate(result.host)}</td>
                  <td className="muted" title={result.error_message ?? ''}>
                    {result.error_message ? result.error_message.slice(0, 80) : '—'}
                  </td>
                </tr>
              ))}
              {!jobStatus || jobStatus.results.length === 0 ? (
                <tr>
                  <td colSpan={7} className="muted">
                    Awaiting first results…
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>

          {showReadyNotice ? (
            <p className="form-success" role="status">
              All hosts passed the enrollment gate.
            </p>
          ) : null}
        </article>
      ) : null}
    </div>
  );
}
