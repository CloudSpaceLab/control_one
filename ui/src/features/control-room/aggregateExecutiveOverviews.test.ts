import { describe, expect, it } from 'vitest';
import type { ControlRoomExecutiveOverview } from '@/lib/api';
import { aggregateExecutiveOverviews } from './aggregateExecutiveOverviews';

function overview(
  tenantId: string,
  overrides: Partial<ControlRoomExecutiveOverview> = {},
): ControlRoomExecutiveOverview {
  return {
    tenant_id: tenantId,
    generated_at: '2026-10-02T06:00:00Z',
    period: '7d',
    estate: {
      groups_total: 1,
      groups_healthy: 1,
      groups_degraded: 0,
      groups_critical: 0,
      groups_unknown: 0,
      nodes_total: 10,
      nodes_healthy: 9,
      predictive: {
        state: 'available',
        freshness_sla_seconds: 10800,
        scored_nodes: 8,
        fresh_nodes: 8,
        unscored_nodes: 2,
        calibrating_nodes: 0,
        stale_nodes: 0,
        at_risk_nodes: 0,
        latest_computed_at: '2026-10-02T05:55:00Z',
      },
      groups: [{
        name: 'Payments',
        state: 'healthy',
        nodes_total: 10,
        nodes_healthy: 9,
        nodes_stale: 1,
        nodes_offline: 0,
        intentionally_isolated: 0,
        predictive_nodes_at_risk: 0,
        drilldown: '/nodes',
      }],
    },
    violations: {
      total: 10,
      critical: 1,
      high: 2,
      medium: 3,
      low: 4,
      info: 0,
      other: 0,
      previous_total: 20,
      delta_pct: -50,
      top_rules: [{
        rule_id: 'restricted-port',
        name: 'Restricted port',
        rule_type: 'port',
        severity: 'critical',
        count: 6,
        drilldown: '/rules',
      }],
    },
    response: {
      handled_automatically: 7,
      blocked: 3,
      contained: 2,
      remediated: 2,
      failed: 1,
    },
    attention: {
      total: 2,
      critical: 1,
      reviews: 1,
      approvals: 1,
      interventions: 0,
      items: [{
        id: `attention-${tenantId}`,
        kind: 'review',
        severity: 'critical',
        domain: 'alerts',
        title: 'Privileged access',
        created_at: '2026-10-02T05:50:00Z',
        drilldown: '/alerts',
      }],
    },
    protection: {
      protected: 8,
      total: 10,
      percentage: 80,
      gaps: 2,
      gap_types: [{ type: 'Firewall state unknown', count: 2 }],
    },
    activity: [{ ts: '2026-10-02T00:00:00Z', critical: 1, high: 2, total: 10 }],
    availability: {
      estate: true,
      violations: true,
      response: true,
      attention: true,
      protection: true,
      activity: true,
    },
    ...overrides,
  };
}

describe('aggregateExecutiveOverviews', () => {
  it('sums exact organisation-wide counts and recomputes ratios', () => {
    const first = overview('tenant-1');
    const second = overview('tenant-2', {
      violations: {
        ...overview('tenant-2').violations,
        total: 30,
        previous_total: 20,
        top_rules: [{
          rule_id: 'restricted-port',
          name: 'Restricted port',
          rule_type: 'port',
          severity: 'critical',
          count: 4,
          drilldown: '/rules',
        }],
      },
      protection: {
        protected: 5,
        total: 10,
        percentage: 50,
        gaps: 5,
        gap_types: [{ type: 'Firewall state unknown', count: 5 }],
      },
    });

    const result = aggregateExecutiveOverviews([
      { tenantId: 'tenant-1', tenantName: 'Bank A', overview: first },
      { tenantId: 'tenant-2', tenantName: 'Bank B', overview: second },
    ], '7d');

    expect(result.tenant_id).toBe('all');
    expect(result.estate.nodes_total).toBe(20);
    expect(result.violations.total).toBe(40);
    expect(result.violations.previous_total).toBe(40);
    expect(result.violations.delta_pct).toBe(0);
    expect(result.response.handled_automatically).toBe(14);
    expect(result.protection.protected).toBe(13);
    expect(result.protection.total).toBe(20);
    expect(result.protection.percentage).toBe(65);
    expect(result.protection.gap_types).toEqual([{ type: 'Firewall state unknown', count: 7 }]);
    expect(result.activity).toEqual([
      { ts: '2026-10-02T00:00:00Z', critical: 2, high: 4, total: 20 },
    ]);
  });

  it('retains tenant context in drilldowns and merges top-rule counts', () => {
    const result = aggregateExecutiveOverviews([
      { tenantId: 'tenant-1', tenantName: 'Bank A', overview: overview('tenant-1') },
      { tenantId: 'tenant-2', tenantName: 'Bank B', overview: overview('tenant-2') },
    ], '7d');

    expect(result.estate.groups.map((group) => group.name)).toEqual([
      'Bank A · Payments',
      'Bank B · Payments',
    ]);
    expect(result.estate.groups[0].drilldown).toBe('/nodes');
    expect(result.attention.items.map((item) => item.title)).toContain('Bank A · Privileged access');
    expect(result.attention.items[0].drilldown).toBe('/alerts');
    expect(result.violations.top_rules[0]).toMatchObject({
      rule_id: 'restricted-port',
      count: 12,
      severity: 'high',
    });
  });

  it('marks aggregate domains unavailable when any tenant request failed', () => {
    const result = aggregateExecutiveOverviews([
      { tenantId: 'tenant-1', tenantName: 'Bank A', overview: overview('tenant-1') },
    ], '7d', 1);

    expect(result.availability).toEqual({
      estate: false,
      violations: false,
      response: false,
      attention: false,
      protection: false,
      activity: false,
    });
    expect(result.estate.predictive.state).toBe('unavailable');
  });
});
