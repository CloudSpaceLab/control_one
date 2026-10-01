import type { ControlRoomExecutiveOverview } from '@/lib/api';

export const CONTROL_ROOM_COPY = {
  eyebrow: 'CONTROL ROOM',
  infrastructureHealth: 'Infrastructure health',
  ruleViolations: 'Rule violations',
  handledAutomatically: 'Handled automatically',
  needsAttention: 'Needs attention',
  protectionCoverage: 'Protection coverage',
  review: 'Review',
  approval: 'Approval',
  intervention: 'Intervention',
  viewDetails: 'View details',
  dataUnavailable: 'Data unavailable',
} as const;

export function executiveHeadline(overview: ControlRoomExecutiveOverview): string {
  if (!executiveCoreAvailable(overview)) {
    return 'Protection status incomplete';
  }
  if (overview.attention.critical > 0) {
    return `${overview.attention.critical} critical ${overview.attention.critical === 1 ? 'item needs' : 'items need'} attention`;
  }
  if (overview.attention.total > 0) {
    return `${overview.attention.total} ${overview.attention.total === 1 ? 'item needs' : 'items need'} attention`;
  }
  return 'No critical action required';
}

export function executiveDescription(
  overview: ControlRoomExecutiveOverview,
  tenantName?: string,
): string {
  const prefix = tenantName ? `${tenantName} · ` : '';
  if (!executiveCoreAvailable(overview)) {
    return `${prefix}Some security or infrastructure data is unavailable.`;
  }
  const protection = overview.protection.total === 0
    ? 'no public listeners detected'
    : `${formatPercent(overview.protection.percentage)} protection coverage`;
  return `${prefix}${overview.estate.groups_healthy} of ${overview.estate.groups_total} infrastructure groups healthy · ${protection}`;
}

export function executiveCoreAvailable(overview: ControlRoomExecutiveOverview): boolean {
  return overview.availability.estate
    && overview.availability.violations
    && overview.availability.response
    && overview.availability.attention
    && overview.availability.protection;
}

export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return 'N/A';
  return `${Math.round(value)}%`;
}

export function formatPeriodDelta(deltaPct: number, previousTotal: number): string {
  if (previousTotal <= 0 || !Number.isFinite(deltaPct)) return '';
  const rounded = Math.round(Math.abs(deltaPct));
  if (rounded === 0) return 'no change';
  return `${rounded}% ${deltaPct < 0 ? 'lower' : 'higher'}`;
}
