import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import {
  Server, AlertTriangle, RefreshCw, LayoutGrid, List,
  Activity, Shield, ChevronRight, Globe, MapPin,
  Wifi, WifiOff,
} from 'lucide-react';
import { SectionHeader, Panel, KpiTile, StatusTag, EmptyState, DataTable, LiveBadge } from '../components/kit';
import type { StateTone } from '../components/kit/types';
import { Button } from '@/components/ui/button';
import { Input } from '../components/ui/input';
import { ConfirmModal } from '../components/ConfirmModal';
import { useTenant } from '@/providers/TenantProvider';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useFleetSummary } from '../hooks/useFleetSummary';
import { useJobs } from '../hooks/useJobs';
import { useApiClient } from '../hooks/useApiClient';
import { useToast } from '../providers/ToastProvider';
import type {
  AtRiskFleetResponse,
  Job,
  NetworkIsolationMode,
  NodeHealthRiskLevel,
  NodeHealthScore,
  NodeSummary,
} from '../lib/api';
import type { NodeState as LifecycleState } from '../lib/api';
import {
  agentUpdateStatusLabel,
  agentUpdateStatusTone,
  agentUpdateTargetVersion,
  latestAgentUpdateByNode,
} from '../lib/agentUpdateJobs';
import { WORLD_COUNTRY_PATHS, projectGeoCoordinates } from '../lib/worldMap';
import type { ColumnDef } from '@tanstack/react-table';

// ── Factual node geolocation ───────────────────────────────────────────────

interface NodeMapPoint {
  node: NodeSummary;
  state: NodeState;
  x: number;
  y: number;
  lat: number;
  lon: number;
  ip: string;
  label: string;
}

function primaryObservedIP(node: NodeSummary): string | null {
  const publicObservations = (node.network_observations ?? [])
    .filter((observation) => observation.kind === 'public_ip' && observation.value)
    .sort((a, b) => b.confidence - a.confidence);
  return publicObservations[0]?.value ?? node.public_ip ?? null;
}

function nodeCountryKey(node: NodeSummary): string | null {
  const geo = node.ip_geo;
  if (!geo) return null;
  const key = geo.country_code?.trim().toUpperCase() || geo.country?.trim();
  return key || null;
}

function nodeLocationLabel(node: NodeSummary): string | null {
  const geo = node.ip_geo;
  if (!geo) return null;
  const parts = [geo.city, geo.region, geo.country]
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value));
  const unique = parts.filter((value, index) => parts.indexOf(value) === index);
  return unique.join(', ') || geo.country_code?.trim() || null;
}

function mapPointForNode(node: NodeSummary, state: NodeState): NodeMapPoint | null {
  const geo = node.ip_geo;
  const lat = geo?.latitude;
  const lon = geo?.longitude;
  if (
    lat == null
    || lon == null
    || !Number.isFinite(lat)
    || !Number.isFinite(lon)
    || lat < -90
    || lat > 90
    || lon < -180
    || lon > 180
  ) {
    return null;
  }

  const projected = projectGeoCoordinates(lon, lat);
  return {
    node,
    state,
    lat,
    lon,
    x: projected.x,
    y: projected.y,
    ip: geo?.ip || primaryObservedIP(node) || '',
    label: nodeLocationLabel(node) ?? 'IP geolocation',
  };
}

// ── Health helpers ─────────────────────────────────────────────────────────

type NodeState = 'healthy' | 'warning' | 'degraded' | 'critical' | 'unknown';

const STATE_COLOR: Record<NodeState, string> = {
  healthy:  '#22c55e',
  warning:  '#eab308',
  degraded: '#f97316',
  critical: '#ef4444',
  unknown:  '#6b7280',
};

const STATE_TONE: Record<NodeState, StateTone> = {
  healthy:  'healthy',
  warning:  'warning',
  degraded: 'degraded',
  critical: 'critical',
  unknown:  'unknown',
};

function riskToState(risk: NodeHealthRiskLevel): NodeState {
  switch (risk) {
    case 'critical':    return 'critical';
    case 'high':        return 'degraded';
    case 'medium':      return 'warning';
    case 'low':         return 'healthy';
    case 'calibrating': return 'unknown';
    default:            return 'unknown';
  }
}

function riskTone(risk: NodeHealthRiskLevel): StateTone {
  return STATE_TONE[riskToState(risk)];
}

function isOnline(node: NodeSummary): boolean {
  if (!node.last_seen_at) return false;
  return Date.now() - new Date(node.last_seen_at).getTime() < 5 * 60 * 1000;
}

interface NodeIsolationView {
  mode: NetworkIsolationMode;
  active: boolean;
  expired: boolean;
  expiresAt?: string;
  hasAllowlist: boolean;
}

function nodeIsolationView(node: NodeSummary): NodeIsolationView {
  const rawMode = normalizeNodeIsolationMode(nodeLabelString(node, 'control_one.isolation.mode', 'connectivity_mode'));
  const expiresAt = nodeLabelString(node, 'control_one.isolation.expires_at', 'connectivity_mode_until') || undefined;
  const expiresAtMs = expiresAt ? new Date(expiresAt).getTime() : Number.NaN;
  const expired = Number.isFinite(expiresAtMs) && expiresAtMs <= Date.now();
  const mode = expired ? 'online' : rawMode;
  const hasAllowlist = nodeLabelList(node, 'control_one.isolation.allowed_applications', 'allowed_applications').length > 0
    || nodeLabelList(node, 'control_one.isolation.allowlist_cidrs', 'allowlist_cidrs').length > 0;
  return {
    mode,
    active: !expired && mode !== 'online',
    expired,
    expiresAt,
    hasAllowlist,
  };
}

function normalizeNodeIsolationMode(value: string): NetworkIsolationMode {
  switch (value.trim().toLowerCase()) {
    case 'airgap':
    case 'airgapped':
    case 'offline':
    case 'isolated':
    case 'local_only':
      return 'airgapped';
    case 'whitelist':
    case 'allowlist':
    case 'whitelist_only':
    case 'allowlist_only':
    case 'locked_down':
      return 'whitelist';
    default:
      return 'online';
  }
}

function nodeLabelString(node: NodeSummary, ...keys: string[]): string {
  for (const key of keys) {
    const value = node.labels?.[key];
    if (typeof value === 'string') return value;
    if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  }
  return '';
}

function nodeLabelList(node: NodeSummary, ...keys: string[]): string[] {
  for (const key of keys) {
    const value = node.labels?.[key];
    if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean);
    if (typeof value === 'string') return value.split(',').map((item) => item.trim()).filter(Boolean);
  }
  return [];
}

