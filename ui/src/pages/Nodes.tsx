import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import {
  Server, AlertTriangle, RefreshCw, LayoutGrid, List,
  Activity, Shield, ChevronRight, Globe, MapPin,
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
import { useApiClient } from '../hooks/useApiClient';
import { useToast } from '../providers/ToastProvider';
import type {
  AtRiskFleetResponse,
  NodeHealthRiskLevel,
  NodeHealthScore,
  NodeSummary,
} from '../lib/api';
import { WORLD_COUNTRY_PATHS, projectGeoCoordinates } from '../lib/worldMap';
import type { ColumnDef } from '@tanstack/react-table';

// ── Region types ───────────────────────────────────────────────────────────

type RegionKey = 'na' | 'sa' | 'eu' | 'afme' | 'apac' | 'oce' | 'unknown';

interface RegionMeta {
  label: string;
  lat: number;
  lon: number;
}

const REGIONS: Record<RegionKey, RegionMeta> = {
  na:      { label: 'N. America',  lat: 39, lon: -98 },
  sa:      { label: 'S. America',  lat: -15, lon: -58 },
  eu:      { label: 'Europe',      lat: 51, lon: 10 },
  afme:    { label: 'Africa / ME', lat: 6, lon: 20 },
  apac:    { label: 'Asia Pacific', lat: 28, lon: 104 },
  oce:     { label: 'Oceania',     lat: -25, lon: 134 },
  unknown: { label: 'Unknown',     lat: -52, lon: 164 },
};

// ── Region inference ───────────────────────────────────────────────────────

function normalizeRegionLabel(v: string): RegionKey {
  const s = v.toLowerCase();
  if (/\b(us|na|north.?am|canada|canad|us-.+|nyc|dal|atl|sfo|lax|chicago)\b/.test(s)) return 'na';
  if (/\b(sa|south.?am|brazil|latam|latin|sao)\b/.test(s)) return 'sa';
  if (/\b(eu|europe|uk|gb|de|fr|nl|ams|lon|fra|dub|ldn|ber|par|ire)\b/.test(s)) return 'eu';
  if (/\b(af|africa|me|middle.?east|uae|dxb|jed|riyadh|cairo)\b/.test(s)) return 'afme';
  if (/\b(ap|asia|apac|jp|sg|hk|tok|sin|seoul|mumbai|india|china|bay|bom|del)\b/.test(s)) return 'apac';
  if (/\b(au|nz|oceania|sydney|melbourne|auckland|syd|mel)\b/.test(s)) return 'oce';
  return 'unknown';
}

function guessRegion(node: NodeSummary): RegionKey {
  for (const key of ['region', 'datacenter', 'location', 'site', 'dc']) {
    const val = node.labels?.[key];
    if (typeof val === 'string') return normalizeRegionLabel(val);
  }

  const ip = node.public_ip;
  if (!ip) return 'unknown';
  const parts = ip.split('.');
  if (parts.length !== 4) return 'unknown';
  const [a, b] = parts.map(Number);

  if (a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)) return 'unknown';

  // Linode/Akamai
  if (a === 139 && b === 162) return 'eu'; // London
  if (a === 45 && (b === 33 || b === 56)) return 'na';
  if (a === 173 && b === 255) return 'na';
  if (a === 50 && b === 116) return 'na';
  if (a === 66 && b === 228) return 'na';

  // DigitalOcean
  if (a === 165 && (b === 227 || b === 232)) return 'na';
  if (a === 162 && b === 243) return 'na';
  if (a === 188 && b === 166) return 'eu';
  if (a === 178 && b === 62) return 'eu';
  if (a === 128 && b === 199) return 'apac';

  // AWS common
  if ([3, 13, 18, 34, 44, 52, 54].includes(a)) return 'na';
  // Azure / GCP EU
  if (a === 20 || a === 40) return 'eu';
  if (a === 35) return 'na';

  // Rough first-octet heuristics (last resort)
  if (a >= 1 && a <= 60) return 'apac';
  if (a >= 61 && a <= 80) return 'apac';
  if (a >= 81 && a <= 100) return 'eu';
  if (a >= 101 && a <= 130) return 'na';
  if (a >= 131 && a <= 165) return 'na';
  if (a >= 166 && a <= 180) return 'eu';
  if (a >= 181 && a <= 220) return 'sa';

  return 'unknown';
}

