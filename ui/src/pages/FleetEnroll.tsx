import { Fragment, FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import {
  SectionHeader,
  Panel,
  EmptyState,
  StatusTag,
  SelectField,
  FileUploadButton,
  ExpandableCode,
} from '../components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import {
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Copy,
  Check,
  Terminal,
} from 'lucide-react';
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

  // Form collapsed when a job is being viewed
  const [formCollapsed, setFormCollapsed] = useState<boolean>(false);
  // Per-host expanded ssh_output rows
  const [expandedHosts, setExpandedHosts] = useState<Set<string>>(new Set());
  // Copy-to-clipboard feedback
  const [copied, setCopied] = useState(false);

  const [jobId, setJobId] = useState<string | null>(null);
  const [jobStatus, setJobStatus] = useState<FleetEnrollStatus | null>(null);

  const [searchParams] = useSearchParams();

  // Auto-load job from URL param and collapse the form so the job panel is prominent.
  useEffect(() => {
    const urlJobId = searchParams.get('job_id');
    if (urlJobId && !jobId) {
      setJobId(urlJobId);
      setFormCollapsed(true);
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const [pollError, setPollError] = useState<string | null>(null);

  const [nodeStates, setNodeStates] = useState<Record<string, NodeGateState>>({});

  const parsedTargets = useMemo(() => parseTargets(targetsRaw), [targetsRaw]);

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
      // teardown on unmount only
    };
  }, [api, nodeStates]);

  useEffect(() => {
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
      setExpandedHosts(new Set());
      setFormCollapsed(true);
      showSuccess(`Fleet job ${response.job_id} queued for ${parsedTargets.length} target(s)`);
      showToast(`Fleet enrollment started — ${parsedTargets.length} hosts`, 'success');
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
    const s = status.toLowerCase();
    if (s === 'succeeded') return <StatusTag tone="healthy">{status}</StatusTag>;
    if (s === 'failed') return <StatusTag tone="critical">{status}</StatusTag>;
    if (s === 'running') return <StatusTag tone="warning">{status}</StatusTag>;
    if (s === 'queued') return <StatusTag tone="info">{status}</StatusTag>;
    if (s === 'cancelled') return <StatusTag tone="unknown">{status}</StatusTag>;
    return <StatusTag tone="info">{status}</StatusTag>;
  }

  function formatDuration(ms?: number): string {
    if (!ms) return '—';
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function renderNodeGate(host: string): JSX.Element {
    const gate = nodeStates[host];
    if (!gate?.nodeId) {
      return <span className="text-text-muted text-xs">—</span>;
    }
    const tone =
      gate.state === 'active'
        ? 'healthy'
        : gate.state === 'enrollment_failed'
          ? 'critical'
          : 'warning';
    const icon =
      gate.state === 'active'
        ? 'OK'
        : gate.state === 'enrollment_failed'
          ? 'FAIL'
          : 'WAIT';
    return (
      <StatusTag tone={tone} title={gate.error ?? gate.state}>
        {icon} {gate.state}
      </StatusTag>
    );
  }

  const copyJobId = async () => {
    if (!jobId) return;
    await navigator.clipboard.writeText(jobId);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const toggleHostExpand = (host: string) => {
    setExpandedHosts((prev) => {
      const next = new Set(prev);
      if (next.has(host)) {
        next.delete(host);
      } else {
        next.add(host);
      }
      return next;
    });
  };

  const jobTerminal = jobStatus?.status === 'succeeded' || jobStatus?.status === 'failed';
  const anyStillPending = Object.values(nodeStates).some(
    (g) => g.state === 'enrollment_pending',
  );
  const showReadyNotice = jobTerminal && !anyStillPending && Object.keys(nodeStates).length > 0;

  const results: FleetEnrollResult[] = jobStatus?.results ?? [];

  return (
    <div className="flex flex-col gap-5" aria-labelledby="fleet-enroll-heading">
      <SectionHeader
        eyebrow="INFRASTRUCTURE · ONBOARDING"
        title="Bulk enrol hosts"
        description="Onboard many hosts over SSH at once. Live progress per target."
      />

      {/* ── Job progress panel — shown first when a job is active ── */}
      {jobId ? (
        <Panel
          padding="md"
          eyebrow="JOB · PROGRESS"
          title="Enrollment progress"
          toneAccent="brand"
          actions={
            <div className="flex items-center gap-2">
              {jobStatus ? statusBadge(jobStatus.status) : <StatusTag tone="info">queued</StatusTag>}
            </div>
          }
        >
          {/* Job ID row */}
          <div className="flex items-center gap-2 rounded-md border border-border-subtle bg-surface px-3 py-2">
            <span className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Job ID</span>
            <code className="flex-1 font-mono text-xs text-foreground">{jobId}</code>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 gap-1.5 px-2"
              onClick={copyJobId}
              title="Copy job ID"
            >
              {copied ? (
                <Check className="h-3.5 w-3.5 text-state-healthy" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
              {copied ? 'Copied' : 'Copy'}
            </Button>
          </div>

          {pollError ? (
            <p className="text-sm text-state-critical" role="alert">
              {pollError}
            </p>
          ) : null}

          {/* Progress stats */}
          {jobStatus && results.length > 0 && (
            <div className="flex items-center gap-4 text-sm text-text-secondary">
              <span>{results.filter((r) => r.success).length} succeeded</span>
              <span>·</span>
              <span>{results.filter((r) => !r.success).length} failed</span>
              <span>·</span>
              <span>{results.length} total</span>
            </div>
          )}

          {/* Results table with expandable ssh_output rows */}
          {results.length > 0 ? (
            <div className="overflow-x-auto rounded-md border border-border-subtle">
              <table className="w-full text-sm" role="table" aria-label="Per-host enrollment progress">
                <thead>
                  <tr className="border-b border-border-subtle bg-surface/60">
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Host
                    </th>
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Port
                    </th>
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      SSH
                    </th>
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Duration
                    </th>
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Node ID
                    </th>
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Gate
                    </th>
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Error
                    </th>
                    <th className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                      Logs
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {results.map((result) => {
                    const isExpanded = expandedHosts.has(result.host);
                    const hasSshOutput = !!result.ssh_output;
                    return (
                      <Fragment key={result.id ?? result.host}>
                        <tr className="border-b border-border-subtle last:border-0 hover:bg-surface/40 transition-colors">
                          <td className="px-3 py-2.5">
                            <span className="font-medium text-foreground">{result.host}</span>
                          </td>
                          <td className="px-3 py-2.5">
                            <span className="text-text-secondary">{result.port || '—'}</span>
                          </td>
                          <td className="px-3 py-2.5">
                            {result.success ? (
                              <StatusTag tone="healthy">OK</StatusTag>
                            ) : (
                              <StatusTag tone="critical">FAIL</StatusTag>
                            )}
                          </td>
                          <td className="px-3 py-2.5">
                            <span className="text-text-secondary">{formatDuration(result.duration_ms)}</span>
                          </td>
                          <td className="px-3 py-2.5">
                            {result.node_id ? (
                              <a
                                href={`/nodes?id=${result.node_id}`}
                                className="font-mono text-xs text-brand-400 hover:underline"
                              >
                                {result.node_id.slice(0, 8)}…
                              </a>
                            ) : (
                              <span className="text-text-muted text-xs">—</span>
                            )}
                          </td>
                          <td className="px-3 py-2.5">{renderNodeGate(result.host)}</td>
                          <td className="max-w-[200px] px-3 py-2.5">
                            <span
                              className="text-xs text-text-muted truncate block"
                              title={result.error_message ?? ''}
                            >
                              {result.error_message ? result.error_message.slice(0, 80) : '—'}
                            </span>
                          </td>
                          <td className="px-3 py-2.5">
                            {hasSshOutput ? (
                              <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-7 gap-1 px-2 text-xs"
                                onClick={() => toggleHostExpand(result.host)}
                              >
                                {isExpanded ? (
                                  <ChevronDown className="h-3 w-3" />
                                ) : (
                                  <ChevronRight className="h-3 w-3" />
                                )}
                                Logs
                              </Button>
                            ) : (
                              <span className="text-text-muted text-xs">—</span>
                            )}
                          </td>
                        </tr>
                        {isExpanded && result.ssh_output && (
                          <tr className="bg-surface/30">
                            <td colSpan={8} className="px-4 py-3">
                              <ExpandableCode
                                label={`SSH output — ${result.host}`}
                                content={result.ssh_output}
                                defaultOpen
                              />
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              title="Awaiting results"
              description="Waiting for first host results…"
              icon={<Terminal />}
            />
          )}

          {showReadyNotice ? (
            <div className="flex items-center gap-2 rounded-md border border-state-healthy/30 bg-state-healthy/10 px-4 py-3 text-sm text-state-healthy">
              <CheckCircle2 className="h-4 w-4 shrink-0" />
              All hosts passed the enrollment gate and are now active.
            </div>
          ) : null}
        </Panel>
      ) : null}

      {/* ── Form panel — collapsible once a job is active ── */}
      <Panel
        padding="md"
        eyebrow="CONFIGURATION"
        title={
          <button
            type="button"
            className="flex items-center gap-2 text-left"
            onClick={() => setFormCollapsed((c) => !c)}
          >
            {formCollapsed ? (
              <ChevronRight className="h-4 w-4 shrink-0 text-text-muted" />
            ) : (
              <ChevronDown className="h-4 w-4 shrink-0 text-text-muted" />
            )}
            <span>{jobId ? 'Start another enrollment' : 'Configure hosts'}</span>
          </button>
        }
        toneAccent={jobId ? undefined : 'brand'}
      >
        {!formCollapsed && (
          <form onSubmit={handleSubmit} aria-label="fleet enrollment form" className="flex flex-col gap-3 pt-1">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="targets">
                Hosts (one per line —{' '}
                <code className="font-mono text-xs">host</code>,{' '}
                <code className="font-mono text-xs">host:port</code>, or{' '}
                <code className="font-mono text-xs">user@host:port</code>)
              </Label>
              <textarea
                id="targets"
                className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                rows={6}
                value={targetsRaw}
                onChange={(e) => setTargetsRaw(e.target.value)}
                placeholder={`10.0.1.5\nadmin@10.0.1.6:22\n# comments allowed`}
                aria-label="targets"
              />
              <p className="text-xs text-text-muted">{parsedTargets.length} target(s) parsed</p>
            </div>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="ssh-user">Default SSH user</Label>
                <Input
                  id="ssh-user"
                  type="text"
                  value={sshUser}
                  onChange={(e) => setSshUser(e.target.value)}
                  autoComplete="username"
                  aria-label="ssh user"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="parallelism">Parallelism</Label>
                <Input
                  id="parallelism"
                  type="number"
                  min={1}
                  max={50}
                  value={parallel}
                  onChange={(e) => setParallel(Math.max(1, Number.parseInt(e.target.value, 10) || 1))}
                  aria-label="parallelism"
                />
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between">
                <Label htmlFor="ssh-key">SSH private key (PEM)</Label>
                <FileUploadButton
                  accept=".pem,.key,text/plain"
                  label="Upload .pem"
                  onContent={(text) => setSshKey(text)}
                />
              </div>
              <textarea
                id="ssh-key"
                className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                rows={4}
                value={sshKey}
                onChange={(e) => setSshKey(e.target.value)}
                placeholder={`-----BEGIN OPENSSH PRIVATE KEY-----\n...`}
                aria-label="ssh private key"
                autoComplete="off"
              />
              <p className="text-xs text-text-muted">Key held in memory only — never persisted.</p>
            </div>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="ssh-password">SSH password (fallback)</Label>
                <Input
                  id="ssh-password"
                  type="password"
                  value={sshPassword}
                  onChange={(e) => setSshPassword(e.target.value)}
                  autoComplete="new-password"
                  aria-label="ssh password"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="enroll-token">Enrollment token</Label>
                <Input
                  id="enroll-token"
                  type="text"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="cot_…"
                  autoComplete="off"
                  aria-label="enrollment token"
                />
              </div>
            </div>

            <SelectField
              id="enroll-tenant"
              label="Tenant (optional, for reference)"
              value={tenantId}
              onChange={(e) => setTenantId(e.target.value)}
            >
              <option value="">— none —</option>
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </SelectField>

            {formError ? (
              <p className="text-sm text-state-critical" role="alert">
                {formError}
              </p>
            ) : null}
            {formSuccess ? (
              <p className="text-sm text-state-healthy" role="status">
                {formSuccess}
              </p>
            ) : null}

            <div className="flex items-center gap-2 pt-2">
              <Button type="submit" variant="primary" disabled={submitting}>
                {submitting ? 'Starting…' : 'Start fleet enrollment'}
              </Button>
            </div>
          </form>
        )}
      </Panel>
    </div>
  );
}
