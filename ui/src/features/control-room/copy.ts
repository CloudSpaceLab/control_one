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
  if (!overview.availability.estate || !overview.availability.attention || !overview.availability.protection) {
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
  if (!overview.availability.estate || !overview.availability.protection) {
    return `${prefix}Some security or infrastructure data is unavailable.`;
  }
  return `${prefix}${overview.estate.groups_healthy} of ${overview.estate.groups_total} infrastructure groups healthy · ${formatPercent(overview.protection.percentage)} protection coverage`;
}

export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return 'N/A';
  return `${Math.round(value)}%`;
}

export function pluralize(value: number, singular: string, plural = `${singular}s`): string {
  return value === 1 ? singular : plural;
}
