import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { EntityHeader } from './EntityHeader';

const entityActionMock = vi.hoisted(() => vi.fn());

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => ({
    entityAction: entityActionMock,
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
    entityActionMock.mockResolvedValue({ nodes_dispatched: 2 });
  });

  it('uses one state-aware IP response menu on the IP lifecycle page', async () => {
    const user = userEvent.setup();

    render(
      <MemoryRouter>
        <EntityHeader type="ip" id="45.135.193.156" canMutate />
      </MemoryRouter>,
    );

    expect(screen.getByRole('button', { name: /copy id/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /block ip/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^allow$/i })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /ip response actions/i }));

    const menu = screen.getByRole('menu');
    expect(within(menu).getByText('Governed response: review scope, TTL, receipts, and rollback before dispatch.')).toBeInTheDocument();
    expect(within(menu).getByText('Review affected-node block')).toBeInTheDocument();
    expect(within(menu).getByText('Review fleet-wide block')).toBeInTheDocument();
    expect(within(menu).getByText('Review unblock')).toBeInTheDocument();
    expect(within(menu).getByText('Active block receipts')).toBeInTheDocument();
    expect(within(menu).queryByText('View in Investigate')).not.toBeInTheDocument();
    expect(within(menu).queryByText('Copy IP')).not.toBeInTheDocument();
  });

  it('requires review confirmation before dispatching IP response work', async () => {
    const user = userEvent.setup();

    render(
      <MemoryRouter>
        <EntityHeader type="ip" id="45.135.193.156" canMutate />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /ip response actions/i }));
    await user.click(screen.getByText('Review affected-node block'));

    expect(entityActionMock).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog', { name: 'Review affected-node block' })).toBeInTheDocument();
    expect(screen.getByText('Affected nodes')).toBeInTheDocument();
    expect(screen.getByText('24h TTL')).toBeInTheDocument();
    expect(screen.getByText(/Active Blocks must show node receipts/i)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Dispatch affected-node block' }));

    expect(entityActionMock).toHaveBeenCalledWith(
      'ip',
      '45.135.193.156',
      expect.objectContaining({
        action: 'block',
        scope: 'affected',
        ttl: 86400,
        reason: expect.stringContaining('receipt_required=true'),
      }),
      { tenantId: 'tenant-1' },
    );
  });
});
