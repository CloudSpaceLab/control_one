import {
  AlertTriangle,
  CheckCircle2,
  CircleSlash,
  Clock3,
  FileWarning,
  HelpCircle,
  ShieldQuestion,
} from 'lucide-react';
import { EmptyState, StatusTag, type StateTone } from '../kit';
import { cn } from '@/lib/utils';
import type { CoverageMatrixRow, CoverageQualityState, CoverageState } from '../../lib/api';

type CanonicalCoverageState =
  | 'supported'
  | 'partial'
  | 'raw_only'
  | 'unsupported'
  | 'manual_evidence'
  | 'stale'
  | 'exception'
  | 'not_applicable'
  | 'unknown';

interface CoverageStateMeta {
  label: string;
  tone: StateTone;
  rank: number;
}

const COVERAGE_STATE_META: Record<CanonicalCoverageState, CoverageStateMeta> = {
  supported: { label: 'Supported', tone: 'healthy', rank: 70 },
  partial: { label: 'Partial', tone: 'warning', rank: 20 },
  raw_only: { label: 'Raw-only', tone: 'info', rank: 30 },
  unsupported: { label: 'Unsupported', tone: 'critical', rank: 10 },
  manual_evidence: { label: 'Manual evidence', tone: 'warning', rank: 40 },
  stale: { label: 'Stale', tone: 'degraded', rank: 15 },
  exception: { label: 'Exception', tone: 'warning', rank: 45 },
  not_applicable: { label: 'N/A', tone: 'unknown', rank: 80 },
  unknown: { label: 'Unknown', tone: 'unknown', rank: 50 },
};

const QUALITY_TONE: Record<string, StateTone> = {
  production_tested: 'healthy',
  fixture_tested: 'info',
  manual: 'warning',
  untested: 'warning',
  unknown: 'unknown',
};

export interface CoverageTruthSummary {
  total: number;
  applicable: number;
  supported: number;
  partial: number;
  rawOnly: number;
  unsupported: number;
  manualEvidence: number;
  stale: number;
  exception: number;
  notApplicable: number;
  unknown: number;
  needsReview: number;
  passingPercent: number | null;
}

export function summarizeCoverageRows(rows: CoverageMatrixRow[]): CoverageTruthSummary {
  const summary: CoverageTruthSummary = {
    total: rows.length,
    applicable: 0,
    supported: 0,
    partial: 0,
    rawOnly: 0,
    unsupported: 0,
    manualEvidence: 0,
    stale: 0,
    exception: 0,
    notApplicable: 0,
    unknown: 0,
    needsReview: 0,
    passingPercent: null,
  };

  for (const row of rows) {
    const state = canonicalCoverageState(rowCoverageState(row));
    if (state !== 'not_applicable') summary.applicable += 1;
    if (state === 'supported') summary.supported += 1;
    else if (state === 'partial') summary.partial += 1;
    else if (state === 'raw_only') summary.rawOnly += 1;
    else if (state === 'unsupported') summary.unsupported += 1;
    else if (state === 'manual_evidence') summary.manualEvidence += 1;
    else if (state === 'stale') summary.stale += 1;
    else if (state === 'exception') summary.exception += 1;
    else if (state === 'not_applicable') summary.notApplicable += 1;
    else summary.unknown += 1;
  }

  summary.needsReview =
    summary.partial + summary.rawOnly + summary.manualEvidence + summary.stale + summary.exception + summary.unknown;
  summary.passingPercent = summary.applicable > 0 ? Math.round((summary.supported / summary.applicable) * 100) : null;
  return summary;
}

export interface CoverageTruthBadgeProps {
  state: CoverageState;
  className?: string;
}

export function CoverageTruthBadge({ state, className }: CoverageTruthBadgeProps): JSX.Element {
  const meta = coverageStateMeta(state);
  return (
    <StatusTag tone={meta.tone} className={className}>
      {meta.label}
    </StatusTag>
  );
}

export interface CoverageQualityBadgeProps {
  quality?: CoverageQualityState;
}

export function CoverageQualityBadge({ quality }: CoverageQualityBadgeProps): JSX.Element | null {
  if (!quality) return null;
  const key = normalizeToken(quality);
  return (
    <StatusTag tone={QUALITY_TONE[key] ?? 'unknown'} variant="outline">
      {formatTokenLabel(quality)}
    </StatusTag>
  );
}

