import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ControlRoom } from './ControlRoom';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantModule from '../providers/TenantProvider';
import type { ControlRoomExecutiveOverview } from '../lib/api';

const executiveOverview: ControlRoomExecutiveOverview = {
  tenant_id: 'tenant-1',
  generated_at: '2026-10-01T17:00:00Z',
  period: '7d',
  estate: {
    groups_total: 13,
    groups_healthy: 12,
    groups_degraded: 1,
    groups_critical: 0,
    groups_unknown: 0,
    nodes_total: 187,
    nodes_healthy: 184,
    groups: [
      {
        name: 'Payments',
        state: 'healthy',
        nodes_total: 18,
        nodes_healthy: 18,
        nodes_stale: 0,
        nodes_offline: 0,
        intentionally_isolated: 0,
        drilldown: '/nodes',
      },
      {
        name: 'Web Edge',
        state: 'degraded',
        nodes_total: 24,
        nodes_healthy: 21,
        nodes_stale: 2,
        nodes_offline: 1,
        intentionally_isolated: 0,
        drilldown: '/nodes',
      },
    ],
  },
  violations: {
    total: 37,
    critical: 3,
    high: 11,
    medium: 18,
    low: 5,
    info: 0,
    other: 0,
    previous_total: 45,
    delta_pct: -17.8,
    top_rules: [
      {
        rule_id: 'rule-1',
        name: 'Restricted port',
        rule_type: 'port',
        severity: 'critical',
        count: 12,
        drilldown: '/rules',
      },
    ],
  },
  response: {
    handled_automatically: 31,
    blocked: 14,
    contained: 9,
    remediated: 8,
    failed: 0,
  },
  attention: {
    total: 6,
    critical: 2,
    reviews: 2,
    approvals: 2,
    interventions: 2,
    items: [
      {
        id: 'alert-1',
        kind: 'review',
        severity: 'critical',
        domain: 'alerts',
        title: 'Suspicious database access',
        reason: 'Unexpected privileged access',
        created_at: '2026-10-01T16:45:00Z',
        drilldown: '/alerts?alert_id=alert-1',
      },
      {
        id: 'approval-1',
        kind: 'approval',
        severity: 'medium',
        domain: 'patch',
        title: 'Patch payment-db-02',
        reason: 'airgapped',
        created_at: '2026-10-01T16:20:00Z',
        drilldown: '/infrastructure/patch',
      },
    ],
  },
  protection: {
    protected: 176,
    total: 187,
    percentage: 94.12,
    gaps: 11,
    gap_types: [
      { type: 'Firewall state unknown', count: 6 },
      { type: 'Firewall default allow', count: 5 },
    ],
  },
  activity: [
    { ts: '2026-09-29T00:00:00Z', critical: 1, high: 2, total: 8 },
    { ts: '2026-09-30T00:00:00Z', critical: 0, high: 3, total: 12 },
    { ts: '2026-10-01T00:00:00Z', critical: 2, high: 4, total: 17 },
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

let getExecutiveOverviewMock: ReturnType<typeof vi.fn>;

function renderControlRoom() {
  return render(
    <MemoryRouter>
      <ControlRoom />
    </MemoryRouter>,
  );
}

describe('ControlRoom executive dashboard', () => {
  beforeEach(() => {
    getExecutiveOverviewMock = vi.fn().mockResolvedValue(executiveOverview);
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue({
      getControlRoomExecutiveOverview: getExecutiveOverviewMock,
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any);
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 'Bank Tenant', created_at: '2024-01-01' },
      tenants: [],
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('loads the executive overview with the CISO default period', async () => {
    renderControlRoom();

    await waitFor(() => {
      expect(getExecutiveOverviewMock).toHaveBeenCalledWith('tenant-1', '7d');
    });
    expect(await screen.findByRole('heading', { name: '2 critical items need attention' })).toBeInTheDocument();
    expect(screen.getByText('12 of 13 infrastructure groups healthy · 94% protection coverage')).toBeInTheDocument();
  });

  it('shows the four canonical decision metrics', async () => {
    renderControlRoom();
    await screen.findByText('Suspicious database access');

    expect(screen.getByText('Infrastructure health')).toBeInTheDocument();
    expect(screen.getByText('Rule violations')).toBeInTheDocument();
    expect(screen.getByText('Handled automatically')).toBeInTheDocument();
    expect(screen.getAllByText('Needs attention').length).toBeGreaterThan(0);

    expect(screen.getByText('12 / 13')).toBeInTheDocument();
    expect(screen.getByText('37')).toBeInTheDocument();
    expect(screen.getAllByText('31').length).toBeGreaterThan(0);
    expect(screen.getByText('184 / 187 nodes healthy')).toBeInTheDocument();
  });

  it('keeps human work directly actionable and semantically typed', async () => {
    renderControlRoom();

    const alert = await screen.findByRole('link', { name: /Suspicious database access/i });
    expect(alert).toHaveAttribute('href', '/alerts?alert_id=alert-1');
    expect(screen.getByText('Review')).toBeInTheDocument();
    expect(screen.getByText('Approval')).toBeInTheDocument();
  });

  it('reveals metric detail on interaction instead of loading it into the page', async () => {
    const user = userEvent.setup();
    renderControlRoom();

    await screen.findByText('Suspicious database access');
    await user.click(screen.getByRole('button', { name: 'Infrastructure health: view details' }));

    expect(await screen.findByRole('heading', { name: 'Infrastructure health' })).toBeInTheDocument();
    expect(screen.getByText('Payments')).toBeInTheDocument();
    expect(screen.getByText('Web Edge')).toBeInTheDocument();
  });

  it('uses factual listener coverage copy rather than implying a node denominator', async () => {
    renderControlRoom();

    expect(await screen.findByText('176 / 187')).toBeInTheDocument();
    expect(screen.getByText(/public listeners protected/i)).toBeInTheDocument();
    expect(screen.getByText('11 protection gaps')).toBeInTheDocument();
  });

  it('does not claim healthy state when a dashboard domain is unavailable', async () => {
    getExecutiveOverviewMock.mockResolvedValue({
      ...executiveOverview,
      availability: {
        ...executiveOverview.availability,
        protection: false,
      },
    });

    renderControlRoom();

    expect(await screen.findByRole('heading', { name: 'Protection status incomplete' })).toBeInTheDocument();
    expect(screen.getByText('Bank Tenant · Some security or infrastructure data is unavailable.')).toBeInTheDocument();
    expect(screen.getAllByText('Data unavailable').length).toBeGreaterThan(0);
  });

  it('does not request data before a tenant is selected', () => {
    vi.mocked(useTenantModule.useTenant).mockReturnValue({
      currentTenantId: null,
      currentTenant: null,
      tenants: [],
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });

    renderControlRoom();

    expect(screen.getByText('Select a tenant')).toBeInTheDocument();
    expect(getExecutiveOverviewMock).not.toHaveBeenCalled();
  });
});
