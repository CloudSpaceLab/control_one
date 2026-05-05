import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, KeyRound, RefreshCw, Trash2 } from 'lucide-react';
import {
  Alert,
  Eyebrow,
  HealthGauge,
  KpiTile,
  Loader,
  Panel,
  SectionHeader,
  Sparkline,
  StatusTag,
} from '@/components/kit';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ConfirmModal } from '@/components/ConfirmModal';
import { RepairAgentDialog } from '@/components/nodes/RepairAgentDialog';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useApiClient } from '@/hooks/useApiClient';
import { useNode } from '@/hooks/useNode';
import { useToast } from '@/providers/ToastProvider';
import type {
  AgentUpdateResponse,
  NodeHealthRiskLevel,
  TelemetryMetric,
  UpdateNodePayload,
} from '@/lib/api';
import type { StateTone } from '@/components/kit/types';
import { formatTs } from '@/lib/format';

// agentLooksDead returns the reason the agent is not reporting, or null
// when the node is healthy. Used to gate the AgentRepairBanner so we
// don't nag operators on healthy nodes.
function agentLooksDead(node: import('@/lib/api').Node): { reason: string; tone: 'warning' | 'critical' } | null {
  if (node.state === 'enrollment_failed') {
    return { reason: 'Enrollment failed — the agent never completed its first call-home.', tone: 'critical' };
  }
  if (node.state === 'enrollment_pending' && !node.last_seen_at) {
    return { reason: 'Enrollment is pending — the agent has been registered but has never reported in.', tone: 'warning' };
  }
  if (node.state === 'retired') return null;
  if (!node.last_seen_at) {
    return { reason: 'Agent has never reported in.', tone: 'critical' };
  }
  // 10× the default 60s heartbeat interval = 10 min stale. Anything older
  // means the agent process is gone, the host is offline, or routing
  // broke. Either way, the operator should know.
  const lastSeen = new Date(node.last_seen_at).getTime();
  if (!Number.isFinite(lastSeen)) return null;
  const stale = Date.now() - lastSeen;
  if (stale > 10 * 60 * 1000) {
    const minutes = Math.round(stale / 60_000);
    return {
      reason: `Agent has not reported in for ${minutes} minute${minutes === 1 ? '' : 's'}.`,
      tone: stale > 60 * 60 * 1000 ? 'critical' : 'warning',
    };
  }
  return null;
}

function AgentRepairBanner({
  node,
  onRepair,
}: {
  node: import('@/lib/api').Node;
  onRepair: () => void;
}): JSX.Element | null {
  const dead = agentLooksDead(node);
  if (!dead) return null;
  return (
    <Alert
      variant={dead.tone}
      title="Agent is not checking in"
      actions={
        <Button variant="secondary" size="sm" onClick={onRepair}>
          Repair agent →
        </Button>
      }
    >
      {dead.reason}{' '}
      Telemetry, knowledge-graph and recommendations stay empty until the
      agent reconnects. Use Settings → Repair agent to generate a fresh
      install command an operator pastes on the host.
    </Alert>
  );
}

function riskTone(risk?: NodeHealthRiskLevel): StateTone {
  switch (risk) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
      return 'healthy';
    case 'calibrating':
    default:
      return 'unknown';
  }
}

function riskLabel(risk: NodeHealthRiskLevel | undefined, score: number, calibratingSamples?: number): string {
  if (!risk) return 'Unknown';
  if (risk === 'calibrating') return `Calibrating (${calibratingSamples ?? 0}/24 samples)`;
  return `${risk[0].toUpperCase()}${risk.slice(1)} · ${score}`;
}

function metricSeries(metrics: TelemetryMetric[], name: string): number[] {
  return metrics
    .filter((m) => m.metric_name === name)
    .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
    .map((m) => m.metric_value);
}

function latestValue(metrics: TelemetryMetric[], name: string): number | null {
  const series = metricSeries(metrics, name);
  return series.length ? series[series.length - 1] : null;
}

