import { useMemo } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import {
  AlertTriangle,
  CheckCircle2,
  CircleSlash,
  RefreshCw,
  ShieldCheck,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  EmptyState,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '@/components/kit';
import {
  CoverageTruthBadge,
  CoverageTruthList,
  summarizeCoverageRows,
} from '@/components/coverage/CoverageTruth';
import { useCoverageMatrix } from '@/hooks/useCoverageMatrix';
import { useTenant } from '@/providers/TenantProvider';
import { cn } from '@/lib/utils';
import type { CoverageDomain, CoverageMatrixRow, CoverageState } from '@/lib/api';

type DomainFilter = CoverageDomain | 'all';

interface DomainGuidance {
  label: string;
  owner: string;
  milestone: string;
  next: string;
  route: string;
}

const DOMAIN_ORDER = [
  'telemetry',
  'parser',
  'detection',
  'compliance',
  'remediation',
  'vulnerability',
  'posture',
  'ai',
  'cases',
  'db_audit',
  'agent',
] as const;

const DOMAIN_GUIDANCE: Record<string, DomainGuidance> = {
  telemetry: {
    label: 'Telemetry',
    owner: 'Data plane',
    milestone: 'M167-2',
    next: 'Keep tenant freshness, replay, and source-row visibility explicit.',
    route: '/telemetry',
  },
  parser: {
    label: 'Parser',
    owner: 'Data plane',
    milestone: 'M167-1',
    next: 'Split raw-only sources from typed parser contracts before detections depend on them.',
    route: '/investigate',
  },
  detection: {
    label: 'Detection',
    owner: 'SOC content',
    milestone: 'M167-6',
    next: 'Attach each rule and baseline to required sources, parser state, MITRE mapping, and disposition health.',
    route: '/rules',
  },
  compliance: {
    label: 'Compliance',
    owner: 'Assurance',
    milestone: 'M167-7',
    next: 'Persist normalized evidence and keep manual evidence separate from automated pass states.',
    route: '/compliance',
  },
  remediation: {
    label: 'Remediation',
    owner: 'Automation',
    milestone: 'M167-8',
    next: 'Map each action to a typed runbook, safety class, policy gate, and verification receipt.',
    route: '/security/network',
  },
  vulnerability: {
    label: 'Vulnerability',
    owner: 'Patch risk',
    milestone: 'M167-9',
    next: 'Require CVE, package, installed version, fixed version, feed source, and verification evidence.',
    route: '/infrastructure/patch',
  },
  posture: {
    label: 'Posture',
    owner: 'Fleet policy',
    milestone: 'M167-4',
    next: 'Show desired, observed, inherited, drifted, and rollback states for every applied template.',
    route: '/control-room',
  },
  ai: {
    label: 'AI',
    owner: 'Investigation',
    milestone: 'M167-3',
    next: 'Keep answers cited to raw rows, normalized events, evidence, policies, posture versions, and receipts.',
    route: '/ask',
  },
  cases: {
    label: 'Cases',
    owner: 'SOC workflow',
    milestone: 'M167-10',
    next: 'Promote linked evidence, timelines, notes, actions, and exports into a first-class case workspace.',
    route: '/control-room',
  },
  db_audit: {
    label: 'DB audit',
    owner: 'Database security',
    milestone: 'M167-5',
    next: 'Expose missing-access, least-privilege grants, engine coverage, query evidence, and side-channel states.',
    route: '/investigate',
  },
  agent: {
    label: 'Agent',
    owner: 'Endpoint capability',
    milestone: 'M167-4',
    next: 'Track enforcement capability, policy receipts, drift events, and upgrade blockers by fleet group.',
    route: '/nodes',
  },
};

const ATTENTION_STATES = new Set([
  'partial',
  'raw_only',
  'unsupported',
  'manual_evidence',
  'stale',
  'exception',
  'unknown',
]);

const STATE_PRIORITY: Record<string, number> = {
  unsupported: 0,
  stale: 1,
  manual_evidence: 2,
  partial: 3,
  raw_only: 4,
  exception: 5,
  unknown: 6,
  not_applicable: 7,
  supported: 8,
};

const EMPTY_COVERAGE_ROWS: CoverageMatrixRow[] = [];

export function Coverage(): JSX.Element {
  const [params, setParams] = useSearchParams();
  const { currentTenant, currentTenantId } = useTenant();
  const selectedDomain = normalizeDomain(params.get('domain'));
  const {
    data,
    loading,
    error,
    unavailable,
    reload,
  } = useCoverageMatrix({ tenantId: currentTenantId ?? undefined });

  const rows = data?.rows ?? EMPTY_COVERAGE_ROWS;
  const domainOptions = useMemo(() => buildDomainOptions(rows, data?.domains), [rows, data?.domains]);
  const visibleRows = useMemo(() => {
    if (selectedDomain === 'all') return rows;
    return rows.filter((row) => normalizeToken(row.domain) === selectedDomain);
  }, [rows, selectedDomain]);
  const summary = useMemo(() => summarizeCoverageRows(visibleRows), [visibleRows]);
  const attentionRows = useMemo(
    () =>
      visibleRows
        .filter((row) => ATTENTION_STATES.has(rowStateKey(row)))
        .sort(compareAttentionRows)
        .slice(0, 10),
    [visibleRows],
  );
  const selectedGuidance = selectedDomain === 'all' ? null : guidanceForDomain(selectedDomain);

  const setDomain = (domain: DomainFilter) => {
    const next = new URLSearchParams(params);
    if (domain === 'all') next.delete('domain');
    else next.set('domain', domain);
    setParams(next, { replace: true });
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="COVERAGE TRUTH"
        title="Coverage"
        description="Tenant-scoped capability truth across telemetry, parser, detection, compliance, remediation, vulnerability, posture, AI, and cases."
        actions={
          <div className="flex items-center gap-2">
            {data?.catalog_version && (
              <StatusTag tone="info" variant="outline">
                {data.catalog_version}
              </StatusTag>
            )}
            <Button type="button" variant="secondary" size="md" onClick={reload} disabled={loading}>
              <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
              Refresh
            </Button>
          </div>
        }
      />

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <Panel
          padding="md"
          eyebrow={currentTenant?.name ?? 'CURRENT TENANT'}
          title="Coverage matrix"
          actions={
            <StatusTag tone={attentionRows.length > 0 ? 'warning' : 'healthy'}>
              {attentionRows.length} attention state{attentionRows.length === 1 ? '' : 's'}
            </StatusTag>
          }
        >
          <DomainFilterBar
            options={domainOptions}
            selected={selectedDomain}
            onSelect={setDomain}
          />
          <CoverageTruthList
            rows={visibleRows}
            loading={loading}
            error={error}
            unavailable={unavailable}
            generatedAt={data?.generated_at}
            maxRows={80}
          />
        </Panel>

        <div className="flex flex-col gap-5">
          <Panel padding="md" eyebrow="PASS CRITERIA" title="Truth rules" toneAccent="warning">
            <div className="flex flex-col gap-3 text-sm text-text-secondary">
              <TruthRule
                icon={<CheckCircle2 className="h-4 w-4" />}
                tone="healthy"
                title="Supported"
                detail="First-party path exists and can be counted as covered for applicable rows."
              />
              <TruthRule
                icon={<AlertTriangle className="h-4 w-4" />}
                tone="warning"
                title="Partial or raw-only"
                detail="Visible capability exists, but source, parser, evidence, or workflow guarantees are incomplete."
              />
              <TruthRule
                icon={<CircleSlash className="h-4 w-4" />}
                tone="critical"
                title="Unsupported and N/A"
                detail="Unsupported and not-applicable rows are never counted as passing."
              />
            </div>
          </Panel>

          <Panel padding="md" eyebrow="SCOPE" title={data?.scope === 'global' ? 'Global catalog' : 'Tenant overlay'}>
            <div className="grid grid-cols-2 gap-3">
              <MetricCell label="Applicable" value={summary.applicable} tone="info" />
              <MetricCell label="Passing" value={summary.supported} tone="healthy" />
              <MetricCell label="Unsupported" value={summary.unsupported} tone={summary.unsupported > 0 ? 'critical' : 'healthy'} />
              <MetricCell label="Stale" value={summary.stale} tone={summary.stale > 0 ? 'degraded' : 'healthy'} />
            </div>
            <p className="text-xs text-text-muted">
              {data?.scope === 'global'
                ? 'Global view shows product capability claims without tenant freshness overlays.'
                : 'Tenant view includes live overlays where the backend can prove current fleet state.'}
            </p>
          </Panel>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <Panel padding="md" eyebrow="#167 ATTENTION" title="Coverage attention queue">
          {attentionRows.length > 0 ? (
            <div className="flex flex-col gap-2">
              {attentionRows.map((row, index) => (
                <AttentionRow key={coverageRowKey(row, index)} row={row} />
              ))}
            </div>
          ) : (
            <EmptyState
              icon={<ShieldCheck />}
              title="No attention states"
              description="The selected coverage slice has no partial, raw-only, stale, manual, exception, or unsupported rows."
            />
          )}
        </Panel>

        <Panel
          padding="md"
          eyebrow={selectedGuidance?.milestone ?? '#167 ROLLOUT'}
          title={selectedGuidance?.label ?? 'Milestone map'}
        >
          {selectedGuidance ? (
            <GuidanceDetail guidance={selectedGuidance} />
          ) : (
            <div className="flex flex-col gap-3">
              {DOMAIN_ORDER.slice(0, 9).map((domain) => (
                <MilestoneRow key={domain} guidance={guidanceForDomain(domain)} />
              ))}
            </div>
          )}
        </Panel>
      </div>
    </div>
  );
}

function DomainFilterBar({
  options,
  selected,
  onSelect,
}: {
  options: Array<{ value: DomainFilter; label: string; count?: number }>;
  selected: string;
  onSelect: (value: DomainFilter) => void;
}): JSX.Element {
  return (
    <div className="flex flex-wrap gap-2" role="group" aria-label="Coverage domain filter">
      {options.map((option) => {
        const active = normalizeToken(option.value) === selected;
        return (
          <Button
            key={option.value}
            type="button"
            variant="secondary"
            size="sm"
            aria-pressed={active}
            onClick={() => onSelect(option.value)}
            className={cn(
              'h-8 border-border-subtle px-2.5',
              active && 'border-brand-500/60 bg-brand-500/10 text-foreground',
            )}
          >
            <span>{option.label}</span>
            {typeof option.count === 'number' && (
              <span className="font-mono text-[0.65rem] text-text-muted">{option.count}</span>
            )}
          </Button>
        );
      })}
    </div>
  );
}

function TruthRule({
  icon,
  tone,
  title,
  detail,
}: {
  icon: JSX.Element;
  tone: StateTone;
  title: string;
  detail: string;
}): JSX.Element {
  return (
    <div className="flex gap-3">
      <span className={cn('mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-surface-2', toneTextClass(tone))}>
        {icon}
      </span>
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        <p className="text-xs leading-5 text-text-secondary">{detail}</p>
      </div>
    </div>
  );
}

function MetricCell({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: StateTone;
}): JSX.Element {
  return (
    <div
      className="rounded-md border border-border-subtle bg-surface p-3"
      aria-label={`${label}: ${value}`}
    >
      <div className="font-mono text-[0.62rem] uppercase tracking-wider text-text-muted">{label}</div>
      <div className={cn('mt-1 font-mono text-2xl font-semibold tabular-nums', toneTextClass(tone))}>
        {value}
      </div>
    </div>
  );
}

function AttentionRow({ row }: { row: CoverageMatrixRow }): JSX.Element {
  const domain = normalizeToken(row.domain);
  const guidance = guidanceForDomain(domain);
  const title = row.title || row.name || row.id || 'Untitled coverage row';
  const next = row.gaps?.[0] || guidance.next;
  const detail = row.reason || row.description || row.details || next;

  return (
    <div className="grid gap-3 rounded-md border border-border-subtle bg-surface p-3 lg:grid-cols-[1fr_auto]">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <StatusTag tone="info" variant="outline">
            {guidance.label}
          </StatusTag>
          <CoverageTruthBadge state={rowCoverageState(row)} />
          <span className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{guidance.milestone}</span>
        </div>
        <h3 className="mt-2 text-sm font-medium text-foreground">{title}</h3>
        <p className="mt-1 line-clamp-2 text-xs leading-5 text-text-secondary">{detail}</p>
        <p className="mt-1 text-xs leading-5 text-text-muted">Next: {next}</p>
      </div>
      <Button asChild variant="secondary" size="sm" className="self-start">
        <Link to={guidance.route}>Open</Link>
      </Button>
    </div>
  );
}

function GuidanceDetail({ guidance }: { guidance: DomainGuidance }): JSX.Element {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2">
        <StatusTag tone="info" variant="outline">{guidance.owner}</StatusTag>
        <StatusTag tone="warning" variant="outline">{guidance.milestone}</StatusTag>
      </div>
      <p className="text-sm leading-6 text-text-secondary">{guidance.next}</p>
      <Button asChild variant="secondary" size="sm" className="self-start">
        <Link to={guidance.route}>Open related surface</Link>
      </Button>
    </div>
  );
}