interface NodeMapPoint {
  node: NodeSummary;
  state: NodeState;
  x: number;
  y: number;
  lat: number;
  lon: number;
  precision: 'exact' | 'estimated';
  label: string;
}

function numericLabel(node: NodeSummary, keys: string[]): number | null {
  const labels = node.labels ?? {};
  for (const key of keys) {
    const raw = labels[key];
    if (typeof raw === 'number' && Number.isFinite(raw)) return raw;
    if (typeof raw === 'string') {
      const parsed = Number(raw);
      if (Number.isFinite(parsed)) return parsed;
    }
  }
  return null;
}

const CITY_COORDS: Record<string, { lat: number; lon: number; label: string }> = {
  london: { lat: 51.5072, lon: -0.1276, label: 'London' },
  lon: { lat: 51.5072, lon: -0.1276, label: 'London' },
  ldn: { lat: 51.5072, lon: -0.1276, label: 'London' },
  amsterdam: { lat: 52.3676, lon: 4.9041, label: 'Amsterdam' },
  ams: { lat: 52.3676, lon: 4.9041, label: 'Amsterdam' },
  frankfurt: { lat: 50.1109, lon: 8.6821, label: 'Frankfurt' },
  fra: { lat: 50.1109, lon: 8.6821, label: 'Frankfurt' },
  newyork: { lat: 40.7128, lon: -74.006, label: 'New York' },
  nyc: { lat: 40.7128, lon: -74.006, label: 'New York' },
  dallas: { lat: 32.7767, lon: -96.797, label: 'Dallas' },
  dal: { lat: 32.7767, lon: -96.797, label: 'Dallas' },
  sfo: { lat: 37.7749, lon: -122.4194, label: 'San Francisco' },
  lax: { lat: 34.0522, lon: -118.2437, label: 'Los Angeles' },
  singapore: { lat: 1.3521, lon: 103.8198, label: 'Singapore' },
  sg: { lat: 1.3521, lon: 103.8198, label: 'Singapore' },
  sin: { lat: 1.3521, lon: 103.8198, label: 'Singapore' },
  tokyo: { lat: 35.6762, lon: 139.6503, label: 'Tokyo' },
  tok: { lat: 35.6762, lon: 139.6503, label: 'Tokyo' },
  mumbai: { lat: 19.076, lon: 72.8777, label: 'Mumbai' },
  bom: { lat: 19.076, lon: 72.8777, label: 'Mumbai' },
  sydney: { lat: -33.8688, lon: 151.2093, label: 'Sydney' },
  syd: { lat: -33.8688, lon: 151.2093, label: 'Sydney' },
};

function cityKey(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]/g, '');
}

function coordinatesFromLabels(node: NodeSummary): { lat: number; lon: number; label: string; precision: 'exact' | 'estimated' } | null {
  const lat = numericLabel(node, ['lat', 'latitude', 'geo.lat', 'geo_lat']);
  const lon = numericLabel(node, ['lon', 'lng', 'longitude', 'geo.lon', 'geo.lng', 'geo_lon']);
  if (lat != null && lon != null && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180) {
    return { lat, lon, label: 'Coordinates', precision: 'exact' };
  }

  for (const key of ['city', 'location', 'site', 'datacenter', 'region', 'dc']) {
    const value = node.labels?.[key];
    if (typeof value !== 'string') continue;
    const coord = CITY_COORDS[cityKey(value)];
    if (coord) return { ...coord, precision: 'estimated' };
  }
  return null;
}