export function NodeDetail(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { showToast } = useToast();
  const { node, health, telemetry, loading, error, reload } = useNode(id);
  const [tab, setTab] = useState<'overview' | 'activity' | 'kg' | 'recommendations' | 'settings'>('overview');

  if (loading && !node) {
    return (
      <div className="flex flex-col gap-4 p-6">
        <Loader size="md" label="Loading node…" />
      </div>
    );
  }

  if (error || !node) {
    return (
      <div className="flex flex-col gap-4 p-6">
        <Alert variant="critical" title="Could not load node">
          {error?.message ?? 'Node not found.'}
        </Alert>
        <Button variant="secondary" size="md" onClick={() => navigate('/nodes')}>
          <ArrowLeft className="h-4 w-4" /> Back to nodes
        </Button>
      </div>
    );
  }

  const cpu = metricSeries(telemetry, 'cpu_usage_percent');
  const mem = metricSeries(telemetry, 'memory_used_percent');
  const disk = metricSeries(telemetry, 'disk_usage_percent');

  const cpuLatest = latestValue(telemetry, 'cpu_usage_percent');
  const memLatest = latestValue(telemetry, 'memory_used_percent');
  const diskLatest = latestValue(telemetry, 'disk_usage_percent');

  const cpuCount = latestValue(telemetry, 'cpu_count');
  const memTotal = latestValue(telemetry, 'memory_total_bytes');
  const diskTotal = latestValue(telemetry, 'disk_total_bytes');

  const calibratingSamples =
    health?.risk_level === 'calibrating'
      ? (health.components as { calibrating_samples?: number })?.calibrating_samples
      : undefined;

  const tone = riskTone(health?.risk_level);

  return (
    <div className="flex flex-col gap-5 px-4 py-6 sm:px-6 lg:px-8">
      <div className="flex items-start justify-between gap-3">
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <Link
              to="/nodes"
              className="inline-flex items-center gap-1 text-xs text-text-muted transition-colors hover:text-foreground"
            >
              <ArrowLeft className="h-3.5 w-3.5" /> Nodes
            </Link>
            <span className="text-text-muted">/</span>
            <span className="font-mono text-xs text-text-muted">{node.id}</span>
          </div>
          <SectionHeader
            eyebrow="FLEET · NODE DETAIL"
            title={node.hostname || 'Unnamed node'}
            description={`${node.os ?? '—'} · ${node.arch ?? '—'} · agent ${node.agent_version ?? '—'}`}
          />
          <div className="flex items-center gap-2">
            <StatusTag tone={tone}>{riskLabel(health?.risk_level, health?.score ?? 0, calibratingSamples)}</StatusTag>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigator.clipboard.writeText(node.id).then(() => showToast('Node ID copied', 'success'))}
            >
              Copy node id
            </Button>
            <Button variant="ghost" size="sm" onClick={reload}>
              <RefreshCw className="h-3.5 w-3.5" /> Refresh
            </Button>
          </div>
        </div>
      </div>

      <AgentRepairBanner node={node} onRepair={() => setTab('settings')} />

      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="activity">Activity</TabsTrigger>
          <TabsTrigger value="kg">Knowledge graph</TabsTrigger>
          <TabsTrigger value="recommendations">Recommendations</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="pt-4">
          <OverviewTab
            node={node}
            health={health}
            cpu={cpu}
            mem={mem}
            disk={disk}
            cpuLatest={cpuLatest}
            memLatest={memLatest}
            diskLatest={diskLatest}
            cpuCount={cpuCount}
            memTotal={memTotal}
            diskTotal={diskTotal}
            telemetryLoading={loading}
          />
        </TabsContent>
        <TabsContent value="activity" className="pt-4">
          <ActivityTab nodeId={node.id} tenantId={node.tenant_id} />
        </TabsContent>
        <TabsContent value="kg" className="pt-4">
          <KnowledgeGraphTab nodeId={node.id} />
        </TabsContent>
        <TabsContent value="recommendations" className="pt-4">
          <RecommendationsTab nodeId={node.id} tenantId={node.tenant_id} health={health} />
        </TabsContent>
        <TabsContent value="settings" className="pt-4">
          <SettingsTab
            node={node}
            onChanged={reload}
            onDeleted={() => navigate('/nodes')}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

interface OverviewProps {
  node: import('@/lib/api').Node;
  health: import('@/lib/api').NodeHealthScore | null;
  cpu: number[];
  mem: number[];
  disk: number[];
  cpuLatest: number | null;
  memLatest: number | null;
  diskLatest: number | null;
  cpuCount: number | null;
  memTotal: number | null;
  diskTotal: number | null;
  telemetryLoading: boolean;
}

function fmtBytes(bytes: number): string {
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(1)} TB`;
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(0)} MB`;
  return `${bytes} B`;
}