function MilestoneRow({ guidance }: { guidance: DomainGuidance }): JSX.Element {
  return (
    <div className="flex items-start justify-between gap-3 rounded-md border border-border-subtle bg-surface px-3 py-2">
      <div className="min-w-0">
        <p className="text-sm font-medium text-foreground">{guidance.label}</p>
        <p className="line-clamp-2 text-xs leading-5 text-text-secondary">{guidance.next}</p>
      </div>
      <StatusTag tone="info" variant="outline">{guidance.milestone}</StatusTag>
    </div>
  );
}

function buildDomainOptions(
  rows: CoverageMatrixRow[],
  definitions?: Array<{ domain: CoverageDomain; title: string }>,
): Array<{ value: DomainFilter; label: string; count?: number }> {
  const counts = new Map<string, number>();
  rows.forEach((row) => {
    const key = normalizeToken(row.domain);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  });

  const labels = new Map<string, string>();
  Object.entries(DOMAIN_GUIDANCE).forEach(([key, guidance]) => labels.set(key, guidance.label));
  definitions?.forEach((definition) => labels.set(normalizeToken(definition.domain), definition.title));

  const keys = new Set<string>();
  DOMAIN_ORDER.forEach((domain) => {
    if (counts.has(domain) || labels.has(domain)) keys.add(domain);
  });
  counts.forEach((_, key) => keys.add(key));

  return [
    { value: 'all', label: 'All', count: rows.length },
    ...Array.from(keys)
      .sort((a, b) => domainSortIndex(a) - domainSortIndex(b) || a.localeCompare(b))
      .map((key) => ({
        value: key as CoverageDomain,
        label: labels.get(key) ?? formatTokenLabel(key),
        count: counts.get(key) ?? 0,
      })),
  ];
}

