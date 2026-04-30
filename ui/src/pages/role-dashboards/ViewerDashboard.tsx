import { useQuery } from '@tanstack/react-query';
import { Download, FileText } from 'lucide-react';
import { useState } from 'react';
import { Button } from '@/components/ui/button';
import {
  Chart,
  EXEC_TIME_RANGES,
  KpiTile,
  OnboardingChecklist,
  Panel,
  PostureBar,
  SectionHeader,
  TimeRangePills,
  type PostureSegment,
} from '@/components/kit';
import { DashboardGrid, DashboardGridItem } from '@/components/shell';
import { useApiClient } from '@/hooks/useApiClient';
import { useLiveSubscribe } from '@/hooks/useLiveSubscribe';
import { useOnboardingState } from '@/hooks/useOnboardingState';
import { useTenant } from '@/providers/TenantProvider';
import {
  AgingTable,
  RiskScoreCard,
  type FindingAging,
  type MTTDMetrics,
  type MTTRMetrics,
  type RemediationVelocity,
  type RiskScore,
} from '@/components/executive';
import type {
  ComplianceByFramework,
  DashboardOverview,
  RemediationVelocityHistory,
  RiskScoreHistory,
} from '@/lib/api';

const PERIOD_DAYS: Record<string, number> = { '7d': 7, '30d': 30, '90d': 90, qtd: 90 };

export interface ViewerDashboardProps {
  readonly?: boolean;
}

// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function ViewerDashboard({ readonly }: ViewerDashboardProps): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [period, setPeriod] = useState('30d');
  const days = PERIOD_DAYS[period] ?? 30;

  const overviewQ = useQuery<DashboardOverview>({
    queryKey: ['dashboard.overview', currentTenantId],
    queryFn: () => client.getDashboardOverview(currentTenantId ?? undefined),
    refetchInterval: 60_000,
  });
  const riskQ = useQuery<RiskScore>({
    queryKey: ['risk-score', currentTenantId],
    queryFn: () => client.getRiskScore(currentTenantId ?? undefined),
    refetchInterval: 60_000,
  });
  const mttdQ = useQuery<MTTDMetrics>({
    queryKey: ['mttd', currentTenantId, days],
    queryFn: () => client.getMTTDMetrics(currentTenantId ?? undefined, 'critical', days),
  });
  const mttrQ = useQuery<MTTRMetrics>({
    queryKey: ['mttr', currentTenantId, days],
    queryFn: () => client.getMTTRMetrics(currentTenantId ?? undefined, 'critical', days),
  });
  const velocityQ = useQuery<RemediationVelocity>({
    queryKey: ['velocity', currentTenantId, days],
    queryFn: () => client.getRemediationVelocity(currentTenantId ?? undefined, days),
  });
  const agingCritQ = useQuery<FindingAging>({
    queryKey: ['aging', currentTenantId, 'critical'],
    queryFn: () => client.getFindingsAging(currentTenantId ?? undefined, 'critical'),
  });
  const agingHighQ = useQuery<FindingAging>({
    queryKey: ['aging', currentTenantId, 'high'],
    queryFn: () => client.getFindingsAging(currentTenantId ?? undefined, 'high'),
  });
  const riskHistoryQ = useQuery<RiskScoreHistory>({
    queryKey: ['risk-history', currentTenantId, days],
    queryFn: () => client.getRiskScoreHistory(currentTenantId ?? undefined, days),
  });
  const remHistoryQ = useQuery<RemediationVelocityHistory>({
    queryKey: ['rem-history', currentTenantId, days],
    queryFn: () => client.getRemediationVelocityHistory(currentTenantId ?? undefined, days),
  });
  const frameworkQ = useQuery<ComplianceByFramework>({
    queryKey: ['compliance-frameworks', currentTenantId],
    queryFn: () => client.getComplianceByFramework(currentTenantId ?? undefined),
  });

  useLiveSubscribe(currentTenantId ?? undefined, [
    { topic: 'compliance.fired', invalidate: [['dashboard.overview'], ['compliance-frameworks']] },
    { topic: 'remediation.applied', invalidate: [['velocity'], ['rem-history']] },
  ]);

  const compliancePct = overviewQ.data
    ? overviewQ.data.compliance_summary.total > 0
      ? (overviewQ.data.compliance_summary.passed / overviewQ.data.compliance_summary.total) * 100
      : 0
    : 0;

  const riskComponents: PostureSegment[] = (riskQ.data?.components ?? []).map((c) => ({
    tone:
      (c.raw_score / Math.max(1, c.max_score)) >= 0.8
        ? 'healthy'
        : (c.raw_score / Math.max(1, c.max_score)) >= 0.6
        ? 'warning'
        : 'critical',
    weight: c.weight,
    label: c.name,
  }));

  const onboarding = useOnboardingState();

  if (onboarding.isReady && onboarding.isEmpty) {
    return (
      <div className="flex flex-col gap-6">
        <SectionHeader
          eyebrow="WELCOME · CISO ONBOARDING"
          title="Risk posture appears once telemetry starts flowing"
          description="Get your first server enrolled and a compliance scan run — risk score and trend charts populate automatically once data arrives."
        />
        <OnboardingChecklist steps={onboarding.steps} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <SectionHeader
        eyebrow="BOARD VIEW · SECURITY POSTURE"
        title="Executive Dashboard"
        description="Risk, compliance and remediation posture at a glance — board-ready."
        actions={
          <>
            <TimeRangePills value={period} options={EXEC_TIME_RANGES} onChange={setPeriod} />
            <Button variant="secondary" size="md">
              <Download className="h-4 w-4" /> Export PDF
            </Button>
          </>
        }
      />

      <DashboardGrid>
        <DashboardGridItem span={{ base: 12, lg: 5 }}>
          <Panel padding="md" eyebrow="RISK SCORE" title="Aggregate Risk">
            <RiskScoreCard score={riskQ.data ?? null} loading={riskQ.isLoading} />
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 7 }}>
          <Panel padding="md" eyebrow="COMPONENT BREAKDOWN" title="Risk drivers" toneAccent="brand">
            <PostureBar
              segments={riskComponents.length ? riskComponents : [{ tone: 'unknown', weight: 1, label: 'Loading…' }]}
              ariaLabel="Risk component weights"
              showLabels
            />
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {(riskQ.data?.components ?? []).map((c) => (
                <div
                  key={c.name}
                  className="rounded-md border border-border-subtle bg-surface px-3 py-2"
                >
                  <div className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                    {c.name}
                  </div>
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="font-mono text-lg font-semibold text-foreground tabular-nums">
                      {c.raw_score.toFixed(0)}
                      <span className="ml-1 text-xs text-text-muted">/ {c.max_score}</span>
                    </span>
                    <span className="font-mono text-xs text-text-secondary">
                      weight {(c.weight * 100).toFixed(0)}%
                    </span>
                  </div>
                  <p className="mt-1 text-xs text-text-secondary line-clamp-2">{c.description}</p>
                </div>
              ))}
            </div>
          </Panel>
        </DashboardGridItem>

        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="MTTD · CRITICAL"
            value={mttdQ.data ? formatMinutes(mttdQ.data.mean_minutes) : '—'}
            tone={mttdQ.data && mttdQ.data.mean_minutes < 15 ? 'healthy' : 'warning'}
            hint={`p95 ${mttdQ.data ? formatMinutes(mttdQ.data.p95_minutes) : '—'}`}
            loading={mttdQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="MTTR · CRITICAL"
            value={mttrQ.data ? formatMinutes(mttrQ.data.mean_minutes) : '—'}
            tone={mttrQ.data && mttrQ.data.mean_minutes < 240 ? 'healthy' : 'warning'}
            hint={`p95 ${mttrQ.data ? formatMinutes(mttrQ.data.p95_minutes) : '—'}`}
            loading={mttrQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="REMEDIATION VELOCITY"
            value={velocityQ.data?.remediations ?? '—'}
            delta={velocityQ.data?.trend_percent}
            tone="brand"
            hint={`${days}d window`}
            loading={velocityQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="COMPLIANCE COVERAGE"
            value={`${compliancePct.toFixed(0)}%`}
            tone={compliancePct >= 95 ? 'healthy' : compliancePct >= 80 ? 'warning' : 'critical'}
            hint={
              overviewQ.data
                ? `${overviewQ.data.compliance_summary.passed} / ${overviewQ.data.compliance_summary.total} passing`
                : ''
            }
            loading={overviewQ.isLoading}
          />
        </DashboardGridItem>

        <DashboardGridItem span={{ base: 12, lg: 7 }}>
          <Panel
            padding="md"
            eyebrow={`RISK SCORE · ${days}D`}
            title="Risk score trend"
            toneAccent="brand"
          >
            {riskHistoryQ.data && riskHistoryQ.data.points.length > 0 ? (
              <Chart
                kind="line"
                height={260}
                ariaLabel="Risk score over time"
                data={{
                  labels: riskHistoryQ.data.points.map((p) => p.ts.slice(0, 10)),
                  datasets: [
                    {
                      label: 'Risk score',
                      data: riskHistoryQ.data.points.map((p) => p.score),
                      borderColor: 'var(--brand-500)',
                      backgroundColor: 'rgba(99,102,241,0.18)',
                      fill: true,
                    },
                  ],
                }}
                options={{ scales: { y: { suggestedMax: 100 } } }}
              />
            ) : (
              <div className="grid h-[240px] place-items-center text-sm text-text-muted">
                {riskHistoryQ.isLoading ? 'Loading…' : 'No history yet.'}
              </div>
            )}
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 5 }}>
          <Panel padding="md" eyebrow="COMPLIANCE" title="Posture by framework" toneAccent="accent">
            <ul className="flex flex-col gap-3">
              {(frameworkQ.data?.frameworks ?? []).map((f) => {
                const total = f.pass + f.fail || 1;
                const pct = (f.pass / total) * 100;
                return (
                  <li key={f.name} className="flex flex-col gap-1.5">
                    <div className="flex items-center justify-between text-sm">
                      <span className="font-display font-semibold text-foreground">{f.name}</span>
                      <span className="font-mono text-xs text-text-secondary tabular-nums">
                        {f.pass} / {total}
                      </span>
                    </div>
                    <PostureBar score={pct} ariaLabel={`${f.name} posture`} />
                  </li>
                );
              })}
              {!frameworkQ.isLoading && (frameworkQ.data?.frameworks?.length ?? 0) === 0 && (
                <li className="text-sm text-text-muted">No framework results yet.</li>
              )}
            </ul>
          </Panel>
        </DashboardGridItem>

        <DashboardGridItem span={{ base: 12, lg: 6 }}>
          <Panel padding="md" eyebrow="CRITICAL FINDINGS · AGE" title="Critical aging" toneAccent="critical">
            <AgingTable aging={agingCritQ.data ?? null} loading={agingCritQ.isLoading} />
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 6 }}>
          <Panel padding="md" eyebrow="HIGH FINDINGS · AGE" title="High aging" toneAccent="warning">
            <AgingTable aging={agingHighQ.data ?? null} loading={agingHighQ.isLoading} />
          </Panel>
        </DashboardGridItem>

        <DashboardGridItem span={{ base: 12, lg: 8 }}>
          <Panel padding="md" eyebrow="REMEDIATIONS" title="Velocity over time" toneAccent="healthy">
            {remHistoryQ.data && remHistoryQ.data.points.length > 0 ? (
              <Chart
                kind="bar"
                height={240}
                ariaLabel="Remediation count by day"
                data={{
                  labels: remHistoryQ.data.points.map((p) => p.ts.slice(5, 10)),
                  datasets: [
                    {
                      label: 'Remediations',
                      data: remHistoryQ.data.points.map((p) => p.count),
                      backgroundColor: 'rgba(34, 211, 164, 0.55)',
                      borderColor: 'rgba(34, 211, 164, 1)',
                      borderWidth: 1,
                    },
                  ],
                }}
              />
            ) : (
              <div className="grid h-[200px] place-items-center text-sm text-text-muted">
                {remHistoryQ.isLoading ? 'Loading…' : 'No remediations yet.'}
              </div>
            )}
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 4 }}>
          <Panel padding="md" eyebrow="TOP DRIVERS" title="What hurts the score most">
            <ol className="flex flex-col gap-2">
              {(riskQ.data?.components ?? [])
                .map((c) => ({
                  ...c,
                  gap: c.max_score - c.raw_score,
                }))
                .sort((a, b) => b.gap - a.gap)
                .slice(0, 5)
                .map((c, i) => (
                  <li
                    key={c.name}
                    className="flex items-center justify-between gap-2 rounded-md border border-border-subtle bg-surface px-3 py-2"
                  >
                    <span className="inline-flex items-center gap-2">
                      <span className="font-mono text-xs text-text-muted">{i + 1}</span>
                      <span className="text-sm text-foreground">{c.name}</span>
                    </span>
                    <span className="font-mono text-xs text-state-critical tabular-nums">
                      −{c.gap.toFixed(0)}
                    </span>
                  </li>
                ))}
              {(riskQ.data?.components?.length ?? 0) === 0 && (
                <li className="text-sm text-text-muted">No risk components reported.</li>
              )}
            </ol>
            <div className="mt-3 flex items-center gap-2 rounded-md border border-border-subtle bg-surface px-3 py-2 text-xs text-text-muted">
              <FileText className="h-3.5 w-3.5" /> Drivers ranked by absolute score gap.
            </div>
          </Panel>
        </DashboardGridItem>
      </DashboardGrid>
    </div>
  );
}

function formatMinutes(min: number): string {
  if (!isFinite(min) || min <= 0) return '—';
  if (min < 1) return `${(min * 60).toFixed(0)}s`;
  if (min < 60) return `${min.toFixed(0)}m`;
  const h = Math.floor(min / 60);
  const m = Math.round(min % 60);
  return m === 0 ? `${h}h` : `${h}h ${m}m`;
}