function OverviewTab({ node, health, cpu, mem, disk, cpuLatest, memLatest, diskLatest, cpuCount, memTotal, diskTotal, telemetryLoading }: OverviewProps) {
  const calibratingSamples =
    health?.risk_level === 'calibrating'
      ? (health.components as { calibrating_samples?: number })?.calibrating_samples
      : undefined;

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-[280px_1fr]">
      <Panel padding="md" eyebrow="HEALTH" title="Predictive score" toneAccent="brand">
        <div className="flex flex-col items-center gap-2">
          {health ? (
            <HealthGauge score={health.score} risk={health.risk_level} size="lg" />
          ) : (
            <Loader size="md" label="No score yet" />
          )}
          <p className="text-xs text-text-muted">
            {riskLabel(health?.risk_level, health?.score ?? 0, calibratingSamples)}
          </p>
          {health?.computed_at && (
            <p className="font-mono text-[0.65rem] text-text-muted">
              updated {formatTs(health.computed_at)}
            </p>
          )}
        </div>
      </Panel>

      <Panel padding="md" eyebrow="VITALS" title="Node summary">
        <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs sm:grid-cols-3">
          <Vital label="Hostname" value={node.hostname} mono />
          <Vital label="Tenant" value={node.tenant_id} mono />
          <Vital label="OS" value={`${node.os ?? '—'} · ${node.arch ?? '—'}`} />
          <Vital label="Public IP" value={node.public_ip ?? '—'} mono />
          <Vital label="Agent version" value={node.agent_version ?? '—'} mono />
          <Vital label="State" value={String(node.state)} />
          <Vital label="First scan" value={formatTs(node.first_scan_at)} />
          <Vital label="Last seen" value={formatTs(node.last_seen_at)} />
          <Vital label="CPU cores" value={cpuCount != null ? String(Math.round(cpuCount)) : '—'} />
          <Vital label="Total RAM" value={memTotal != null ? fmtBytes(memTotal) : '—'} />
          <Vital label="Disk size" value={diskTotal != null ? fmtBytes(diskTotal) : '—'} />
        </dl>
      </Panel>

      <Panel padding="md" eyebrow="CPU" title="Last 24h" className="lg:col-span-1">
        <KpiTile
          label="Latest"
          value={cpuLatest != null ? `${cpuLatest.toFixed(1)}%` : '—'}
          tone={cpuLatest != null && cpuLatest > 85 ? 'critical' : cpuLatest != null && cpuLatest > 60 ? 'warning' : 'healthy'}
        />
        <div className="mt-2">
          <Sparkline
            data={cpu}
            tone="brand"
            height={36}
            ariaLabel="CPU usage trend"
            loading={telemetryLoading && cpu.length === 0}
            emptyLabel="No CPU samples yet"
          />
        </div>
      </Panel>

      <Panel padding="md" eyebrow="MEMORY" title="Last 24h" className="lg:col-span-1">
        <KpiTile
          label="Latest"
          value={memLatest != null ? `${memLatest.toFixed(1)}%` : '—'}
          tone={memLatest != null && memLatest > 90 ? 'critical' : memLatest != null && memLatest > 75 ? 'warning' : 'healthy'}
        />
        <div className="mt-2">
          <Sparkline
            data={mem}
            tone="accent"
            height={36}
            ariaLabel="Memory usage trend"
            loading={telemetryLoading && mem.length === 0}
            emptyLabel="No memory samples yet"
          />
        </div>
      </Panel>

      <Panel padding="md" eyebrow="DISK" title="Last 24h" className="lg:col-span-1">
        <KpiTile
          label="Latest"
          value={diskLatest != null ? `${diskLatest.toFixed(1)}%` : '—'}
          tone={diskLatest != null && diskLatest > 90 ? 'critical' : diskLatest != null && diskLatest > 75 ? 'warning' : 'healthy'}
        />
        <div className="mt-2">
          <Sparkline
            data={disk}
            tone="healthy"
            height={36}
            ariaLabel="Disk usage trend"
            loading={telemetryLoading && disk.length === 0}
            emptyLabel="No disk samples yet"
          />
        </div>
      </Panel>
    </div>
  );
}