function coordinatesFromIP(node: NodeSummary): { lat: number; lon: number; label: string; precision: 'estimated' } | null {
  const ip = node.public_ip;
  if (!ip) return null;
  const parts = ip.split('.').map(Number);
  if (parts.length !== 4 || parts.some((part) => !Number.isFinite(part))) return null;
  const [a, b] = parts;

  if (a === 139 && b === 162) return { ...CITY_COORDS.london, precision: 'estimated' };
  if (a === 188 && b === 166) return { ...CITY_COORDS.amsterdam, precision: 'estimated' };
  if (a === 178 && b === 62) return { ...CITY_COORDS.frankfurt, precision: 'estimated' };
  if (a === 128 && b === 199) return { ...CITY_COORDS.singapore, precision: 'estimated' };
  return null;
}

interface IPGeoPoint {
  lat: number;
  lon: number;
  label: string;
}

function mapPointForNode(node: NodeSummary, state: NodeState, index: number, ipGeo?: IPGeoPoint | null): NodeMapPoint {
  const coords = coordinatesFromLabels(node)
    ?? (ipGeo ? { ...ipGeo, precision: 'exact' as const } : null)
    ?? coordinatesFromIP(node);
  const fallbackRegion = guessRegion(node);
  const lat = coords?.lat ?? REGIONS[fallbackRegion].lat;
  const lon = coords?.lon ?? REGIONS[fallbackRegion].lon;
  const projected = projectGeoCoordinates(lon, lat);
  const jitterAngle = index * 2.3999632297;
  const jitterRadius = coords?.precision === 'exact' ? Math.min(index % 4, 2) * 2 : 8 + (index % 5) * 3;
  const label = coords?.label ?? REGIONS[fallbackRegion].label;
  return {
    node,
    state,
    lat,
    lon,
    x: projected.x + Math.cos(jitterAngle) * jitterRadius,
    y: projected.y + Math.sin(jitterAngle) * jitterRadius,
    precision: coords?.precision ?? 'estimated',
    label,
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

// ── World Map SVG ──────────────────────────────────────────────────────────

interface WorldMapProps {
  regionData: Record<RegionKey, { count: number; state: NodeState }>;
  activeRegion: RegionKey | null;
  onRegionClick: (region: RegionKey | null) => void;
}

function WorldMap({ regionData, activeRegion, onRegionClick }: WorldMapProps) {
  const [hovered, setHovered] = useState<RegionKey | null>(null);

  const entries = (Object.entries(regionData) as [RegionKey, { count: number; state: NodeState }][])
    .filter(([, v]) => v.count > 0);

  return (
    <div className="relative w-full overflow-hidden rounded-lg bg-[#0a0f1a] border border-border-subtle">
      <svg
        viewBox="0 0 1000 480"
        preserveAspectRatio="xMidYMid meet"
        className="w-full"
        aria-label="Fleet world map"
      >
        {/* Subtle grid lines */}
        <defs>
          <pattern id="grid" width="100" height="80" patternUnits="userSpaceOnUse">
            <path d="M 100 0 L 0 0 0 80" fill="none" stroke="#1e293b" strokeWidth="0.5" opacity="0.6" />
          </pattern>
        </defs>
        <rect width="1000" height="480" fill="url(#grid)" />

        {/* Continent shapes */}
        {WORLD_COUNTRY_PATHS.map((country: { id: string; d: string }) => (
          <path
            key={country.id}
            d={country.d}
            fill="#1e2d3d"
            stroke="#2d4057"
            strokeWidth="0.65"
          />
        ))}

        {/* Region indicators */}
        {entries.map(([key, { count, state }]) => {
          const meta = REGIONS[key];
          const projected = projectGeoCoordinates(meta.lon, meta.lat);
          const color = STATE_COLOR[state];
          const isActive = activeRegion === key;
          const isHov = hovered === key;
          const radius = Math.max(18, Math.min(36, 12 + count * 3));

          return (
            <g
              key={key}
              transform={`translate(${projected.x},${projected.y})`}
              onClick={() => onRegionClick(isActive ? null : key)}
              onMouseEnter={() => setHovered(key)}
              onMouseLeave={() => setHovered(null)}
              style={{ cursor: 'pointer' }}
            >
              {/* Outer pulse ring */}
              {(state === 'critical' || state === 'degraded') && (
                <motion.circle
                  r={radius + 4}
                  fill="none"
                  stroke={color}
                  strokeWidth="1.5"
                  initial={{ scale: 0.8, opacity: 0.7 }}
                  animate={{ scale: [0.9, 1.6], opacity: [0.7, 0] }}
                  transition={{ duration: state === 'critical' ? 1.2 : 2, repeat: Infinity, ease: 'easeOut' }}
                />
              )}

              {/* Selection ring */}
              {(isActive || isHov) && (
                <circle
                  r={radius + 8}
                  fill="none"
                  stroke={color}
                  strokeWidth="1.5"
                  opacity="0.5"
                />
              )}

              {/* Main dot */}
              <circle
                r={radius}
                fill={color}
                fillOpacity="0.18"
                stroke={color}
                strokeWidth="1.5"
              />

              {/* Count label */}
              <text
                textAnchor="middle"
                dominantBaseline="central"
                fill={color}
                fontSize={count > 99 ? 11 : 13}
                fontWeight="700"
                fontFamily="monospace"
              >
                {count}
              </text>

              {/* Region label below */}
              {(isActive || isHov) && (
                <text
                  y={radius + 14}
                  textAnchor="middle"
                  fill="#cbd5e1"
                  fontSize="10"
                  fontWeight="600"
                >
                  {meta.label}
                </text>
              )}
            </g>
          );
        })}

        {/* Inactive regions (empty) — subtle label only on hover */}
        {entries.length === 0 && (
          <text x="500" y="240" textAnchor="middle" fill="#475569" fontSize="14">
            No nodes online
          </text>
        )}
      </svg>

      {/* Legend */}
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

// ── Node card ──────────────────────────────────────────────────────────────

function NodeWorldMap({
  points,
  onNodeClick,
}: {
  points: NodeMapPoint[];
  onNodeClick: (nodeId: string) => void;
}) {
  const [hovered, setHovered] = useState<string | null>(null);
  const exactCount = points.filter((point) => point.precision === 'exact').length;

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
                    {point.node.public_ip ?? 'no public ip'}
                  </text>
                  <text x="10" y="51" fill="#64748b" fontSize="9">
                    {point.label} - {point.precision}
                  </text>
                </g>
              )}
            </g>
          );
        })}

        {points.length === 0 && (
          <text x="500" y="240" textAnchor="middle" fill="#475569" fontSize="14">
            No nodes to plot
          </text>
        )}
      </svg>

      <div className="absolute left-3 top-3 flex items-center gap-2 rounded-md border border-border-subtle bg-black/60 px-3 py-1.5 backdrop-blur-sm">
        <MapPin className="h-3.5 w-3.5 text-brand-300" />
        <span className="text-[10px] font-medium uppercase tracking-[0.18em] text-text-muted">
          {points.length} nodes - {exactCount} exact
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
  tenantName: string;
  onClick: () => void;
}