export interface CoverageTruthListProps {
  rows: CoverageMatrixRow[];
  loading?: boolean;
  error?: string | null;
  unavailable?: boolean;
  generatedAt?: string;
  maxRows?: number;
  className?: string;
}

export function CoverageTruthList({
  rows,
  loading = false,
  error,
  unavailable = false,
  generatedAt,
  maxRows = 8,
  className,
}: CoverageTruthListProps): JSX.Element {
  const summary = summarizeCoverageRows(rows);
  const sortedRows = [...rows].sort(compareCoverageRows).slice(0, maxRows);
  const hiddenCount = Math.max(0, rows.length - sortedRows.length);

  if (unavailable) {
    return (
      <EmptyState
        icon={<ShieldQuestion />}
        title="Coverage matrix unavailable"
        description="The coverage truth endpoint is not wired in this environment yet."
        className={className}
      />
    );
  }

  if (error) {
    return (
      <div className={cn('rounded-md border border-state-warning/30 bg-state-warning/5 p-3 text-sm text-state-warning', className)}>
        {error}
      </div>
    );
  }

  if (loading && rows.length === 0) {
    return <CoverageTruthSkeleton className={className} />;
  }

  if (rows.length === 0) {
    return (
      <EmptyState
        icon={<HelpCircle />}
        title="No coverage rows"
        description="No telemetry, parser, detection, compliance, or remediation coverage rows were returned."
        className={className}
      />
    );
  }

  return (
    <div className={cn('flex flex-col gap-4', className)}>
      <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
        <CoverageSummaryCell
          label="Supported"
          value={summary.supported}
          tone="healthy"
          helper={summary.passingPercent === null ? 'No applicable rows' : `${summary.passingPercent}% applicable`}
        />
        <CoverageSummaryCell label="Needs review" value={summary.needsReview} tone={summary.needsReview > 0 ? 'warning' : 'healthy'} />
        <CoverageSummaryCell label="Unsupported" value={summary.unsupported} tone={summary.unsupported > 0 ? 'critical' : 'healthy'} />
        <CoverageSummaryCell label="N/A" value={summary.notApplicable} tone="unknown" />
      </div>

      <div className="flex flex-col gap-2">
        {sortedRows.map((row, index) => (
          <CoverageTruthRow key={coverageRowKey(row, index)} row={row} />
        ))}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-text-muted">
        <span>
          {hiddenCount > 0 ? `${hiddenCount} more row${hiddenCount === 1 ? '' : 's'}` : `${rows.length} total row${rows.length === 1 ? '' : 's'}`}
        </span>
        {generatedAt && <span className="font-mono tabular-nums">Generated {formatCoverageTimestamp(generatedAt)}</span>}
      </div>
    </div>
  );
}

function CoverageTruthRow({ row }: { row: CoverageMatrixRow }): JSX.Element {
  const state = canonicalCoverageState(rowCoverageState(row));
  const meta = COVERAGE_STATE_META[state];
  const title = row.name || row.title || row.id || 'Untitled coverage row';
  const detail = row.reason || row.description || row.details || (row.gaps?.length ? `Gap: ${row.gaps[0]}` : undefined);
  const sources = rowCoverageSources(row);
  const evidence = typeof row.evidence_count === 'number' ? `${row.evidence_count} ev` : null;
  const lastSeen = row.last_seen_at || row.updated_at;

  return (
    <div className="grid gap-3 rounded-md border border-border-subtle bg-surface p-3 md:grid-cols-[1fr_auto]">
      <div className="flex min-w-0 gap-3">
        <div className={cn('mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-surface-2', toneTextClass(meta.tone))}>
          {coverageStateIcon(state)}
        </div>
        <div className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="font-mono text-[0.65rem] uppercase tracking-wide text-text-muted">{row.domain}</span>
            <h4 className="min-w-0 truncate text-sm font-medium text-foreground">{title}</h4>
          </div>
          {detail && <p className="mt-1 line-clamp-2 text-xs text-text-secondary">{detail}</p>}
          {sources && <p className="mt-1 truncate text-xs text-text-muted">Sources: {sources}</p>}
        </div>
      </div>
      <div className="flex flex-wrap items-start gap-1.5 md:justify-end">
        <CoverageTruthBadge state={rowCoverageState(row)} />
        {rowCoverageQuality(row).map((quality) => (
          <CoverageQualityBadge key={quality} quality={quality} />
        ))}
        {evidence && <StatusTag tone="info" variant="outline">{evidence}</StatusTag>}
        {lastSeen && <StatusTag tone="unknown" variant="outline">{formatCoverageTimestamp(lastSeen)}</StatusTag>}
      </div>
    </div>
  );
}