function Vital({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <Eyebrow>{label}</Eyebrow>
      <span className={`text-text-secondary ${mono ? 'font-mono text-[0.7rem]' : 'text-xs'}`}>{value}</span>
    </div>
  );
}

interface ActivityProps {
  nodeId: string;
  tenantId: string;
}

function ActivityTab({ nodeId, tenantId }: ActivityProps) {
  const api = useApiClient();
  const [audit, setAudit] = useState<import('@/lib/api').AuditLog[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .listAuditLogs({ tenant_id: tenantId, resource_type: 'node', resource_id: nodeId, limit: 25 })
      .then((resp) => {
        if (!cancelled) setAudit(resp.data);
      })
      .catch((e) => {
        if (!cancelled) setErr(e instanceof Error ? e.message : 'load failed');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [api, nodeId, tenantId]);

  return (
    <Panel padding="md" eyebrow="ACTIVITY" title="Recent audit events">
      {loading && <Loader label="Loading audit log…" />}
      {err && <Alert variant="critical">{err}</Alert>}
      {!loading && audit.length === 0 && (
        <p className="text-sm text-text-muted">No recent audit events for this node.</p>
      )}
      <ul className="flex flex-col divide-y divide-border-subtle">
        {audit.map((row) => (
          <li key={row.id} className="flex items-start gap-3 py-2 text-sm">
            <span className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
              {row.action}
            </span>
            <span className="flex-1 truncate text-text-secondary">
              {row.actor_type}{row.actor_id ? ` · ${row.actor_id}` : ''}
            </span>
            <span className="font-mono text-[0.65rem] text-text-muted">{formatTs(row.created_at)}</span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function KnowledgeGraphTab({ nodeId }: { nodeId: string }) {
  const api = useApiClient();
  const [services, setServices] = useState<import('@/lib/api').NodeService[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .listNodeServices(nodeId)
      .then((resp) => {
        if (!cancelled) setServices(resp.data ?? []);
      })
      .catch((e) => {
        if (!cancelled) setErr(e instanceof Error ? e.message : 'load failed');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [api, nodeId]);

  return (
    <Panel padding="md" eyebrow="KNOWLEDGE GRAPH" title="Listening services on this node">
      {loading && <Loader label="Loading services…" />}
      {err && <Alert variant="critical">{err}</Alert>}
      {!loading && services.length === 0 ? (
        <Alert variant="info" title="No services reported yet">
          The agent-side service collector lands in a follow-up. Once it ships,
          listening ports + service kinds + probed URLs for this node appear here
          and contribute to the per-tenant knowledge_graph.md.
        </Alert>
      ) : (
        <ul className="flex flex-col divide-y divide-border-subtle text-sm">
          {services.map((svc) => (
            <li key={svc.id} className="flex items-center gap-3 py-2">
              <span className="font-mono text-xs tabular-nums text-text-muted w-12">
                {svc.port}
              </span>
              <span className="flex-1">
                <span className="font-display font-semibold text-foreground">
                  {svc.service_kind}
                </span>
                <span className="ml-2 font-mono text-xs text-text-muted">
                  {svc.process}
                </span>
              </span>
              {svc.probe_title && (
                <span className="truncate text-xs text-text-secondary">{svc.probe_title}</span>
              )}
              {svc.probe_status != null && (
                <span className="font-mono text-[0.7rem] text-text-muted">
                  HTTP {svc.probe_status}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function RecommendationsTab({ nodeId, tenantId, health }: { nodeId: string; tenantId: string; health: import('@/lib/api').NodeHealthScore | null }) {
  const api = useApiClient();
  const [recs, setRecs] = useState<import('@/lib/api').Recommendation[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .listRecommendations(tenantId)
      .then((resp) => {
        if (cancelled) return;
        // Until the backend gains ?node_id=, filter client-side via evidence.
        const filtered = (resp.data ?? []).filter((r) => {
          const ev = (r.evidence as Record<string, unknown> | undefined) ?? {};
          return ev.node_id === nodeId || !ev.node_id;
        });
        setRecs(filtered);
      })
      .catch(() => {
        if (!cancelled) setRecs([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [api, nodeId, tenantId]);

  const breakdown = useMemo(() => {
    if (!health || !health.components) return [];
    const c = health.components as Record<string, unknown>;
    const b = (c.breakdown ?? c) as Record<string, unknown>;
    return Object.entries(b)
      .filter(([, v]) => typeof v === 'number')
      .map(([k, v]) => ({ key: k, penalty: v as number }))
      .sort((a, b) => a.penalty - b.penalty)
      .slice(0, 5);
  }, [health]);

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <Panel padding="md" eyebrow="AI · NODE HEALTH" title="Top contributing signals">
        {breakdown.length === 0 ? (
          <p className="text-sm text-text-muted">No predictive signals yet — keep telemetry flowing.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {breakdown.map((row) => (
              <li
                key={row.key}
                className="flex items-center justify-between rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm"
              >
                <span className="text-text-secondary">{row.key}</span>
                <span className="font-mono text-xs tabular-nums text-state-critical">
                  {row.penalty.toFixed(1)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <Panel padding="md" eyebrow="POSTURE" title="Recommendations for this node">
        {loading && <Loader label="Loading…" />}
        {!loading && recs.length === 0 && (
          <p className="text-sm text-text-muted">No open recommendations matched this node.</p>
        )}
        <ul className="flex flex-col gap-2">
          {recs.map((r) => (
            <li key={r.title} className="rounded-md border border-border-subtle bg-surface px-3 py-2">
              <p className="text-sm font-semibold text-foreground">{r.title}</p>
              <p className="text-xs text-text-secondary">{r.rationale}</p>
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  );
}

function SettingsTab({
  node,
  onChanged,
  onDeleted,
}: {
  node: import('@/lib/api').Node;
  onChanged: () => void;
  onDeleted: () => void;
}) {
  const api = useApiClient();
  const { showToast } = useToast();
  const [showUpdate, setShowUpdate] = useState(false);
  const [showRegen, setShowRegen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [agentUpdating, setAgentUpdating] = useState(false);

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <Panel padding="md" eyebrow="REPAIR / RE-ENROLL" title="Bring a drifted or dead agent back online">
        <p className="text-sm text-text-secondary">
          Two paths depending on what&apos;s wrong: update the node&apos;s metadata if the
          agent is alive but reporting stale config, or regenerate a fresh
          install command if the agent is dead / never enrolled / can&apos;t reach
          controlplane.
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button variant="secondary" size="md" onClick={() => setShowUpdate(true)}>
            Update settings…
          </Button>
          <Button variant="primary" size="md" onClick={() => setShowRegen(true)}>
            <KeyRound className="h-4 w-4" /> Repair agent…
          </Button>
        </div>
      </Panel>

      <Panel padding="md" eyebrow="AGENT" title="Agent maintenance">
        <p className="text-sm text-text-secondary">
          Trigger an agent update or retire / delete the node. Retire keeps the
          history; delete removes everything.
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button
            variant="secondary"
            size="md"
            disabled={agentUpdating}
            onClick={async () => {
              setAgentUpdating(true);
              try {
                const resp: AgentUpdateResponse = await api.updateAgent(node.id);
                showToast(`Agent update queued (job ${resp.job_id})`, 'success');
              } catch (err) {
                showToast(err instanceof Error ? err.message : 'agent update failed', 'error');
              } finally {
                setAgentUpdating(false);
              }
            }}
          >
            <RefreshCw className="h-4 w-4" /> Update agent
          </Button>
          <Button
            variant="ghost"
            size="md"
            className="text-state-critical hover:text-state-critical"
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 className="h-4 w-4" /> Delete node
          </Button>
        </div>
      </Panel>

      <UpdateSettingsDialog
        open={showUpdate}
        node={node}
        onOpenChange={setShowUpdate}
        onSaved={onChanged}
      />

      <RepairAgentDialog
        open={showRegen}
        node={node}
        onOpenChange={setShowRegen}
      />

      <ConfirmModal
        open={confirmDelete}
        onCancel={() => setConfirmDelete(false)}
        title="Delete this node?"
        body="History and telemetry tied to this node will be removed. This cannot be undone."
        confirmLabel="Delete node"
        variant="danger"
        onConfirm={async () => {
          await api.deleteNode(node.id);
          showToast('Node deleted', 'success');
          onDeleted();
        }}
      />
    </div>
  );
}

function UpdateSettingsDialog({
  open,
  node,
  onOpenChange,
  onSaved,
}: {
  open: boolean;
  node: import('@/lib/api').Node;
  onOpenChange: (b: boolean) => void;
  onSaved: () => void;
}) {
  const api = useApiClient();
  const { showToast } = useToast();
  const [hostname, setHostname] = useState(node.hostname ?? '');
  const [os, setOs] = useState(node.os ?? '');
  const [arch, setArch] = useState(node.arch ?? '');
  const [publicIp, setPublicIp] = useState(node.public_ip ?? '');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) {
      setHostname(node.hostname ?? '');
      setOs(node.os ?? '');
      setArch(node.arch ?? '');
      setPublicIp(node.public_ip ?? '');
    }
  }, [open, node]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const payload: UpdateNodePayload = {};
      if (hostname && hostname !== node.hostname) payload.hostname = hostname;
      if (os !== node.os) payload.os = os || undefined;
      if (arch !== node.arch) payload.arch = arch || undefined;
      if (publicIp !== node.public_ip) payload.public_ip = publicIp || undefined;
      await api.updateNode(node.id, payload);
      showToast('Settings updated · agent picks them up on next check-in', 'success');
      onSaved();
      onOpenChange(false);
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'update failed', 'error');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Update node settings</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <FormField label="Hostname">
              <Input value={hostname} onChange={(e) => setHostname(e.target.value)} />
            </FormField>
            <FormField label="Public IP">
              <Input value={publicIp} onChange={(e) => setPublicIp(e.target.value)} />
            </FormField>
            <FormField label="OS">
              <Input value={os} onChange={(e) => setOs(e.target.value)} />
            </FormField>
            <FormField label="Arch">
              <Input value={arch} onChange={(e) => setArch(e.target.value)} />
            </FormField>
          </div>
          <DialogFooter>
            <Button variant="ghost" type="button" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              {busy ? 'Saving…' : 'Save changes'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}


function FormField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <Label className="text-xs text-text-muted">{label}</Label>
      {children}
    </div>
  );
}