function NodeCard({ node, health, tenantName, onClick }: NodeCardProps) {
  const state: NodeState = health
    ? riskToState(health.risk_level)
    : isOnline(node)
    ? 'healthy'
    : 'unknown';

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
        {node.public_ip && (
          <code className="font-mono text-[0.65rem] text-text-muted">{node.public_ip}</code>
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
        </div>
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-[0.6rem] text-text-muted truncate">{tenantName}</span>
        <StatusTag tone={STATE_TONE[state]}>
          {health ? (health.risk_level === 'calibrating' ? 'calibrating' : `${health.risk_level} · ${health.score}`) : state}
        </StatusTag>
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
    </motion.button>
  );
}

// ── Fleet group (tenant) row ───────────────────────────────────────────────

interface TenantGroupRowProps {
  tenantId: string;
  tenantName: string;
  nodes: NodeSummary[];
  healthMap: Record<string, NodeHealthScore | null>;
  activeRegion: RegionKey | null;
  onNodeClick: (nodeId: string) => void;
}

function TenantGroupRow({ tenantName, nodes, healthMap, onNodeClick }: TenantGroupRowProps) {
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

type ViewMode = 'overview' | 'table';

export function Nodes(): JSX.Element {
  const api = useApiClient();
  const navigate = useNavigate();
  const { showToast } = useToast();
  const { currentTenantId } = useTenant();

  const [view, setView] = useState<ViewMode>('overview');
  const [activeRegion, setActiveRegion] = useState<RegionKey | null>(null);
  const [hostnameFilter, setHostnameFilter] = useState('');
  const [healthMap, setHealthMap] = useState<Record<string, NodeHealthScore | null>>({});
  const [ipGeoMap, setIPGeoMap] = useState<Record<string, IPGeoPoint>>({});
  const [atRiskFleet, setAtRiskFleet] = useState<AtRiskFleetResponse | null>(null);
  const [agentUpdateNodeId, setAgentUpdateNodeId] = useState<string | null>(null);
  const [agentUpdating, setAgentUpdating] = useState(false);

  // Fetch all nodes with a generous limit for the overview
  const { data: nodes, loading, error, pagination, reload } = useNodes({ limit: 500 });
  const { data: fleetSnap, loading: snapLoading } = useFleetSummary({ tenantId: currentTenantId ?? undefined, intervalMs: 30_000 });
  const { data: tenants } = useTenants();

  const tenantNames = useMemo(() => {
    const m = new Map<string, string>();
    for (const t of tenants) m.set(t.id, t.name);
    return m;
  }, [tenants]);

  // Bulk-fetch per-node health scores
  useEffect(() => {
    let cancelled = false;
    if (nodes.length === 0) return;

    Promise.all(
      nodes.map((n) =>
        api.getNodeHealth(n.id)
          .then((score) => [n.id, score] as const)
          .catch(() => [n.id, null] as const),
      ),
    ).then((entries) => {
      if (cancelled) return;
      const next: Record<string, NodeHealthScore | null> = {};
      for (const [id, score] of entries) next[id] = score;
      setHealthMap(next);
    });

    return () => { cancelled = true; };
  }, [api, nodes]);

  // At-risk fleet
  useEffect(() => {
    let cancelled = false;
    api.listAtRiskNodes(undefined).then((r) => {
      if (!cancelled) setAtRiskFleet(r);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [api, nodes.length]);

  // Region grouping
  const regionData = useMemo((): Record<RegionKey, { count: number; state: NodeState }> => {
    const groups: Record<RegionKey, { nodes: NodeSummary[]; states: NodeState[] }> = {
      na: { nodes: [], states: [] }, sa: { nodes: [], states: [] },
      eu: { nodes: [], states: [] }, afme: { nodes: [], states: [] },
      apac: { nodes: [], states: [] }, oce: { nodes: [], states: [] },
      unknown: { nodes: [], states: [] },
    };

    for (const node of nodes) {
      const region = guessRegion(node);
      const h = healthMap[node.id];
      const state: NodeState = h
        ? riskToState(h.risk_level)
        : isOnline(node) ? 'healthy' : 'unknown';
      groups[region].nodes.push(node);
      groups[region].states.push(state);
    }

    const result = {} as Record<RegionKey, { count: number; state: NodeState }>;
    for (const [key, g] of Object.entries(groups) as [RegionKey, typeof groups[RegionKey]][]) {
      result[key] = { count: g.nodes.length, state: worstState(g.states) };
    }
    return result;
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

  // Filtered nodes for region / hostname
  const filteredNodes = useMemo(() => {
    let result = nodes;
    if (activeRegion) result = result.filter((n) => guessRegion(n) === activeRegion);
    if (hostnameFilter.trim()) {
      const q = hostnameFilter.trim().toLowerCase();
      result = result.filter((n) => n.hostname.toLowerCase().includes(q));
    }
    return result;
  }, [nodes, activeRegion, hostnameFilter]);

  const nodeMapPoints = useMemo(() => (
    filteredNodes.slice(0, 49).map((node, index) => {
      const h = healthMap[node.id];
      const state: NodeState = h
        ? riskToState(h.risk_level)
        : isOnline(node) ? 'healthy' : 'unknown';
      return mapPointForNode(node, state, index, node.public_ip ? ipGeoMap[node.public_ip] : null);
    })
  ), [filteredNodes, healthMap, ipGeoMap]);

  const useNodeMap = pagination.total > 0 && pagination.total < 50;

  useEffect(() => {
    let cancelled = false;
    if (!useNodeMap) return;

    const ips = Array.from(
      new Set(
        filteredNodes
          .slice(0, 49)
          .map((node) => node.public_ip)
          .filter((ip): ip is string => Boolean(ip)),
      ),
    ).filter((ip) => !ipGeoMap[ip]);

    if (ips.length === 0) return;

    Promise.all(
      ips.slice(0, 24).map(async (ip) => {
        try {
          const enrichment = await api.enrichIp(ip);
          const lat = enrichment.geo?.latitude;
          const lon = enrichment.geo?.longitude;
          if (lat == null || lon == null || !Number.isFinite(lat) || !Number.isFinite(lon)) {
            return [ip, null] as const;
          }
          const parts = [enrichment.geo?.city, enrichment.geo?.country].filter(Boolean);
          const label = parts.join(', ') || enrichment.geo?.country_code || 'IP geolocation';
          return [ip, { lat, lon, label }] as const;
        } catch {
          return [ip, null] as const;
        }
      }),
    ).then((entries) => {
      if (cancelled) return;
      setIPGeoMap((prev) => {
        const next = { ...prev };
        for (const [ip, geo] of entries) {
          if (geo) next[ip] = geo;
        }
        return next;
      });
    });

    return () => {
      cancelled = true;
    };
  }, [api, filteredNodes, ipGeoMap, useNodeMap]);

  // Filtered tenant groups (respects activeRegion + hostnameFilter)
  const filteredTenantGroups = useMemo(() => {
    const groups = new Map<string, NodeSummary[]>();
    for (const node of filteredNodes) {
      const list = groups.get(node.tenant_id) ?? [];
      list.push(node);
      groups.set(node.tenant_id, list);
    }
    return groups;
  }, [filteredNodes]);

  // Table columns
  type NodeRow = (typeof nodes)[number];
  const tableColumns: ColumnDef<NodeRow>[] = [
    {
      header: 'Status',
      id: 'status',
      cell: ({ row }) => {
        const h = healthMap[row.original.id];
        const state: NodeState = h ? riskToState(h.risk_level) : isOnline(row.original) ? 'healthy' : 'unknown';
        return <PulsingDot state={state} size={8} />;
      },
    },
    {
      header: 'Hostname',
      accessorKey: 'hostname',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.hostname}</span>,
    },
    {
      header: 'Tenant',
      accessorKey: 'tenant_id',
      cell: ({ row }) => <span className="text-text-secondary">{tenantNames.get(row.original.tenant_id) ?? row.original.tenant_id}</span>,
    },
    {
      header: 'Region',
      id: 'region',
      cell: ({ row }) => {
        const r = guessRegion(row.original);
        return <span className="text-text-muted text-xs">{REGIONS[r].label}</span>;
      },
    },
    {
      header: 'OS',
      accessorKey: 'os',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.os ?? '—'}</span>,
    },
    {
      header: 'Public IP',
      accessorKey: 'public_ip',
      cell: ({ row }) => <code className="font-mono text-xs text-text-secondary">{row.original.public_ip ?? '—'}</code>,
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
      header: 'Last seen',
      id: 'last_seen',
      cell: ({ row }) => <span className="text-xs text-text-muted">{formatLastSeen(row.original.last_seen_at)}</span>,
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex items-center gap-1">
          <Button type="button" variant="secondary" size="sm" onClick={() => navigate(`/nodes/${row.original.id}`)}>
            Open
          </Button>
          <Button type="button" variant="ghost" size="sm" title="Queue agent update" onClick={() => setAgentUpdateNodeId(row.original.id)}>
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
        </div>
      ),
    },
  ];

  const handleAgentUpdate = async () => {
    if (!agentUpdateNodeId) return;
    setAgentUpdating(true);
    try {
      await api.updateAgent(agentUpdateNodeId);
      showToast('Agent update queued.', 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to queue agent update.', 'error');
    } finally {
      setAgentUpdating(false);
      setAgentUpdateNodeId(null);
    }
  };

  const totals = fleetSnap?.totals;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="INFRASTRUCTURE · FLEET"
        title="Fleet Overview"
        description={`${pagination.total} agent${pagination.total === 1 ? '' : 's'} across ${tenantGroups.size} group${tenantGroups.size === 1 ? '' : 's'}`}
        actions={
          <div className="flex items-center gap-2">
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
          value={pagination.total}
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
          value={atRiskFleet?.total_count ?? '—'}
          tone={atRiskFleet && atRiskFleet.total_count > 0 ? 'critical' : 'unknown'}
          icon={<Shield />}
        />
      </div>

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

      {/* World map */}
      <Panel padding="md" eyebrow="GLOBAL DISTRIBUTION" toneAccent="brand"
        actions={
          <div className="flex items-center gap-1.5">
            {activeRegion && (
              <Button type="button" variant="ghost" size="sm" onClick={() => setActiveRegion(null)}>
                Clear filter
              </Button>
            )}
            <span className="text-xs text-text-muted">
              {useNodeMap
                ? 'Node markers open details'
                : activeRegion ? `Showing ${REGIONS[activeRegion].label}` : 'Click region to filter'}
            </span>
          </div>
        }
      >
        {useNodeMap ? (
          <NodeWorldMap
            points={nodeMapPoints}
            onNodeClick={(nodeId) => navigate(`/nodes/${nodeId}`)}
          />
        ) : (
          <WorldMap
            regionData={regionData}
            activeRegion={activeRegion}
            onRegionClick={setActiveRegion}
          />
        )}
        {/* Region chips */}
        <div className="flex flex-wrap gap-2 pt-1">
          {(Object.entries(regionData) as [RegionKey, { count: number; state: NodeState }][])
            .filter(([, v]) => v.count > 0)
            .sort(([, a], [, b]) => b.count - a.count)
            .map(([key, { count, state }]) => (
              <button
                key={key}
                type="button"
                onClick={() => setActiveRegion(activeRegion === key ? null : key)}
                className={`flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-all ${
                  activeRegion === key
                    ? 'border-brand-500/50 bg-brand-500/10 text-brand-300'
                    : 'border-border-subtle bg-surface-2 text-text-secondary hover:border-border-strong'
                }`}
              >
                <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: STATE_COLOR[state] }} />
                {REGIONS[key].label}
                <span className="font-mono text-text-muted">{count}</span>
              </button>
            ))}
        </div>
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
                onClick={() => setView('overview')}
                className="rounded-none border-0"
              >
                <LayoutGrid className="h-3.5 w-3.5" />
              </Button>
              <Button
                type="button"
                variant={view === 'table' ? 'primary' : 'ghost'}
                size="sm"
                onClick={() => setView('table')}
                className="rounded-none border-0 border-l border-border-subtle"
              >
                <List className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        }
      >
        {error && (
          <p className="text-sm text-state-critical" role="alert">Failed to load nodes: {error}</p>
        )}

        {view === 'overview' ? (
          filteredTenantGroups.size === 0 ? (
            <EmptyState
              title="No nodes"
              description={activeRegion ? `No nodes in ${REGIONS[activeRegion].label}` : 'No nodes match filters'}
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
                    activeRegion={activeRegion}
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
        body="The node agent will download the latest binary and restart on its next heartbeat cycle."
        confirmLabel={agentUpdating ? 'Queuing…' : 'Update agent'}
        onConfirm={handleAgentUpdate}
        onCancel={() => setAgentUpdateNodeId(null)}
      />
    </div>
  );
}
