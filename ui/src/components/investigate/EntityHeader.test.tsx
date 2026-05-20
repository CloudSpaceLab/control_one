import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { EntityHeader } from './EntityHeader';

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => ({
    entityAction: vi.fn(),
  }),
}));

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({
    currentTenantId: 'tenant-1',
  }),
}));

describe('EntityHeader', () => {
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
    expect(within(menu).getByText('Block on affected nodes')).toBeInTheDocument();
    expect(within(menu).getByText('Block fleet-wide')).toBeInTheDocument();
    expect(within(menu).getByText('Unblock (allow)')).toBeInTheDocument();
    expect(within(menu).queryByText('View in Investigate')).not.toBeInTheDocument();
    expect(within(menu).queryByText('Copy IP')).not.toBeInTheDocument();
  });
});
