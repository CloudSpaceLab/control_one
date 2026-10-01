import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { EntityHeader } from './EntityHeader';

const entityActionMock = vi.hoisted(() => vi.fn());
const getIPBlockStatusMock = vi.hoisted(() => vi.fn());
const getTenantRemediationConfigMock = vi.hoisted(() => vi.fn());

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => ({
    entityAction: entityActionMock,
    getIPBlockStatus: getIPBlockStatusMock,
    getTenantRemediationConfig: getTenantRemediationConfigMock,
  }),
}));

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({
    currentTenantId: 'tenant-1',
  }),
}));

describe('EntityHeader', () => {
  beforeEach(() => {
    entityActionMock.mockReset();
    getIPBlockStatusMock.mockReset();
    getTenantRemediationConfigMock.mockReset();
    entityActionMock.mockResolvedValue({ nodes_dispatched: 2 });
    getIPBlockStatusMock.mockResolvedValue({
      active: false,
      state: 'unblocked',
      scope: 'affected',
      fleet_nodes: 4,
      target_nodes: 0,
      nodes_applied: 0,
      nodes_pending: 0,
      nodes_failed: 0,
    });
    getTenantRemediationConfigMock.mockResolvedValue({
      AutoBlockEnabled: true,
      AutoBlockMinConfidence: 100,
      DefaultIPBlockScope: 'affected',
      DefaultIPBlockTTLSeconds: 3600,
      RequireCorroboratingThreatIntel: true,
      PatchRequiresApproval: true,
    });
  });

  it('shows concise IP state and actions without a review dialog', async () => {
    const user = userEvent.setup();

    render(
      <MemoryRouter>
        <EntityHeader type="ip" id="45.135.193.156" canMutate />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /ip response actions/i }));

    const menu = screen.getByRole('menu');
    await waitFor(() => expect(within(menu).getByText('Not blocked')).toBeInTheDocument());
    expect(within(menu).getByText('No active Control One block')).toBeInTheDocument();
    expect(within(menu).getByText('Block IP')).toBeInTheDocument();
    expect(within(menu).getByText('Block fleet-wide')).toBeInTheDocument();
    expect(within(menu).getByText('View block details')).toBeInTheDocument();
    expect(within(menu).queryByText(/Governed response/i)).not.toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('dispatches the normal block directly with a concise audit reason', async () => {
    const user = userEvent.setup();

    render(
      <MemoryRouter>
        <EntityHeader type="ip" id="45.135.193.156" canMutate />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /ip response actions/i }));
    await screen.findByText('Affected · 1h');
    await user.click(screen.getByText('Block IP'));

    expect(entityActionMock).toHaveBeenCalledWith(
      'ip',
      '45.135.193.156',
      {
        action: 'block',
        scope: 'affected',
        ttl: 3600,
        reason: 'Manual IP block',
      },
      { tenantId: 'tenant-1' },
    );
  });

  it('shows current fleet-wide coverage and only the relevant next action', async () => {
    const user = userEvent.setup();
    getIPBlockStatusMock.mockResolvedValue({
      active: true,
      state: 'blocked',
      scope: 'fleet',
      fleet_nodes: 4,
      target_nodes: 4,
      nodes_applied: 4,
      nodes_pending: 0,
      nodes_failed: 0,
      provenance: 'manual',
    });

    render(
      <MemoryRouter>
        <EntityHeader type="ip" id="45.135.193.156" canMutate />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /ip response actions/i }));

    const menu = screen.getByRole('menu');
    await waitFor(() => expect(within(menu).getByText('Blocked')).toBeInTheDocument());
    expect(within(menu).getByText('Manually blocked · Fleet-wide · 4/4 applied')).toBeInTheDocument();
    expect(within(menu).getByText('Allow IP')).toBeInTheDocument();
    expect(within(menu).queryByText('Extend to fleet')).not.toBeInTheDocument();
    expect(within(menu).queryByText('Block IP')).not.toBeInTheDocument();
  });
});
