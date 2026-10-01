import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ControlRoom } from './ControlRoom';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantModule from '../providers/TenantProvider';
import type { ControlRoomExecutiveOverview } from '../lib/api';

// Breakpoint smoke: jsdom cannot validate the visual grid, but this keeps the
// executive Control Room render path honest across the viewport sizes operators use.

const viewports = [
  { label: 'mobile-375', width: 375, height: 667 },
  { label: 'tablet-768', width: 768, height: 1024 },
  { label: 'desktop-1280', width: 1280, height: 900 },
];

const overview: ControlRoomExecutiveOverview = {
  tenant_id: 'tenant-1',
  generated_at: '2026-10-01T17:00:00Z',
  period: '7d',
  estate: {
    groups_total: 3,
    groups_healthy: 2,
    groups_degraded: 1,
    groups_critical: 0,
    groups_unknown: 0,
    nodes_total: 12,
    nodes_healthy: 11,
    predictive: {
      state: 'calibrating',
      freshness_sla_seconds: 10800,
      scored_nodes: 12,
      fresh_nodes: 12,
      unscored_nodes: 0,
      calibrating_nodes: 12,
      stale_nodes: 0,
      at_risk_nodes: 0,
      latest_computed_at: '2026-10-01T16:55:00Z',
    },
    groups: [
      {
        name: 'Payments',
        state: 'healthy',
        nodes_total: 4,
        nodes_healthy: 4,
        nodes_stale: 0,
        nodes_offline: 0,
        intentionally_isolated: 0,
        predictive_nodes_at_risk: 0,
        drilldown: '/nodes',
      },
      {
        name: 'Web Edge',
        state: 'degraded',
        nodes_total: 4,
        nodes_healthy: 3,
        nodes_stale: 1,
        nodes_offline: 0,
        intentionally_isolated: 0,
        predictive_nodes_at_risk: 0,
        drilldown: '/nodes',
      },
    ],
  },
  violations: {
    total: 5,
    critical: 0,
    high: 1,
    medium: 4,
    low: 0,
    info: 0,
    other: 0,
    previous_total: 8,
    delta_pct: -37.5,
    top_rules: [],
  },
  response: {
    handled_automatically: 4,
    blocked: 2,
    contained: 1,
    remediated: 1,
    failed: 0,
  },
  attention: {
    total: 1,
    critical: 0,
    reviews: 0,
    approvals: 1,
    interventions: 0,
    items: [
      {
        id: 'approval-1',
        kind: 'approval',
        severity: 'medium',
        domain: 'patch',
        title: 'Patch payments-db-02',
        created_at: '2026-10-01T16:00:00Z',
        drilldown: '/infrastructure/patch',
      },
    ],
  },
  protection: {
    protected: 7,
    total: 8,
    percentage: 87.5,
    gaps: 1,
    gap_types: [{ type: 'Firewall state unknown', count: 1 }],
  },
  activity: [
    { ts: '2026-09-30T00:00:00Z', critical: 0, high: 1, total: 2 },
    { ts: '2026-10-01T00:00:00Z', critical: 0, high: 2, total: 3 },
  ],
  availability: {
    estate: true,
    violations: true,
    response: true,
    attention: true,
    protection: true,
    activity: true,
  },
};

describe('Control Room at multiple breakpoints', () => {
  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (vi.spyOn(useApiClientModule, 'useApiClient') as any).mockReturnValue({
      getControlRoomExecutiveOverview: vi.fn().mockResolvedValue(overview),
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (vi.spyOn(useTenantModule, 'useTenant') as any).mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 'Bank Tenant', created_at: '' },
      tenants: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '' }],
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
      loading: false,
      error: null,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  viewports.forEach((vp) => {
    it(`renders at ${vp.label}`, async () => {
      Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: vp.width });
      Object.defineProperty(window, 'innerHeight', { writable: true, configurable: true, value: vp.height });
      window.matchMedia = ((query: string) => ({
        matches: query.includes(`max-width: ${vp.width}px`),
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })) as typeof window.matchMedia;

      render(
        <MemoryRouter>
          <ControlRoom />
        </MemoryRouter>,
      );

      expect(await screen.findByText('Infrastructure health')).toBeInTheDocument();
      expect(screen.getAllByText('Rule violations').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Handled automatically').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Needs attention').length).toBeGreaterThan(0);
      expect(screen.getByText('Patch payments-db-02')).toBeInTheDocument();
    });
  });
});
