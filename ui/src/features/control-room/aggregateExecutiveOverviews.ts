import type {
  ControlRoomExecutiveOverview,
  ControlRoomExecutivePredictiveHealth,
  ControlRoomExecutiveTopRule,
} from '@/lib/api';

export interface TenantExecutiveOverview {
  tenantId: string;
  tenantName: string;
  overview: ControlRoomExecutiveOverview;
}

const ATTENTION_SAMPLE_LIMIT = 8;
const DEFAULT_TOP_RULE_LIMIT = 5;

export function aggregateExecutiveOverviews(
  entries: TenantExecutiveOverview[],
  period: string,
  failedTenantCount = 0,
): ControlRoomExecutiveOverview {
  if (entries.length === 0) {
    throw new Error('At least one tenant overview is required');
  }

  const complete = failedTenantCount === 0;
  const availability = {
    estate: complete && entries.every(({ overview }) => overview.availability.estate),
    violations: complete && entries.every(({ overview }) => overview.availability.violations),
    response: complete && entries.every(({ overview }) => overview.availability.response),
    attention: complete && entries.every(({ overview }) => overview.availability.attention),
    protection: complete && entries.every(({ overview }) => overview.availability.protection),
    activity: complete && entries.every(({ overview }) => overview.availability.activity),
  };

  const previousViolations = sum(entries, ({ overview }) => overview.violations.previous_total);
  const currentViolations = sum(entries, ({ overview }) => overview.violations.total);
  const protectedListeners = sum(entries, ({ overview }) => overview.protection.protected);
  const totalListeners = sum(entries, ({ overview }) => overview.protection.total);

  return {
    tenant_id: 'all',
    period,
    generated_at: latestTimestamp(entries.map(({ overview }) => overview.generated_at)) ?? new Date().toISOString(),
    estate: {
      groups_total: sum(entries, ({ overview }) => overview.estate.groups_total),
      groups_healthy: sum(entries, ({ overview }) => overview.estate.groups_healthy),
      groups_degraded: sum(entries, ({ overview }) => overview.estate.groups_degraded),
      groups_critical: sum(entries, ({ overview }) => overview.estate.groups_critical),
      groups_unknown: sum(entries, ({ overview }) => overview.estate.groups_unknown),
      nodes_total: sum(entries, ({ overview }) => overview.estate.nodes_total),
      nodes_healthy: sum(entries, ({ overview }) => overview.estate.nodes_healthy),
      predictive: aggregatePredictiveHealth(entries, complete),
      groups: entries.flatMap(({ tenantName, overview }) =>
        overview.estate.groups.map((group) => ({
          ...group,
          name: `${tenantName} · ${group.name}`,
        })),
      ),
    },
    violations: {
      total: currentViolations,
      critical: sum(entries, ({ overview }) => overview.violations.critical),
      high: sum(entries, ({ overview }) => overview.violations.high),
      medium: sum(entries, ({ overview }) => overview.violations.medium),
      low: sum(entries, ({ overview }) => overview.violations.low),
      info: sum(entries, ({ overview }) => overview.violations.info),
      other: sum(entries, ({ overview }) => overview.violations.other),
      previous_total: previousViolations,
      delta_pct: previousViolations > 0
        ? ((currentViolations - previousViolations) / previousViolations) * 100
        : 0,
      top_rules: aggregateTopRules(entries),
    },
    response: {
      handled_automatically: sum(entries, ({ overview }) => overview.response.handled_automatically),
      blocked: sum(entries, ({ overview }) => overview.response.blocked),
      contained: sum(entries, ({ overview }) => overview.response.contained),
      remediated: sum(entries, ({ overview }) => overview.response.remediated),
      failed: sum(entries, ({ overview }) => overview.response.failed),
    },
    attention: {
      total: sum(entries, ({ overview }) => overview.attention.total),
      critical: sum(entries, ({ overview }) => overview.attention.critical),
      reviews: sum(entries, ({ overview }) => overview.attention.reviews),
      approvals: sum(entries, ({ overview }) => overview.attention.approvals),
      interventions: sum(entries, ({ overview }) => overview.attention.interventions),
      items: entries
        .flatMap(({ tenantName, overview }) =>
          overview.attention.items.map((item) => ({
            ...item,
            title: `${tenantName} · ${item.title}`,
          })),
        )
        .sort(compareAttention)
        .slice(0, ATTENTION_SAMPLE_LIMIT),
    },
    protection: {
      protected: protectedListeners,
      total: totalListeners,
      percentage: totalListeners > 0 ? (protectedListeners / totalListeners) * 100 : 100,
      gaps: sum(entries, ({ overview }) => overview.protection.gaps),
      gap_types: aggregateGapTypes(entries),
    },
    activity: aggregateActivity(entries),
    availability,
  };
}