function nodeIsolationTone(isolation: NodeIsolationView): StateTone {
  if (isolation.expired) return 'warning';
  if (isolation.mode === 'airgapped') return 'healthy';
  if (isolation.mode === 'whitelist') return isolation.hasAllowlist ? 'healthy' : 'warning';
  return 'unknown';
}

function nodeIsolationLabel(isolation: NodeIsolationView): string {
  if (isolation.expired) return 'expired';
  if (isolation.mode === 'airgapped') return 'airgapped';
  if (isolation.mode === 'whitelist') return 'whitelist-only';
  return 'online';
}

function worstState(states: NodeState[]): NodeState {
  const order: NodeState[] = ['critical', 'degraded', 'warning', 'unknown', 'healthy'];
  for (const s of order) {
    if (states.includes(s)) return s;
  }
  return 'unknown';
}

function formatLastSeen(val?: string): string {
  if (!val) return 'never';
  const ago = Date.now() - new Date(val).getTime();
  if (ago < 60_000) return 'just now';
  if (ago < 3_600_000) return `${Math.floor(ago / 60_000)}m ago`;
  if (ago < 86_400_000) return `${Math.floor(ago / 3_600_000)}h ago`;
  return `${Math.floor(ago / 86_400_000)}d ago`;
}

function formatRelativeTime(val?: string): string {
  if (!val) return 'no timer';
  const date = new Date(val);
  if (Number.isNaN(date.getTime())) return val;
  const diff = date.getTime() - Date.now();
  const abs = Math.abs(diff);
  const value = abs < 60_000
    ? 'less than 1m'
    : abs < 3_600_000
      ? `${Math.floor(abs / 60_000)}m`
      : abs < 86_400_000
        ? `${Math.floor(abs / 3_600_000)}h`
        : `${Math.floor(abs / 86_400_000)}d`;
  return diff >= 0 ? `in ${value}` : `${value} ago`;
}

function formatAgentUpdateTime(job?: Job | null): string {
  if (!job) return 'Never queued';
  const status = (job.status ?? '').toLowerCase();
  const terminalAt = job.finished_at ?? job.updated_at ?? job.created_at;
  if (status === 'succeeded') return `Updated ${formatRelativeTime(terminalAt)}`;
  if (status === 'failed') return `Failed ${formatRelativeTime(terminalAt)}`;
  if (status === 'cancelled') return `Cancelled ${formatRelativeTime(terminalAt)}`;
  if (status === 'running') return `Started ${formatRelativeTime(job.started_at ?? job.updated_at ?? job.created_at)}`;
  if (job.scheduled_at) return `Scheduled ${formatRelativeTime(job.scheduled_at)}`;
  return `Queued ${formatRelativeTime(job.created_at)}`;
}

// ── Pulsing health dot ─────────────────────────────────────────────────────

function PulsingDot({ state, size = 10 }: { state: NodeState; size?: number }) {
  const color = STATE_COLOR[state];
  const shouldPulse = state === 'critical' || state === 'degraded';
  return (
    <span className="relative inline-flex items-center justify-center" style={{ width: size, height: size }}>
      {shouldPulse && (
        <motion.span
          className="absolute rounded-full"
          style={{ backgroundColor: color, width: size, height: size, opacity: 0.6 }}
          animate={{ scale: [1, 2.2, 1], opacity: [0.6, 0, 0] }}
          transition={{ duration: state === 'critical' ? 1.2 : 2, repeat: Infinity, ease: 'easeOut' }}
        />
      )}
      <span
        className="relative rounded-full"
        style={{ backgroundColor: color, width: size, height: size }}
      />
    </span>
  );
}

// ── Node world map ────────────────────────────────────────────────────────

function NodeWorldMap({
  points,
  totalCount,
  unlocatedCount,
  onNodeClick,
}: {
  points: NodeMapPoint[];
  totalCount: number;
  unlocatedCount: number;
  onNodeClick: (nodeId: string) => void;
}) {
  const [hovered, setHovered] = useState<string | null>(null);

  return (
    <div className="relative w-full overflow-hidden rounded-lg border border-border-subtle bg-[#080d18]">
      <svg
        viewBox="0 0 1000 480"
        preserveAspectRatio="xMidYMid meet"
        className="w-full"
        aria-label="Node-level world map"
      >
        <defs>
          <pattern id="node-map-grid" width="100" height="80" patternUnits="userSpaceOnUse">
            <path d="M 100 0 L 0 0 0 80" fill="none" stroke="#1e293b" strokeWidth="0.5" opacity="0.55" />
          </pattern>
          <radialGradient id="node-map-vignette" cx="50%" cy="45%" r="65%">
            <stop offset="0%" stopColor="#101827" stopOpacity="0" />
            <stop offset="100%" stopColor="#020617" stopOpacity="0.72" />
          </radialGradient>
        </defs>
        <rect width="1000" height="480" fill="url(#node-map-grid)" />
        {WORLD_COUNTRY_PATHS.map((country: { id: string; d: string }) => (
          <path key={country.id} d={country.d} fill="#172235" stroke="#30445e" strokeWidth="0.7" />
        ))}
        <rect width="1000" height="480" fill="url(#node-map-vignette)" />

        {points.map((point) => {
          const color = STATE_COLOR[point.state];
          const active = hovered === point.node.id;
          return (
            <g
              key={point.node.id}
              transform={`translate(${point.x},${point.y})`}
              onMouseEnter={() => setHovered(point.node.id)}
              onMouseLeave={() => setHovered(null)}
              onClick={() => onNodeClick(point.node.id)}
              style={{ cursor: 'pointer' }}
            >
              {(point.state === 'critical' || point.state === 'degraded') && (
                <motion.circle
                  r="12"
                  fill="none"
                  stroke={color}
                  strokeWidth="1.4"
                  initial={{ scale: 0.8, opacity: 0.75 }}
                  animate={{ scale: [0.8, 2.1], opacity: [0.75, 0] }}
                  transition={{ duration: point.state === 'critical' ? 1.2 : 2, repeat: Infinity, ease: 'easeOut' }}
                />
              )}
              <circle r={active ? 12 : 9} fill={color} fillOpacity="0.18" stroke={color} strokeWidth="1.5" />
              <circle r="3.2" fill={color} />
              {active && (
                <g transform="translate(16,-30)">
                  <rect width="190" height="58" rx="7" fill="#020617" fillOpacity="0.92" stroke="#334155" />
                  <text x="10" y="20" fill="#e2e8f0" fontSize="12" fontWeight="700">
                    {point.node.hostname}
                  </text>
                  <text x="10" y="37" fill="#94a3b8" fontSize="10" fontFamily="monospace">
                    {point.ip || 'no public IP'}
                  </text>
                  <text x="10" y="51" fill="#64748b" fontSize="9">
                    {point.label} · IP geolocation
                  </text>
                </g>
              )}
            </g>
          );
        })}

        {points.length === 0 && (
          <text x="500" y="240" textAnchor="middle" fill="#475569" fontSize="14">
            No factual IP locations available
          </text>
        )}
      </svg>

      <div className="absolute left-3 top-3 flex items-center gap-2 rounded-md border border-border-subtle bg-black/60 px-3 py-1.5 backdrop-blur-sm">
        <MapPin className="h-3.5 w-3.5 text-brand-300" />
        <span className="text-[10px] font-medium uppercase tracking-[0.18em] text-text-muted">
          {points.length} located of {totalCount}{unlocatedCount > 0 ? ` · ${unlocatedCount} unavailable` : ''}
        </span>
      </div>
      <div className="absolute bottom-3 right-3 flex items-center gap-3 rounded-md border border-border-subtle bg-black/60 px-3 py-1.5 backdrop-blur-sm">
        {(['healthy', 'warning', 'critical'] as NodeState[]).map((s) => (
          <span key={s} className="flex items-center gap-1.5 text-[10px] text-text-muted capitalize">
            <span className="h-2 w-2 rounded-full" style={{ backgroundColor: STATE_COLOR[s] }} />
            {s}
          </span>
        ))}
        <span className="flex items-center gap-1.5 text-[10px] text-text-muted">
          <span className="h-2 w-2 rounded-full" style={{ backgroundColor: STATE_COLOR.unknown }} />
          offline
        </span>
      </div>
    </div>
  );
}

