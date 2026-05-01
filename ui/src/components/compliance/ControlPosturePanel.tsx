import { useMemo, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { EmptyState, Panel, PostureBar, StatusTag, type StateTone } from '../kit';
import type { ControlCoverage, ControlPostureResponse } from '../../lib/api';

const FRAMEWORK_PILLS = ['SOC2', 'HIPAA', 'PCI-DSS', 'ISO27001', 'GDPR'];

function statusTone(status: ControlCoverage['status']): StateTone {
  switch (status) {
    case 'PASS':
      return 'healthy';
    case 'PARTIAL':
      return 'warning';
    case 'FAIL':
      return 'critical';
    case 'NO_COVERAGE':
    default:
      return 'unknown';
  }
}

function formatLastChecked(iso?: string): string {
  if (!iso) return '—';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return iso;
  return t.toLocaleString();
}

interface ControlPosturePanelProps {
  framework: string;
  onFrameworkChange: (next: string) => void;
  posture: ControlPostureResponse | null;
  loading: boolean;
  error: string | null;
  tenantSelected: boolean;
}

// ControlPosturePanel renders the per-control coverage view added in PR 1.
// Layout: framework pills · totals KPIs · gap analysis banner (only when
// gap_count > 0) · accordion of control rows. Empty states are explicit so
// the synthetic-fallback removal does not surface as a blank panel.
export function ControlPosturePanel({
  framework,
  onFrameworkChange,
  posture,
  loading,
  error,
  tenantSelected,
}: ControlPosturePanelProps): JSX.Element {
  const coverage = posture?.coverage ?? [];

  const counts = useMemo(() => {
    let pass = 0,
      partial = 0,
      fail = 0,
      noCov = 0;
    for (const c of coverage) {
      if (c.status === 'PASS') pass += 1;
      else if (c.status === 'PARTIAL') partial += 1;
      else if (c.status === 'FAIL') fail += 1;
      else noCov += 1;
    }
    return { pass, partial, fail, noCov, total: coverage.length };
  }, [coverage]);

  const gaps = useMemo(() => coverage.filter((c) => c.status === 'NO_COVERAGE'), [coverage]);
  const compliancePct = counts.total > 0 ? Math.round((counts.pass / counts.total) * 100) : null;

  return (
    <Panel
      padding="md"
      eyebrow={`CONTROL POSTURE · ${framework}`}
      title="Per-control coverage"
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap gap-1.5">
          {FRAMEWORK_PILLS.map((fw) => (
            <button
              key={fw}
              type="button"
              onClick={() => onFrameworkChange(fw)}
              className={`rounded-md border px-3 py-1 text-xs font-medium transition-colors ${
                fw === framework
                  ? 'border-brand bg-brand/10 text-brand'
                  : 'border-border-subtle bg-surface text-text-secondary hover:border-border'
              }`}
            >
              {fw}
            </button>
          ))}
        </div>

        {!tenantSelected && (
          <EmptyState
            title="Select a tenant"
            description="Per-control posture is computed per tenant. Choose a tenant above to load coverage."
          />
        )}

        {tenantSelected && error && (
          <p className="rounded-md border border-state-critical/30 bg-state-critical/5 p-3 text-sm text-state-critical">
            {error}
          </p>
        )}

        {tenantSelected && !error && (
          <>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <CoverageTile label="Total controls" value={counts.total} loading={loading} tone="info" />
              <CoverageTile label="Passing" value={counts.pass} loading={loading} tone="healthy" />
              <CoverageTile label="Failing" value={counts.fail + counts.partial} loading={loading} tone={counts.fail > 0 ? 'critical' : 'warning'} />
              <CoverageTile label="Gap" value={counts.noCov} loading={loading} tone={counts.noCov > 0 ? 'warning' : 'healthy'} />
            </div>

            {compliancePct !== null && (
              <PostureBar score={compliancePct} ariaLabel={`${framework} coverage ${compliancePct}%`} showLabels />
            )}

            {gaps.length > 0 && <GapAnalysisBanner gaps={gaps} />}

            {coverage.length === 0 && !loading && (
              <EmptyState
                title="No automated checks configured"
                description={`No policies map to ${framework} controls for this tenant yet. Assign CIS-mapped policies to start scanning.`}
              />
            )}

            {coverage.length > 0 && (
              <div className="flex flex-col gap-1">
                {coverage.map((c) => (
                  <ControlAccordionRow key={c.control_id} control={c} />
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </Panel>
  );
}

interface CoverageTileProps {
  label: string;
  value: number;
  tone: StateTone;
  loading: boolean;
}

function CoverageTile({ label, value, tone, loading }: CoverageTileProps): JSX.Element {
  const toneClasses: Record<StateTone, string> = {
    healthy: 'border-state-healthy/30 text-state-healthy',
    warning: 'border-state-warning/30 text-state-warning',
    critical: 'border-state-critical/30 text-state-critical',
    degraded: 'border-state-warning/30 text-state-warning',
    info: 'border-state-info/30 text-state-info',
    unknown: 'border-border-subtle text-text-secondary',
  };
  return (
    <div className={`rounded-md border bg-surface p-3 ${toneClasses[tone]}`}>
      <div className="text-[10px] font-medium uppercase tracking-wide text-text-muted">{label}</div>
      <div className="mt-1 font-mono text-2xl font-semibold tabular-nums">
        {loading ? '—' : value}
      </div>
    </div>
  );
}

function GapAnalysisBanner({ gaps }: { gaps: ControlCoverage[] }): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <details
      className="rounded-md border border-state-warning/40 bg-state-warning/5 p-3 text-sm text-state-warning"
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
    >
      <summary className="flex cursor-pointer items-center gap-2 font-medium">
        <ChevronDown className={`h-4 w-4 transition-transform ${open ? '' : '-rotate-90'}`} />
        Gap analysis — {gaps.length} control{gaps.length === 1 ? '' : 's'} without automated coverage
      </summary>
      <ul className="mt-3 space-y-1 pl-6 text-text-secondary">
        {gaps.map((g) => (
          <li key={g.control_id}>
            <code className="rounded bg-surface-2 px-1 py-0.5 font-mono text-[0.7rem] text-text-secondary">
              {g.control_id}
            </code>{' '}
            {g.title}
          </li>
        ))}
      </ul>
    </details>
  );
}

function ControlAccordionRow({ control }: { control: ControlCoverage }): JSX.Element {
  const [open, setOpen] = useState(false);
  const ratio = control.nodes_checked > 0
    ? `${control.nodes_passing}/${control.nodes_checked}`
    : '—';
  return (
    <details
      className="rounded-md border border-border-subtle bg-surface"
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
    >
      <summary className="grid cursor-pointer grid-cols-[24px_140px_1fr_90px_90px_80px] items-center gap-3 px-3 py-2 text-xs">
        <ChevronDown className={`h-4 w-4 transition-transform ${open ? '' : '-rotate-90'}`} />
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] text-text-secondary">
          {control.control_id}
        </code>
        <span className="truncate text-foreground">{control.title}</span>
        <StatusTag tone={statusTone(control.status)}>{control.status}</StatusTag>
        <span className="font-mono tabular-nums text-text-secondary">{ratio} nodes</span>
        <span className="font-mono tabular-nums text-text-muted">{control.evidence_count} ev</span>
      </summary>
      <div className="border-t border-border-subtle px-3 py-3 text-xs">
        <dl className="grid grid-cols-2 gap-y-2 gap-x-6">
          <DetailRow label="Status" value={control.status} />
          <DetailRow label="Last checked" value={formatLastChecked(control.last_checked_at)} />
          <DetailRow label="Nodes passing" value={String(control.nodes_passing)} />
          <DetailRow label="Nodes failing" value={String(control.nodes_failing)} />
          <DetailRow label="Evidence count" value={String(control.evidence_count)} />
          {control.applicability && (
            <DetailRow label="Applicability" value={control.applicability} />
          )}
        </dl>
      </div>
    </details>
  );
}

function DetailRow({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="text-[10px] uppercase tracking-wide text-text-muted">{label}</dt>
      <dd className="font-mono tabular-nums text-text-secondary">{value}</dd>
    </div>
  );
}