function CoverageSummaryCell({
  label,
  value,
  tone,
  helper,
}: {
  label: string;
  value: number;
  tone: StateTone;
  helper?: string;
}): JSX.Element {
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <div className="text-[10px] font-medium uppercase tracking-wide text-text-muted">{label}</div>
      <div className={cn('mt-1 font-mono text-2xl font-semibold tabular-nums', toneTextClass(tone))}>{value}</div>
      {helper && <div className="mt-1 text-xs text-text-muted">{helper}</div>}
    </div>
  );
}

function CoverageTruthSkeleton({ className }: { className?: string }): JSX.Element {
  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <div key={index} className="h-20 animate-pulse rounded-md border border-border-subtle bg-surface" />
        ))}
      </div>
      {Array.from({ length: 3 }, (_, index) => (
        <div key={index} className="h-20 animate-pulse rounded-md border border-border-subtle bg-surface" />
      ))}
    </div>
  );
}

function compareCoverageRows(a: CoverageMatrixRow, b: CoverageMatrixRow): number {
  const aRank = coverageStateMeta(rowCoverageState(a)).rank;
  const bRank = coverageStateMeta(rowCoverageState(b)).rank;
  if (aRank !== bRank) return aRank - bRank;
  return (a.name || a.title || a.id || '').localeCompare(b.name || b.title || b.id || '');
}

function coverageStateMeta(state: CoverageState | undefined | null): CoverageStateMeta {
  return COVERAGE_STATE_META[canonicalCoverageState(state)];
}

function canonicalCoverageState(state: CoverageState | undefined | null): CanonicalCoverageState {
  const normalized = normalizeToken(state ?? 'unknown');
  if (normalized in COVERAGE_STATE_META) return normalized as CanonicalCoverageState;
  return 'unknown';
}

function coverageStateIcon(state: CanonicalCoverageState): JSX.Element {
  switch (state) {
    case 'supported':
      return <CheckCircle2 className="h-4 w-4" />;
    case 'unsupported':
      return <CircleSlash className="h-4 w-4" />;
    case 'stale':
      return <Clock3 className="h-4 w-4" />;
    case 'manual_evidence':
    case 'exception':
      return <FileWarning className="h-4 w-4" />;
    case 'partial':
    case 'raw_only':
      return <AlertTriangle className="h-4 w-4" />;
    case 'not_applicable':
    case 'unknown':
    default:
      return <HelpCircle className="h-4 w-4" />;
  }
}

function normalizeToken(value: string): string {
  return value.trim().toLowerCase().replace(/[\s-]+/g, '_');
}

function formatTokenLabel(value: string): string {
  const normalized = normalizeToken(value);
  if (normalized === 'raw_only') return 'Raw-only';
  if (normalized === 'not_applicable') return 'N/A';
  return normalized
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function formatCoverageTimestamp(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function coverageRowKey(row: CoverageMatrixRow, index: number): string {
  return row.id || `${row.domain}:${row.name || row.title || index}`;
}

function rowCoverageState(row: CoverageMatrixRow): CoverageState {
  return row.state ?? row.coverage_state ?? 'unknown';
}

function rowCoverageQuality(row: CoverageMatrixRow): CoverageQualityState[] {
  const quality = row.quality_state ?? row.quality ?? row.fixture_status;
  if (!quality) return [];
  return Array.isArray(quality) ? quality : [quality];
}

function rowCoverageSources(row: CoverageMatrixRow): string | undefined {
  if (row.required_sources?.length) return row.required_sources.join(', ');
  if (row.evidence?.length) return row.evidence.slice(0, 2).join(', ');
  return row.source || row.source_kind;
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