interface NodeCardProps {
  node: NodeSummary;
  health: NodeHealthScore | null | undefined;
  agentJob?: Job;
  tenantName: string;
  onClick: () => void;
}

function NodeCard({ node, health, agentJob, tenantName, onClick }: NodeCardProps) {
  const state: NodeState = health
    ? riskToState(health.risk_level)
    : isOnline(node)
    ? 'healthy'
    : 'unknown';
  const observedIP = primaryObservedIP(node);

  return (
    <motion.button
      type="button"
      layout
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -8 }}
      onClick={onClick}
      className="group flex flex-col gap-3 rounded-lg border border-border-subtle bg-elevated p-4 text-left transition-all hover:border-border-strong hover:-translate-y-0.5 hover:shadow-lg focus:outline-none focus:ring-2 focus:ring-brand-500/40"
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <PulsingDot state={state} size={8} />
          <span className="font-medium text-foreground truncate text-sm">{node.hostname}</span>
        </div>
        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-text-muted opacity-0 transition-opacity group-hover:opacity-100" />
      </div>

      {/* Meta */}
      <div className="flex flex-col gap-1">
        {observedIP && (
          <div className="flex items-center gap-1.5 text-[0.65rem] text-text-muted">
            <span className="font-medium uppercase tracking-wider">Observed IP</span>
            <code className="font-mono">{observedIP}</code>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-1.5">
          {node.os && (
            <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[0.6rem] font-medium uppercase tracking-wider text-text-secondary">
              {node.os}
            </span>
          )}
          {node.arch && (
            <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[0.6rem] font-medium uppercase tracking-wider text-text-muted">
              {node.arch}
            </span>
          )}
          {node.target_type && node.target_type !== 'unknown' && (
            <span className="rounded bg-accent-400/15 px-1.5 py-0.5 text-[0.6rem] font-medium uppercase tracking-wider text-accent-300">
              {node.target_type.replace(/_/g, ' ')}
            </span>
          )}
          {node.reachability_mode && node.reachability_mode !== 'unknown' && (
            <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[0.6rem] font-medium tracking-wider text-text-muted">
              {node.reachability_mode.replace(/_/g, ' ')}
            </span>
          )}
        </div>
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-[0.6rem] text-text-muted truncate">{tenantName}</span>
        <div className="flex items-center gap-1.5">
          {(() => {
            const lifecycleState = node.state as LifecycleState;
            const lifecycleTone: StateTone = lifecycleState === 'active' ? 'healthy'
              : lifecycleState === 'enrollment_pending' ? 'warning'
              : lifecycleState === 'enrollment_failed' ? 'critical'
              : 'unknown';
            return (
              <StatusTag tone={lifecycleTone}>
                {String(lifecycleState).replace(/_/g, ' ')}
              </StatusTag>
            );
          })()}
          <StatusTag tone={STATE_TONE[state]}>
            {health ? (health.risk_level === 'calibrating' ? 'calibrating' : `${health.risk_level} · ${health.score}`) : state}
          </StatusTag>
        </div>
      </div>

      {/* Last seen */}
      <div className="flex items-center justify-between text-[0.6rem] text-text-muted -mt-1">
        <span className="flex items-center gap-1">
          <Activity className="h-2.5 w-2.5" />
          {formatLastSeen(node.last_seen_at)}
        </span>
        {node.agent_version && (
          <span className="font-mono">{node.agent_version}</span>
        )}
      </div>

      {agentJob && (
        <div className="flex items-center justify-between gap-2 border-t border-border-subtle pt-2 text-[0.6rem]">
          <span className="text-text-muted">Agent update</span>
          <StatusTag tone={agentUpdateStatusTone(agentJob.status)} title={formatAgentUpdateTime(agentJob)}>
            {agentUpdateStatusLabel(agentJob.status)}
          </StatusTag>
        </div>
      )}
    </motion.button>
  );
}

// ── Fleet group (tenant) row ───────────────────────────────────────────────

interface TenantGroupRowProps {
  tenantId: string;
  tenantName: string;
  nodes: NodeSummary[];
  healthMap: Record<string, NodeHealthScore | null>;
  agentJobsByNode: Map<string, Job>;
  onNodeClick: (nodeId: string) => void;
}

