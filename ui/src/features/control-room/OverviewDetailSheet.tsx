import { Link } from 'react-router-dom';
import { ArrowRight } from 'lucide-react';
import type { ControlRoomExecutiveOverview } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { StatusTag, severityTone } from '@/components/kit';
import { CONTROL_ROOM_COPY, formatPercent } from './copy';

export type ExecutiveDetailKey =
  | 'infrastructure'
  | 'violations'
  | 'response'
  | 'attention'
  | 'protection';

interface OverviewDetailSheetProps {
  detail: ExecutiveDetailKey | null;
  overview: ControlRoomExecutiveOverview;
  onClose: () => void;
}

export function OverviewDetailSheet({ detail, overview, onClose }: OverviewDetailSheetProps) {
  const content = detail ? detailContent(detail, overview) : null;
  return (
    <Sheet open={detail !== null} onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-lg">
        {content ? (
          <>
            <SheetHeader>
              <SheetTitle>{content.title}</SheetTitle>
              <SheetDescription>{content.description}</SheetDescription>
            </SheetHeader>
            <div className="mt-6">{content.body}</div>
          </>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function detailContent(detail: ExecutiveDetailKey, overview: ControlRoomExecutiveOverview) {
  switch (detail) {
    case 'infrastructure':
      return {
        title: CONTROL_ROOM_COPY.infrastructureHealth,
        description: `${overview.estate.groups_healthy} of ${overview.estate.groups_total} groups healthy.`,
        body: (
          <div className="space-y-3">
            <PredictiveHealthSummary predictive={overview.estate.predictive} />
            <div className="space-y-2">
            {overview.estate.groups.map((group) => (
              <Link
                key={group.name}
                to={group.drilldown}
                className="flex items-center justify-between gap-3 rounded-lg border border-border-subtle bg-surface p-3 hover:bg-hover"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-foreground">{group.name}</p>
                  <p className="mt-1 text-xs text-text-muted">
                    {group.nodes_healthy}/{group.nodes_total} healthy
                    {group.intentionally_isolated > 0 ? ` · ${group.intentionally_isolated} intentionally isolated` : ''}
                    {group.predictive_risk ? ` · predictive ${group.predictive_risk}` : ''}
                  </p>
                </div>
                <StatusTag tone={groupStateTone(group.state)}>{group.state}</StatusTag>
              </Link>
            ))}
            <DetailLink to="/nodes" label="View infrastructure" />
            </div>
          </div>
        ),
      };
    case 'violations':
      return {
        title: CONTROL_ROOM_COPY.ruleViolations,
        description: `${overview.violations.total} organisation-defined rule violations in this period.`,
        body: (
          <div className="space-y-2">
            {overview.violations.top_rules.length > 0 ? overview.violations.top_rules.map((rule) => (
              <Link
                key={rule.rule_id}
                to={rule.drilldown}
                className="flex items-center justify-between gap-3 rounded-lg border border-border-subtle bg-surface p-3 hover:bg-hover"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-foreground">{rule.name}</p>
                  <p className="mt-1 text-xs text-text-muted">{rule.rule_type}</p>
                </div>
                <div className="flex items-center gap-2">
                  <StatusTag tone={severityTone(rule.severity)}>{rule.severity}</StatusTag>
                  <span className="font-mono text-sm font-semibold tabular-nums">{rule.count}</span>
                </div>
              </Link>
            )) : <p className="text-sm text-text-muted">No rule violations in this period.</p>}
            <DetailLink to="/rules" label="View detection rules" />
          </div>
        ),
      };
    case 'response':
      return {
        title: CONTROL_ROOM_COPY.handledAutomatically,
        description: `${overview.response.handled_automatically} verified responses completed without a remaining human gate.`,
        body: (
          <div className="space-y-3">
            <ResponseRow label="Blocked" value={overview.response.blocked} />
            <ResponseRow label="Contained" value={overview.response.contained} />
            <ResponseRow label="Remediated" value={overview.response.remediated} />
            <ResponseRow label="Failed" value={overview.response.failed} critical={overview.response.failed > 0} />
            <DetailLink to="/audit" label="View audit trail" />
          </div>
        ),
      };
    case 'attention':
      return {
        title: CONTROL_ROOM_COPY.needsAttention,
        description: `${overview.attention.total} human ${overview.attention.total === 1 ? 'action' : 'actions'} outstanding.`,
        body: (
          <div className="space-y-2">
            {overview.attention.items.map((item) => (
              <Link
                key={`${item.kind}:${item.id}`}
                to={item.drilldown}
                className="block rounded-lg border border-border-subtle bg-surface p-3 hover:bg-hover"
              >
                <div className="flex items-center justify-between gap-3">
                  <p className="min-w-0 truncate text-sm font-medium text-foreground">{item.title}</p>
                  <StatusTag tone={severityTone(item.severity)}>{item.severity}</StatusTag>
                </div>
                <p className="mt-1 text-xs text-text-muted">{item.kind} · {item.domain}</p>
              </Link>
            ))}
            {overview.attention.total > overview.attention.items.length ? (
              <p className="text-xs text-text-muted">
                Showing {overview.attention.items.length} highest-priority items of {overview.attention.total}.
              </p>
            ) : null}
          </div>
        ),
      };
    case 'protection':
      return {
        title: CONTROL_ROOM_COPY.protectionCoverage,
        description: overview.protection.total === 0
          ? 'No public network listeners are currently reported.'
          : `${overview.protection.protected} of ${overview.protection.total} public listeners have verified protection evidence (${formatPercent(overview.protection.percentage)}).`,
        body: (
          <div className="space-y-2">
            {overview.protection.gap_types.length > 0 ? overview.protection.gap_types.map((gap) => (
              <div key={gap.type} className="flex items-center justify-between rounded-lg border border-border-subtle bg-surface p-3">
                <span className="text-sm text-foreground">{gap.type}</span>
                <span className="font-mono text-sm font-semibold tabular-nums">{gap.count}</span>
              </div>
            )) : <p className="text-sm text-text-muted">No protection gaps.</p>}
            <DetailLink to="/control-room/exposure" label="View exposure details" />
          </div>
        ),
      };
  }
}

function PredictiveHealthSummary({
  predictive,
}: {
  predictive: ControlRoomExecutiveOverview['estate']['predictive'];
}) {
  const stateLabel = predictive.state === 'available'
    ? predictive.at_risk_nodes > 0
      ? `${predictive.at_risk_nodes} at-risk ${predictive.at_risk_nodes === 1 ? 'node' : 'nodes'}`
      : 'No predictive risks'
    : predictive.state === 'calibrating'
      ? 'Calibrating'
      : predictive.state === 'stale'
        ? 'Stale'
        : 'Unavailable';

  return (
    <div className="rounded-lg border border-border-subtle bg-surface p-3">
      <div className="flex items-center justify-between gap-3">
        <span className="text-xs font-semibold uppercase tracking-wide text-text-muted">Predictive health</span>
        <StatusTag tone={predictiveStateTone(predictive.state)} variant="outline">{stateLabel}</StatusTag>
      </div>
      <p className="mt-2 text-xs text-text-muted">
        {predictive.state === 'available'
          ? `${predictive.fresh_nodes} fresh scores · ${predictive.stale_nodes} stale`
          : predictive.state === 'calibrating'
            ? `${predictive.calibrating_nodes} nodes calibrating`
            : predictive.state === 'stale'
              ? `${predictive.stale_nodes} stale scores`
              : 'No current predictive scores'}
      </p>
    </div>
  );
}

function predictiveStateTone(state: string): 'healthy' | 'warning' | 'info' | 'unknown' {
  if (state === 'available') return 'info';
  if (state === 'calibrating') return 'info';
  if (state === 'stale') return 'warning';
  return 'unknown';
}

function ResponseRow({ label, value, critical }: { label: string; value: number; critical?: boolean }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-border-subtle bg-surface px-3 py-2.5">
      <span className="text-sm text-text-secondary">{label}</span>
      <span className={`font-mono text-sm font-semibold tabular-nums ${critical ? 'text-state-critical' : 'text-foreground'}`}>{value}</span>
    </div>
  );
}

function DetailLink({ to, label }: { to: string; label: string }) {
  return (
    <Button asChild variant="outline" size="sm" className="mt-3 w-full justify-between">
      <Link to={to}>{label}<ArrowRight /></Link>
    </Button>
  );
}

function groupStateTone(state: string): 'healthy' | 'warning' | 'critical' | 'unknown' {
  if (state === 'healthy') return 'healthy';
  if (state === 'critical') return 'critical';
  if (state === 'degraded') return 'warning';
  return 'unknown';
}
