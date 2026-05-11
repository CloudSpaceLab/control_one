import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Dashboard } from './Dashboard';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantModule from '../providers/TenantProvider';
import * as useTenantsModule from '../hooks/useTenants';
import type { DashboardOverview } from '../lib/api';

const overview: DashboardOverview = {
  generated_at: new Date().toISOString(),
  node_counts: { total: 3, healthy: 2, offline: 1 },
  security_event_counts: { critical: 1, high: 2, medium: 0, low: 0, total: 3 },
  health_incident_counts: { critical: 0, high: 1, medium: 0, low: 0, total: 1 },
  compliance_summary: { total: 10, passed: 8, failed: 2 },
  rule_trigger_counts_24h: { port: 4, log: 1 },
  remediations_applied_24h: 2,
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function stubClient(): any {
  return {
    getDashboardOverview: vi.fn().mockResolvedValue(overview),
    getRiskScore: vi.fn().mockResolvedValue({
      score: 88,
      max_score: 100,
      percent: 88,
      trend_direction: 'stable',
      trend_delta: 0,
      components: [],
      calculated_at: new Date().toISOString(),
    }),
    getMTTDMetrics: vi.fn().mockResolvedValue({
      severity: 'critical',
      mean_minutes: 240,
      median_minutes: 180,
      p95_minutes: 360,
      event_count: 3,
      period: '7d',
      calculated_at: new Date().toISOString(),
    }),
    getMTTRMetrics: vi.fn().mockResolvedValue({
      severity: 'critical',
      mean_minutes: 720,
      median_minutes: 600,
      p95_minutes: 1440,
      remediation_count: 2,
      period: '7d',
      calculated_at: new Date().toISOString(),
    }),
    getRemediationVelocity: vi.fn().mockResolvedValue({
      period: '30d',
      period_count: 30,
      remediations: 7,
      avg_per_period: 0.23,
      trend_direction: 'stable',
      trend_percent: 0,
    }),
    getFindingsAging: vi.fn().mockResolvedValue({
      severity: 'critical',
      less_than_7_days: 1,
      days_7_to_30: 1,
      days_30_to_90: 0,
      over_90_days: 0,
      total_open: 2,
    }),
    streamEvents: vi.fn().mockReturnValue(() => undefined),
  };
}

describe('Dashboard', () => {
  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (vi.spyOn(useApiClientModule, 'useApiClient') as any).mockReturnValue(stubClient());
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (vi.spyOn(useTenantModule, 'useTenant') as any).mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 't', created_at: '2024-01-01', updated_at: '2024-01-01' },
      tenants: [{ id: 'tenant-1', name: 't', created_at: '2024-01-01', updated_at: '2024-01-01' }],
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
      loading: false,
      error: null,
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (vi.spyOn(useTenantsModule, 'useTenants') as any).mockReturnValue({
      data: [{ id: 'tenant-1', name: 't', created_at: '2024-01-01', updated_at: '2024-01-01' }],
      pagination: { total: 1, count: 1, limit: 1, offset: 0, nextOffset: null, prevOffset: null },
      loading: false,
      error: null,
      refresh: vi.fn(),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the 5-section overview with totals from /dashboard/overview', async () => {
    render(
      <MemoryRouter>
        <Dashboard />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText(/Security events/)).toBeInTheDocument();
    });
    expect(screen.getByText(/Open health incidents/)).toBeInTheDocument();
    expect(screen.getByText(/Compliance alerts/)).toBeInTheDocument();
    expect(screen.getByText(/Rule triggers/)).toBeInTheDocument();
    expect(screen.getByText(/Auto-remediations/)).toBeInTheDocument();
  });
});