function guidanceForDomain(domain: string): DomainGuidance {
  const key = normalizeToken(domain);
  return DOMAIN_GUIDANCE[key] ?? {
    label: formatTokenLabel(domain),
    owner: 'Unassigned',
    milestone: 'M167',
    next: 'Add an owner, required sources, support state, evidence path, and explicit next step.',
    route: '/control-room',
  };
}

function compareAttentionRows(a: CoverageMatrixRow, b: CoverageMatrixRow): number {
  const aState = rowStateKey(a);
  const bState = rowStateKey(b);
  const priority = (STATE_PRIORITY[aState] ?? 99) - (STATE_PRIORITY[bState] ?? 99);
  if (priority !== 0) return priority;
  return (a.title || a.name || a.id || '').localeCompare(b.title || b.name || b.id || '');
}

function rowCoverageState(row: CoverageMatrixRow): CoverageState {
  return row.state ?? row.coverage_state ?? 'unknown';
}

function rowStateKey(row: CoverageMatrixRow): string {
  return normalizeToken(rowCoverageState(row));
}

function normalizeDomain(value: string | null): string {
  if (!value) return 'all';
  const normalized = normalizeToken(value);
  return normalized || 'all';
}

function normalizeToken(value: string): string {
  return value.trim().toLowerCase().replace(/[\s-]+/g, '_');
}

function formatTokenLabel(value: string): string {
  const normalized = normalizeToken(value);
  if (normalized === 'db_audit') return 'DB audit';
  if (normalized === 'ai') return 'AI';
  return normalized
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function domainSortIndex(domain: string): number {
  const index = DOMAIN_ORDER.indexOf(domain as (typeof DOMAIN_ORDER)[number]);
  return index === -1 ? DOMAIN_ORDER.length + 1 : index;
}

function coverageRowKey(row: CoverageMatrixRow, index: number): string {
  return row.id || `${row.domain}:${row.title || row.name || index}`;
}

function toneTextClass(tone: StateTone): string {
  switch (tone) {
    case 'healthy':
      return 'text-state-healthy';
    case 'warning':
      return 'text-state-warning';
    case 'degraded':
      return 'text-state-degraded';
    case 'critical':
      return 'text-state-critical';
    case 'info':
      return 'text-state-info';
    case 'unknown':
    default:
      return 'text-text-secondary';
  }
}