function TenantGroupRow({ tenantName, nodes, healthMap, agentJobsByNode, onNodeClick }: TenantGroupRowProps) {
  const [expanded, setExpanded] = useState(false);

  const states = nodes.map((n) => {
    const h = healthMap[n.id];
    return h ? riskToState(h.risk_level) : isOnline(n) ? 'healthy' : ('unknown' as NodeState);
  });

  const worst = worstState(states);
  const onlineCount = nodes.filter(isOnline).length;
  const critCount = states.filter((s) => s === 'critical' || s === 'degraded').length;

  const barSegments: { state: NodeState; count: number }[] = (['critical', 'degraded', 'warning', 'healthy', 'unknown'] as NodeState[])
    .map((s) => ({ state: s, count: states.filter((x) => x === s).length }))
    .filter((x) => x.count > 0);

  return (
    <div className="flex flex-col gap-0">
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="flex items-center gap-4 rounded-lg border border-border-subtle bg-elevated px-4 py-3 text-left transition-all hover:border-border-strong hover:bg-surface-2 focus:outline-none"
      >
        {/* Tenant info */}
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <PulsingDot state={worst} size={9} />
          <span className="font-medium text-foreground truncate">{tenantName}</span>
        </div>

        {/* Stats */}
        <div className="flex items-center gap-4 shrink-0">
          <span className="text-xs text-text-muted">
            <span className="font-mono font-semibold text-foreground">{onlineCount}</span>
            <span className="text-text-muted">/{nodes.length}</span>
            <span className="ml-1 text-text-muted">online</span>
          </span>

          {critCount > 0 && (
            <StatusTag tone="critical" icon={<AlertTriangle className="h-3 w-3" />}>
              {critCount} at risk
            </StatusTag>
          )}

          {/* Health bar */}
          <div className="hidden sm:flex h-2 w-24 overflow-hidden rounded-full bg-surface-2">
            {barSegments.map(({ state, count }) => (
              <div
                key={state}
                className="h-full transition-all"
                style={{
                  width: `${(count / nodes.length) * 100}%`,
                  backgroundColor: STATE_COLOR[state],
                }}
              />
            ))}
          </div>

          <ChevronRight
            className={`h-4 w-4 text-text-muted transition-transform ${expanded ? 'rotate-90' : ''}`}
          />
        </div>
      </button>

      <AnimatePresence>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="overflow-hidden"
          >
            <div className="grid grid-cols-2 gap-2 p-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
              {nodes.map((node) => (
                <NodeCard
                  key={node.id}
                  node={node}
                  health={healthMap[node.id]}
                  agentJob={agentJobsByNode.get(node.id)}
                  tenantName={tenantName}
                  onClick={() => onNodeClick(node.id)}
                />
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ── Main component ─────────────────────────────────────────────────────────

interface AgentUpdateRolloutPanelProps {
  nodes: NodeSummary[];
  jobsByNode: Map<string, Job>;
  loading: boolean;
  onQueue: (nodeId: string) => void;
}

function AgentUpdateRolloutPanel({ nodes, jobsByNode, loading, onQueue }: AgentUpdateRolloutPanelProps) {
  if (nodes.length === 0) return null;

  const rows = nodes
    .map((node) => {
      const job = jobsByNode.get(node.id);
      const status = (job?.status ?? 'not_queued').toLowerCase();
      const active = status === 'queued' || status === 'running';
      const failed = status === 'failed' || status === 'cancelled';
      const priority = active ? 0 : failed ? 1 : status === 'not_queued' ? 2 : 3;
      const updated = new Date(job?.updated_at ?? job?.created_at ?? 0).getTime() || 0;
      return { node, job, status, active, priority, updated };
    })
    .sort((a, b) => a.priority - b.priority || b.updated - a.updated || a.node.hostname.localeCompare(b.node.hostname));

  const activeCount = rows.filter((row) => row.active).length;
  const failedCount = rows.filter((row) => row.status === 'failed' || row.status === 'cancelled').length;
  const updatedCount = rows.filter((row) => row.status === 'succeeded').length;
  const neverQueuedCount = rows.filter((row) => row.status === 'not_queued').length;
  const visibleRows = rows.slice(0, 6);

  return (
    <Panel
      padding="md"
      eyebrow="AGENT ROLLOUT"
      title="Self-update status"
      toneAccent={failedCount > 0 ? 'critical' : activeCount > 0 ? 'warning' : 'brand'}
      actions={
        <StatusTag tone={loading ? 'unknown' : activeCount > 0 ? 'warning' : failedCount > 0 ? 'critical' : 'healthy'}>
          {loading ? 'checking' : activeCount > 0 ? `${activeCount} active` : 'idle'}
        </StatusTag>
      }
    >
      <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
        <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
          <div className="text-[0.6rem] uppercase tracking-[0.18em] text-text-muted">updated</div>
          <div className="mt-1 font-mono text-lg font-semibold text-state-healthy">{updatedCount}</div>
        </div>
        <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
          <div className="text-[0.6rem] uppercase tracking-[0.18em] text-text-muted">queued/running</div>
          <div className="mt-1 font-mono text-lg font-semibold text-state-warning">{activeCount}</div>
        </div>
        <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
          <div className="text-[0.6rem] uppercase tracking-[0.18em] text-text-muted">attention</div>
          <div className="mt-1 font-mono text-lg font-semibold text-state-critical">{failedCount}</div>
        </div>
        <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
          <div className="text-[0.6rem] uppercase tracking-[0.18em] text-text-muted">not queued</div>
          <div className="mt-1 font-mono text-lg font-semibold text-text-secondary">{neverQueuedCount}</div>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
        {visibleRows.map(({ node, job, active }) => {
          const target = agentUpdateTargetVersion(job);
          return (
            <div
              key={node.id}
              className="flex items-center justify-between gap-3 rounded-md border border-border-subtle bg-surface px-3 py-2"
            >
              <div className="min-w-0">
                <div className="truncate text-sm font-medium text-foreground">{node.hostname}</div>
                <div className="mt-0.5 flex flex-wrap items-center gap-2 text-[0.65rem] text-text-muted">
                  <span className="font-mono">agent {node.agent_version ?? '-'}</span>
                  {target && <span className="font-mono">target {target}</span>}
                  <span>{formatAgentUpdateTime(job)}</span>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <StatusTag tone={agentUpdateStatusTone(job?.status)}>
                  {agentUpdateStatusLabel(job?.status)}
                </StatusTag>
                <Button
                  type="button"
                  variant={active ? 'ghost' : 'secondary'}
                  size="sm"
                  disabled={active}
                  aria-label={`Queue agent update for ${node.hostname}`}
                  onClick={() => onQueue(node.id)}
                >
                  <RefreshCw className={`h-3.5 w-3.5 ${active ? 'animate-spin' : ''}`} />
                  Update
                </Button>
              </div>
            </div>
          );
        })}
      </div>

      {rows.length > visibleRows.length && (
        <div className="text-xs text-text-muted">
          Showing {visibleRows.length} of {rows.length} visible nodes.
        </div>
      )}
    </Panel>
  );
}

type ViewMode = 'overview' | 'table';

interface InlineActionState {
  message: string;
  tone: StateTone;
  busy?: boolean;
}

interface PendingIsolationAction {
  node: NodeSummary;
  currentlyAirgapped: boolean;
}

export function Nodes(): JSX.Element {
  const api = useApiClient();
  const navigate = useNavigate();
  const { showToast } = useToast();
  const { currentTenantId } = useTenant();

  const [view, setView] = useState<ViewMode>('overview');
  const [activeCountry, setActiveCountry] = useState<string | null>(null);
  const [hostnameFilter, setHostnameFilter] = useState('');
  const [healthMap, setHealthMap] = useState<Record<string, NodeHealthScore | null>>({});
  const [healthError, setHealthError] = useState<string | null>(null);
  const [atRiskFleet, setAtRiskFleet] = useState<AtRiskFleetResponse | null>(null);
  const [atRiskError, setAtRiskError] = useState<string | null>(null);
  const [agentUpdateNodeId, setAgentUpdateNodeId] = useState<string | null>(null);
  const [agentUpdateError, setAgentUpdateError] = useState<string | null>(null);
  const [agentUpdating, setAgentUpdating] = useState(false);
  const [isolationUpdatingId, setIsolationUpdatingId] = useState<string | null>(null);
  const [isolationAction, setIsolationAction] = useState<Record<string, InlineActionState>>({});
  const [pendingIsolation, setPendingIsolation] = useState<PendingIsolationAction | null>(null);
  const [confirmIsolationError, setConfirmIsolationError] = useState<string | null>(null);

  // Fetch nodes scoped to the active tenant. Without a tenant filter the
  // server returns every tenant's nodes, so callers must pass currentTenantId.
  const { data: nodes, loading, error, pagination, reload } = useNodes({
    tenantId: currentTenantId ?? undefined,
    limit: 500,
  });
  const { data: fleetSnap, loading: snapLoading } = useFleetSummary({ tenantId: currentTenantId ?? undefined, intervalMs: 30_000 });
  const { data: tenants } = useTenants();
  const {
    data: agentUpdateJobs,
    loading: agentUpdateJobsLoading,
    refresh: refreshAgentUpdateJobs,
  } = useJobs({
    tenantId: currentTenantId ?? undefined,
    type: 'agent.update',
    limit: 100,
    pollIntervalMs: 10_000,
  });

  const tenantNames = useMemo(() => {
    const m = new Map<string, string>();
    for (const t of tenants) m.set(t.id, t.name);
    return m;
  }, [tenants]);

  const latestAgentJobsByNode = useMemo(
    () => latestAgentUpdateByNode(agentUpdateJobs),
    [agentUpdateJobs],
  );

  // Bulk-fetch per-node health scores
  useEffect(() => {
    let cancelled = false;
    if (nodes.length === 0) {
      setHealthMap({});
      setHealthError(null);
      return () => { cancelled = true; };
    }
    setHealthError(null);

    Promise.all(
      nodes.map((n) =>
        api.getNodeHealth(n.id)
          .then((score) => ({ id: n.id, score, error: null as string | null }))
          .catch((err) => ({ id: n.id, score: null, error: errorMessage(err, 'Health score unavailable.') })),
      ),
    ).then((entries) => {
      if (cancelled) return;
      const next: Record<string, NodeHealthScore | null> = {};
      const failures = entries.filter((entry) => entry.error).length;
      for (const entry of entries) next[entry.id] = entry.score;
      setHealthMap(next);
      if (failures === entries.length) {
        setHealthError(`Node health scores could not be loaded: ${entries[0].error}`);
      } else if (failures > 0) {
        setHealthError(`${failures} node health ${failures === 1 ? 'score is' : 'scores are'} unavailable.`);
      }
    });

    return () => { cancelled = true; };
  }, [api, nodes]);

  // At-risk fleet. Omitting tenant_id is the server's explicit all-tenant scope.
  useEffect(() => {
    let cancelled = false;
    setAtRiskError(null);
    api.listAtRiskNodes(currentTenantId ?? undefined).then((response) => {
      if (!cancelled) setAtRiskFleet(response);
    }).catch((err) => {
      if (cancelled) return;
      setAtRiskFleet(null);
      setAtRiskError(errorMessage(err, 'At-risk fleet could not be loaded.'));
    });
    return () => { cancelled = true; };
  }, [api, currentTenantId]);

  // Country grouping is derived only from offline IP geolocation evidence.
  const countryData = useMemo(() => {
    const countries = new Map<string, { label: string; count: number; state: NodeState }>();
    for (const node of nodes) {
      const key = nodeCountryKey(node);
      if (!key) continue;
      const health = healthMap[node.id];
      const state: NodeState = health
        ? riskToState(health.risk_level)
        : isOnline(node) ? 'healthy' : 'unknown';
      const current = countries.get(key);
      countries.set(key, {
        label: node.ip_geo?.country?.trim() || node.ip_geo?.country_code?.trim() || key,
        count: (current?.count ?? 0) + 1,
        state: current ? worstState([current.state, state]) : state,
      });
    }
    return countries;
  }, [nodes, healthMap]);

  // Tenant grouping
  const tenantGroups = useMemo(() => {
    const groups = new Map<string, NodeSummary[]>();
    for (const node of nodes) {
      const list = groups.get(node.tenant_id) ?? [];
      list.push(node);
      groups.set(node.tenant_id, list);
    }
    return groups;
  }, [nodes]);

  // Filtered nodes for factual country / hostname.
  const filteredNodes = useMemo(() => {
    let result = nodes;
    if (activeCountry) result = result.filter((node) => nodeCountryKey(node) === activeCountry);
    if (hostnameFilter.trim()) {
      const query = hostnameFilter.trim().toLowerCase();
      result = result.filter((node) => node.hostname.toLowerCase().includes(query));
    }
    return result;
  }, [nodes, activeCountry, hostnameFilter]);

  const nodeMapPoints = useMemo(
    () => filteredNodes
      .map((node) => {
        const health = healthMap[node.id];
        const state: NodeState = health
          ? riskToState(health.risk_level)
          : isOnline(node) ? 'healthy' : 'unknown';
        return mapPointForNode(node, state);
      })
      .filter((point): point is NodeMapPoint => point !== null),
    [filteredNodes, healthMap],
  );
  const unlocatedNodeCount = filteredNodes.length - nodeMapPoints.length;

  // Filtered tenant groups (respects factual country + hostname filters)
  const filteredTenantGroups = useMemo(() => {
    const groups = new Map<string, NodeSummary[]>();
    for (const node of filteredNodes) {
      const list = groups.get(node.tenant_id) ?? [];
      list.push(node);
      groups.set(node.tenant_id, list);
    }
    return groups;
  }, [filteredNodes]);

  const requestQuickAirgap = (node: NodeSummary, isolation: NodeIsolationView) => {
    const currentlyAirgapped = isolation.active && isolation.mode === 'airgapped';
    setConfirmIsolationError(null);
    setPendingIsolation({ node, currentlyAirgapped });
  };

  const executeQuickAirgap = async () => {
    if (!pendingIsolation) return;
    const { node, currentlyAirgapped } = pendingIsolation;
    const actionLabel = currentlyAirgapped
      ? `Return ${node.hostname} online`
      : `Airgap ${node.hostname} for 1 hour`;
    setIsolationUpdatingId(node.id);
    setIsolationAction((current) => ({
      ...current,
      [node.id]: { message: `${actionLabel} updating...`, tone: 'warning', busy: true },
    }));
    try {
      await api.setNodeIsolation(
        node.id,
        currentlyAirgapped
          ? { mode: 'online' }
          : { mode: 'airgapped', duration_seconds: 60 * 60, reason: 'Operator quick airgap from fleet list' },
      );
      setIsolationAction((current) => ({
        ...current,
        [node.id]: { message: `${actionLabel} updated`, tone: 'healthy' },
      }));
      showToast(currentlyAirgapped ? `${node.hostname} returned online.` : `${node.hostname} airgapped for 1 hour.`, 'success');
      setPendingIsolation(null);
      reload();
    } catch (err) {
      const failure = `${actionLabel} failed: ${errorMessage(err, 'Failed to update network isolation.')}`;
      setIsolationAction((current) => ({
        ...current,
        [node.id]: { message: failure, tone: 'critical' },
      }));
      setConfirmIsolationError(failure);
    } finally {
      setIsolationUpdatingId(null);
    }
  };

  // Table columns
  type NodeRow = (typeof nodes)[number];
  const tableColumns: ColumnDef<NodeRow>[] = [
    {
      header: 'Health',
      id: 'status',
      cell: ({ row }) => {
        const h = healthMap[row.original.id];
        const state: NodeState = h ? riskToState(h.risk_level) : isOnline(row.original) ? 'healthy' : 'unknown';
        const healthLabel = state === 'healthy' ? 'Healthy' : state === 'unknown' ? 'Health unknown or awaiting telemetry' : String(state);
        return <span title={healthLabel}><PulsingDot state={state} size={8} /></span>;
      },
    },
    {
      header: 'Lifecycle (registered)',
      id: 'lifecycle',
      cell: ({ row }) => {
        const state = row.original.state as LifecycleState;
        const tone: StateTone = state === 'active' ? 'healthy'
          : state === 'enrollment_pending' ? 'warning'
          : state === 'enrollment_failed' ? 'critical'
          : state === 'retired' ? 'unknown'
          : 'unknown';
        return <StatusTag tone={tone}>{String(state).replace(/_/g, ' ')}</StatusTag>;
      },
    },
    {
      header: 'Hostname',
      accessorKey: 'hostname',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.hostname}</span>,
    },
    {
      header: 'Type',
      id: 'target_type',
      cell: ({ row }) => {
        const tt = row.original.target_type;
        if (!tt || tt === 'unknown') return <span className="text-text-muted">—</span>;
        return <span className="text-xs text-accent-300 capitalize">{tt.replace(/_/g, ' ')}</span>;
      },
    },
    {
      header: 'Reachability',
      id: 'reachability',
      cell: ({ row }) => {
        const rm = row.original.reachability_mode;
        if (!rm || rm === 'unknown') return <span className="text-text-muted">—</span>;
        return <span className="text-xs text-text-secondary capitalize">{rm.replace(/_/g, ' ')}</span>;
      },
    },
    {
      header: 'Tenant',
      accessorKey: 'tenant_id',
      cell: ({ row }) => <span className="text-text-secondary">{tenantNames.get(row.original.tenant_id) ?? row.original.tenant_id}</span>,
    },
    {
      header: 'Location',
      id: 'location',
      cell: ({ row }) => (
        <span className="text-text-muted text-xs">
          {nodeLocationLabel(row.original) ?? 'Location unavailable'}
        </span>
      ),
    },
    {
      header: 'OS',
      accessorKey: 'os',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.os ?? '—'}</span>,
    },
    {
      header: 'Observed IP',
      id: 'observed_ip',
      cell: ({ row }) => <code className="font-mono text-xs text-text-secondary">{primaryObservedIP(row.original) ?? '—'}</code>,
    },
    {
      header: 'Health',
      id: 'health',
      cell: ({ row }) => {
        const h = healthMap[row.original.id];
        if (!h) return <span className="text-text-muted">—</span>;
        return <StatusTag tone={riskTone(h.risk_level)}>{h.risk_level} · {h.score}</StatusTag>;
      },
    },
    {
      header: 'Isolation',
      id: 'isolation',
      cell: ({ row }) => {
        const isolation = nodeIsolationView(row.original);
        const timer = isolation.expiresAt ? `Expires ${formatRelativeTime(isolation.expiresAt)}` : 'No timer';
        return (
          <StatusTag tone={nodeIsolationTone(isolation)} title={timer}>
            {nodeIsolationLabel(isolation)}
          </StatusTag>
        );
      },
    },
    {
      header: 'Agent update',
      id: 'agent_update',
      cell: ({ row }) => {
        const job = latestAgentJobsByNode.get(row.original.id);
        return (
          <StatusTag tone={agentUpdateStatusTone(job?.status)} title={formatAgentUpdateTime(job)}>
            {agentUpdateStatusLabel(job?.status)}
          </StatusTag>
        );
      },
    },
    {
      header: 'Last seen',
      id: 'last_seen',
      cell: ({ row }) => <span className="text-xs text-text-muted">{formatLastSeen(row.original.last_seen_at)}</span>,
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const isolation = nodeIsolationView(row.original);
        const airgapped = isolation.active && isolation.mode === 'airgapped';
        const actionState = isolationAction[row.original.id];
        return (
          <div className="flex flex-col items-end gap-1">
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                aria-label={`Open ${row.original.hostname}`}
                onClick={() => navigate(`/nodes/${row.original.id}`)}
              >
                Open
              </Button>
              <Button
                type="button"
                variant={airgapped ? 'secondary' : 'ghost'}
                size="sm"
                title={airgapped ? 'Return online' : 'Airgap for 1 hour'}
                aria-label={airgapped ? `Return ${row.original.hostname} online` : `Airgap ${row.original.hostname} for 1 hour`}
                disabled={isolationUpdatingId === row.original.id || actionState?.busy}
                onClick={() => requestQuickAirgap(row.original, isolation)}
              >
                {airgapped ? <Wifi className="h-3.5 w-3.5" /> : <WifiOff className="h-3.5 w-3.5" />}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                title="Queue agent update"
                aria-label={`Queue agent update for ${row.original.hostname}`}
                onClick={() => {
                  setAgentUpdateError(null);
                  setAgentUpdateNodeId(row.original.id);
                }}
              >
                <RefreshCw className="h-3.5 w-3.5" />
              </Button>
            </div>
            {actionState && (
              <p
                className={`max-w-56 text-right text-[0.65rem] ${
                  actionState.tone === 'critical' ? 'text-state-critical' : 'text-text-muted'
                }`}
                role={actionState.tone === 'critical' ? 'alert' : undefined}
              >
                {actionState.message}
              </p>
            )}
          </div>
        );
      },
    },
  ];

  const handleAgentUpdate = async () => {
    if (!agentUpdateNodeId) return;
    setAgentUpdating(true);
    setAgentUpdateError(null);
    const targetNode = nodes.find((node) => node.id === agentUpdateNodeId);
    try {
      await api.updateAgent(agentUpdateNodeId);
      showToast('Agent update queued.', 'success');
      refreshAgentUpdateJobs();
      reload();
      setAgentUpdateNodeId(null);
    } catch (err) {
      setAgentUpdateError(
        `Agent update failed for ${targetNode?.hostname ?? agentUpdateNodeId}: ${errorMessage(err, 'Failed to queue agent update.')}`,
      );
    } finally {
      setAgentUpdating(false);
    }
  };

  const totals = fleetSnap?.totals;
  const nodesUnavailable = !loading && Boolean(error);
  const pendingAgentNode = agentUpdateNodeId ? nodes.find((node) => node.id === agentUpdateNodeId) : null;
  const pendingIsolationLabel = pendingIsolation
    ? pendingIsolation.currentlyAirgapped
      ? `Return ${pendingIsolation.node.hostname} online`
      : `Airgap ${pendingIsolation.node.hostname} for 1 hour`
    : '';

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="INFRASTRUCTURE"
        title="Nodes"
        description={
          nodesUnavailable
            ? 'Fleet data unavailable.'
            : `${pagination.total} node${pagination.total === 1 ? '' : 's'} across ${tenantGroups.size} group${tenantGroups.size === 1 ? '' : 's'}`
        }
        actions={
          <div className="flex items-center gap-2">
            <Button asChild type="button" variant="secondary" size="sm">
              <Link to="/onboard">
                <Server className="h-3.5 w-3.5" />
                Onboard
              </Link>
            </Button>
            <LiveBadge />
            <Button type="button" variant="ghost" size="sm" onClick={reload} disabled={loading}>
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            </Button>
          </div>
        }
      />

      {/* KPI tiles */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        <KpiTile
          label="Total nodes"
          value={nodesUnavailable ? '—' : pagination.total}
          tone="brand"
          icon={<Server />}
          loading={loading}
        />
        <KpiTile
          label="Healthy"
          value={totals?.healthy ?? '—'}
          tone="healthy"
          loading={snapLoading}
        />
        <KpiTile
          label="Warning"
          value={totals?.warning ?? '—'}
          tone="warning"
          loading={snapLoading}
        />
        <KpiTile
          label="Degraded"
          value={totals ? (totals.degraded + totals.critical) : '—'}
          tone="critical"
          icon={<AlertTriangle />}
          loading={snapLoading}
        />
        <KpiTile
          label="At risk"
          value={atRiskError ? '—' : atRiskFleet?.total_count ?? '—'}
          tone={atRiskFleet && atRiskFleet.total_count > 0 ? 'critical' : 'unknown'}
          icon={<Shield />}
        />
      </div>

      {healthError && (
        <Panel padding="md" toneAccent="warning" title="Node health scores degraded">
          <p className="text-sm text-state-warning" role="alert">{healthError}</p>
        </Panel>
      )}

      {atRiskError && (
        <Panel padding="md" toneAccent="critical" title="At-risk fleet unavailable">
          <p className="text-sm text-state-critical" role="alert">
            At-risk fleet could not be loaded: {atRiskError}
          </p>
        </Panel>
      )}

      <AgentUpdateRolloutPanel
        nodes={filteredNodes}
        jobsByNode={latestAgentJobsByNode}
        loading={agentUpdateJobsLoading}
        onQueue={(nodeId) => {
          setAgentUpdateError(null);
          setAgentUpdateNodeId(nodeId);
        }}
      />

      {/* At-risk alert banner */}
      {atRiskFleet && atRiskFleet.total_count > 0 && (
        <Panel padding="md" eyebrow="PREDICTIVE · AT-RISK FLEET" toneAccent="critical">
          <div className="flex flex-wrap items-center gap-3">
            <PulsingDot state="critical" size={10} />
            <span className="font-medium text-foreground">
              {atRiskFleet.total_count} node{atRiskFleet.total_count === 1 ? '' : 's'} require attention
            </span>
            <div className="flex items-center gap-2">
              {atRiskFleet.critical > 0 && (
                <StatusTag tone="critical" icon={<AlertTriangle className="h-3 w-3" />}>
                  {atRiskFleet.critical} critical
                </StatusTag>
              )}
              {atRiskFleet.high > 0 && (
                <StatusTag tone="degraded">{atRiskFleet.high} high</StatusTag>
              )}
            </div>
          </div>
          <div className="flex flex-wrap gap-2 mt-1">
            {atRiskFleet.data.map((n) => (
              <button
                key={n.node_id}
                type="button"
                onClick={() => navigate(`/nodes/${n.node_id}`)}
                className="flex items-center gap-2 rounded-md border border-border-subtle bg-surface-2 px-3 py-1.5 text-sm hover:border-state-critical/40 transition-colors"
              >
                <PulsingDot state={riskToState(n.risk_level)} size={7} />
                <span className="font-medium text-foreground">{n.hostname}</span>
                <StatusTag tone={riskTone(n.risk_level)}>{n.risk_level} · {n.score}</StatusTag>
              </button>
            ))}
          </div>
        </Panel>
      )}

      {/* Factual node map */}
      <Panel
        padding="md"
        eyebrow="NODE LOCATIONS"
        toneAccent="brand"
        actions={
          <div className="flex items-center gap-1.5">
            {activeCountry && (
              <Button type="button" variant="ghost" size="sm" onClick={() => setActiveCountry(null)}>
                Clear filter
              </Button>
            )}
            <span className="text-xs text-text-muted">Offline IP geolocation</span>
          </div>
        }
      >
        {nodesUnavailable ? (
          <EmptyState
            title="Node locations unavailable"
            description="Retry the node list."
            icon={<MapPin />}
          />
        ) : (
          <NodeWorldMap
            points={nodeMapPoints}
            totalCount={filteredNodes.length}
            unlocatedCount={unlocatedNodeCount}
            onNodeClick={(nodeId) => navigate(`/nodes/${nodeId}`)}
          />
        )}
        {!nodesUnavailable && (
          <div className="flex flex-wrap gap-2 pt-1">
            {[...countryData.entries()]
              .sort(([, a], [, b]) => b.count - a.count || a.label.localeCompare(b.label))
              .map(([key, { label, count, state }]) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setActiveCountry(activeCountry === key ? null : key)}
                  className={`flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-all ${
                    activeCountry === key
                      ? 'border-brand-500/50 bg-brand-500/10 text-brand-300'
                      : 'border-border-subtle bg-surface-2 text-text-secondary hover:border-border-strong'
                  }`}
                >
                  <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: STATE_COLOR[state] }} />
                  {label}
                  <span className="font-mono text-text-muted">{count}</span>
                </button>
              ))}
            {unlocatedNodeCount > 0 && (
              <span className="rounded-full border border-border-subtle bg-surface-2 px-3 py-1 text-xs text-text-muted">
                Location unavailable <span className="font-mono">{unlocatedNodeCount}</span>
              </span>
            )}
          </div>
        )}
      </Panel>

      {/* Fleet groups + nodes */}
      <Panel
        padding="md"
        eyebrow="FLEET GROUPS"
        toneAccent="brand"
        actions={
          <div className="flex items-center gap-2">
            <Input
              type="search"
              placeholder="Filter hostname…"
              value={hostnameFilter}
              onChange={(e) => setHostnameFilter(e.target.value)}
              className="h-8 w-48 text-sm"
            />
            <div className="flex rounded-md border border-border-subtle overflow-hidden">
              <Button
                type="button"
                variant={view === 'overview' ? 'primary' : 'ghost'}
                size="sm"
                aria-label="Show node cards"
                onClick={() => setView('overview')}
                className="rounded-none border-0"
              >
                <LayoutGrid className="h-3.5 w-3.5" />
              </Button>
              <Button
                type="button"
                variant={view === 'table' ? 'primary' : 'ghost'}
                size="sm"
                aria-label="Show node table"
                onClick={() => setView('table')}
                className="rounded-none border-0 border-l border-border-subtle"
              >
                <List className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        }
      >
        {nodesUnavailable ? (
          <div className="flex flex-col gap-3">
            <p className="text-sm text-state-critical" role="alert">Failed to load nodes: {error}</p>
            <EmptyState
              title="Node list unavailable"
              description="Retry the node list."
              icon={<Server />}
            />
          </div>
        ) : view === 'overview' ? (
          filteredTenantGroups.size === 0 ? (
            <EmptyState
              title="No nodes"
              description={activeCountry ? `No nodes in ${countryData.get(activeCountry)?.label ?? activeCountry}` : 'No nodes match filters'}
              icon={<Globe />}
            />
          ) : (
            <div className="flex flex-col gap-2">
              {[...filteredTenantGroups.entries()]
                .sort(([, a], [, b]) => b.length - a.length)
                .map(([tenantId, tenantNodes]) => (
                  <TenantGroupRow
                    key={tenantId}
                    tenantId={tenantId}
                    tenantName={tenantNames.get(tenantId) ?? tenantId}
                    nodes={tenantNodes}
                    healthMap={healthMap}
                    agentJobsByNode={latestAgentJobsByNode}
                    onNodeClick={(id) => navigate(`/nodes/${id}`)}
                  />
                ))}
            </div>
          )
        ) : (
          <>
            <DataTable
              columns={tableColumns}
              rows={filteredNodes}
              loading={loading}
              rowKey={(row) => row.id}
              empty={
                <EmptyState
                  title="No nodes"
                  description="No nodes match current filters."
                  icon={<Server />}
                />
              }
            />
            <div className="text-xs text-text-muted pt-1">
              Showing {filteredNodes.length} of {pagination.total} nodes
            </div>
          </>
        )}
      </Panel>

      <ConfirmModal
        open={agentUpdateNodeId !== null}
        title="Queue agent self-update?"
        body={
          pendingAgentNode
            ? `${pendingAgentNode.hostname} will download the latest binary and restart on its next heartbeat cycle.`
            : 'The node agent will download the latest binary and restart on its next heartbeat cycle.'
        }
        confirmLabel={agentUpdating ? 'Queuing…' : 'Update agent'}
        confirmDisabled={agentUpdating}
        cancelDisabled={agentUpdating}
        onConfirm={handleAgentUpdate}
        onCancel={() => {
          setAgentUpdateNodeId(null);
          setAgentUpdateError(null);
        }}
      >
        {agentUpdateError && (
          <p className="text-sm text-state-critical" role="alert">{agentUpdateError}</p>
        )}
      </ConfirmModal>

      <ConfirmModal
        open={pendingIsolation !== null}
        title="Change node isolation?"
        body={
          pendingIsolation?.currentlyAirgapped
            ? `${pendingIsolationLabel}. This returns the node to normal connectivity.`
            : `${pendingIsolationLabel}. This blocks non-control traffic for one hour from the fleet list.`
        }
        confirmLabel={isolationUpdatingId ? 'Updating…' : pendingIsolation?.currentlyAirgapped ? 'Return online' : 'Airgap node'}
        confirmDisabled={Boolean(isolationUpdatingId)}
        cancelDisabled={Boolean(isolationUpdatingId)}
        variant={pendingIsolation?.currentlyAirgapped ? 'default' : 'danger'}
        onConfirm={executeQuickAirgap}
        onCancel={() => {
          setPendingIsolation(null);
          setConfirmIsolationError(null);
        }}
      >
        {confirmIsolationError && (
          <p className="text-sm text-state-critical" role="alert">{confirmIsolationError}</p>
        )}
      </ConfirmModal>
    </div>
  );
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}