function aggregatePredictiveHealth(
  entries: TenantExecutiveOverview[],
  complete: boolean,
): ControlRoomExecutivePredictiveHealth {
  const states = entries.map(({ overview }) => overview.estate.predictive.state);
  const latestComputedAt = oldestTimestamp(
    entries
      .map(({ overview }) => overview.estate.predictive.latest_computed_at)
      .filter((value): value is string => Boolean(value)),
  );
  const freshnessSlaSeconds = entries
    .map(({ overview }) => overview.estate.predictive.freshness_sla_seconds)
    .filter((value) => Number.isFinite(value) && value > 0);

  return {
    state: !complete || states.includes('unavailable')
      ? 'unavailable'
      : states.includes('stale')
        ? 'stale'
        : states.includes('calibrating')
          ? 'calibrating'
          : 'available',
    freshness_sla_seconds: freshnessSlaSeconds.length > 0 ? Math.min(...freshnessSlaSeconds) : 0,
    scored_nodes: sum(entries, ({ overview }) => overview.estate.predictive.scored_nodes),
    fresh_nodes: sum(entries, ({ overview }) => overview.estate.predictive.fresh_nodes),
    unscored_nodes: sum(entries, ({ overview }) => overview.estate.predictive.unscored_nodes),
    calibrating_nodes: sum(entries, ({ overview }) => overview.estate.predictive.calibrating_nodes),
    stale_nodes: sum(entries, ({ overview }) => overview.estate.predictive.stale_nodes),
    at_risk_nodes: sum(entries, ({ overview }) => overview.estate.predictive.at_risk_nodes),
    latest_computed_at: latestComputedAt,
  };
}

function aggregateTopRules(entries: TenantExecutiveOverview[]): ControlRoomExecutiveTopRule[] {
  // Each tenant endpoint intentionally returns only a bounded top-rule sample.
  // Keep those samples tenant-qualified rather than pretending their union is
  // an exact organisation-wide ranking.
  return entries
    .flatMap(({ tenantId, tenantName, overview }) =>
      overview.violations.top_rules.map((rule) => ({
        ...rule,
        rule_id: `${tenantId}:${rule.rule_id}`,
        name: `${tenantName} · ${rule.name}`,
      })),
    )
    .sort((a, b) =>
      b.count - a.count
      || severityRank(b.severity) - severityRank(a.severity)
      || a.rule_id.localeCompare(b.rule_id),
    )
    .slice(0, DEFAULT_TOP_RULE_LIMIT);
}

function aggregateGapTypes(entries: TenantExecutiveOverview[]) {
  const counts = new Map<string, number>();
  for (const { overview } of entries) {
    for (const gap of overview.protection.gap_types) {
      counts.set(gap.type, (counts.get(gap.type) ?? 0) + gap.count);
    }
  }
  return [...counts.entries()]
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count || a.type.localeCompare(b.type));
}

function aggregateActivity(entries: TenantExecutiveOverview[]) {
  const points = new Map<string, { ts: string; critical: number; high: number; total: number }>();
  for (const { overview } of entries) {
    for (const point of overview.activity) {
      const current = points.get(point.ts) ?? { ts: point.ts, critical: 0, high: 0, total: 0 };
      current.critical += point.critical;
      current.high += point.high;
      current.total += point.total;
      points.set(point.ts, current);
    }
  }
  return [...points.values()].sort((a, b) => timestampValue(a.ts) - timestampValue(b.ts));
}

function compareAttention(
  a: ControlRoomExecutiveOverview['attention']['items'][number],
  b: ControlRoomExecutiveOverview['attention']['items'][number],
): number {
  return severityRank(b.severity) - severityRank(a.severity)
    || timestampValue(b.created_at) - timestampValue(a.created_at)
    || a.id.localeCompare(b.id);
}

function severityRank(value: string): number {
  switch (value.toLowerCase()) {
    case 'critical': return 5;
    case 'high': return 4;
    case 'medium': return 3;
    case 'low': return 2;
    case 'info': return 1;
    default: return 0;
  }
}

function sum(
  entries: TenantExecutiveOverview[],
  getValue: (entry: TenantExecutiveOverview) => number,
): number {
  return entries.reduce((total, entry) => total + getValue(entry), 0);
}

function latestTimestamp(values: string[]): string | undefined {
  return selectTimestamp(values, Math.max);
}

function oldestTimestamp(values: string[]): string | undefined {
  return selectTimestamp(values, Math.min);
}

function selectTimestamp(
  values: string[],
  selector: (...values: number[]) => number,
): string | undefined {
  const parsed = values
    .map((value) => ({ value, timestamp: timestampValue(value) }))
    .filter(({ timestamp }) => Number.isFinite(timestamp));
  if (parsed.length === 0) return undefined;
  const selected = selector(...parsed.map(({ timestamp }) => timestamp));
  return parsed.find(({ timestamp }) => timestamp === selected)?.value;
}

function timestampValue(value: string): number {
  const timestamp = new Date(value).getTime();
  return Number.isFinite(timestamp) ? timestamp : 0;
}
