import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AllTenantNetworkSummary } from './AllTenantNetworkSummary';

const mocks = vi.hoisted(() => ({
  getIPBehaviorOverview: vi.fn(),
  listBlockProposals: vi.fn(),
  getControlRoomOverview: vi.fn(),
  setCurrentTenantId: vi.fn(),
  tenants: [
    { id: 'tenant-1', name: 'Bank A', created_at: '2026-01-01T00:00:00Z' },
    { id: 'tenant-2', name: 'Bank B', created_at: '2026-01-02T00:00:00Z' },
  ],
}));

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => ({
    getIPBehaviorOverview: mocks.getIPBehaviorOverview,
    listBlockProposals: mocks.listBlockProposals,
    getControlRoomOverview: mocks.getControlRoomOverview,
  }),
}));

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({
    tenants: mocks.tenants,
    currentTenantId: null,
    currentTenant: null,
    loading: false,
    error: null,
    setCurrentTenantId: mocks.setCurrentTenantId,
    refresh: vi.fn(),
  }),
}));

describe('AllTenantNetworkSummary', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getIPBehaviorOverview.mockImplementation(async ({ tenantId }: { tenantId: string }) => ({
      tenant_id: tenantId,
      since: '2026-10-02T06:00:00Z',
      request_count: tenantId === 'tenant-1' ? 100 : 200,
      bytes_out: tenantId === 'tenant-1' ? 1000 : 2000,
      status_counts: tenantId === 'tenant-1'
        ? { '401': 2, '500': 1 }
        : { '403': 3, '503': 2 },
      top_countries: [],
      generated_at: '2026-10-02T07:00:00Z',
    }));
    mocks.listBlockProposals.mockImplementation(async ({ tenantId, status }: { tenantId: string; status: string }) => ({
      data: [],
      pagination: {
        total: tenantId === 'tenant-1' ? 2 : status === 'active' ? 4 : 3,
        count: 0,
        limit: 1,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    }));
  });

  it('sums exact per-tenant IP behavior totals and keeps tenant drill-in explicit', async () => {
    const user = userEvent.setup();
    render(<AllTenantNetworkSummary mode="ip-behavior" since="2026-10-02T06:00:00Z" />);

    expect(await screen.findByText('Bank A')).toBeInTheDocument();
    expect(screen.getByText('Bank B')).toBeInTheDocument();
    expect(screen.getByText('300')).toBeInTheDocument();
    expect(screen.getByText('5')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();

    const openButtons = screen.getAllByRole('button', { name: /open tenant/i });
    await user.click(openButtons[1]);
    expect(mocks.setCurrentTenantId).toHaveBeenCalledWith('tenant-2');
  });

  it('uses exact paginated proposal counts instead of merging bounded rows', async () => {
    render(<AllTenantNetworkSummary mode="approvals" />);

    expect(await screen.findByText('5')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
    await waitFor(() => expect(mocks.listBlockProposals).toHaveBeenCalledTimes(2));
    expect(mocks.listBlockProposals).toHaveBeenCalledWith(expect.objectContaining({ limit: 1, offset: 0, status: 'proposed' }));
  });

  it('marks partial tenant failures instead of silently undercounting', async () => {
    mocks.getIPBehaviorOverview.mockImplementation(async ({ tenantId }: { tenantId: string }) => {
      if (tenantId === 'tenant-2') throw new Error('behavior store unavailable');
      return {
        tenant_id: tenantId,
        since: '2026-10-02T06:00:00Z',
        request_count: 100,
        bytes_out: 1000,
        status_counts: {},
        top_countries: [],
        generated_at: '2026-10-02T07:00:00Z',
      };
    });

    render(<AllTenantNetworkSummary mode="ip-behavior" />);

    expect(await screen.findByText('1 tenant view is unavailable. Totals exclude unavailable tenants.')).toBeInTheDocument();
    expect(screen.getByText('behavior store unavailable')).toBeInTheDocument();
    expect(screen.getByText('1 of 2 tenant views loaded')).toBeInTheDocument();
  });
});
