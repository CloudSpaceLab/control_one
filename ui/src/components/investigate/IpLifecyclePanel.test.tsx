import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { IpLifecyclePanel } from './IpLifecyclePanel';

const connectionState = vi.hoisted(() => ({
  value: {
    data: [],
    isLoading: false,
    error: new Error('connections lane unavailable'),
  },
}));

vi.mock('@/hooks/useConnectionsByIp', () => ({
  useConnectionsByIp: () => connectionState.value,
}));

vi.mock('@/hooks/useNodes', () => ({
  useNodes: () => ({ data: [] }),
}));

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1' }),
}));

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => ({
    entityAction: vi.fn(),
  }),
}));

describe('IpLifecyclePanel', () => {
  it('does not show an empty-state success message when the Doris lane errors', () => {
    render(
      <MemoryRouter>
        <IpLifecyclePanel ip="45.135.193.156" />
      </MemoryRouter>,
    );

    expect(screen.getByText('connections lane unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No lifecycles found')).not.toBeInTheDocument();
  });
});
