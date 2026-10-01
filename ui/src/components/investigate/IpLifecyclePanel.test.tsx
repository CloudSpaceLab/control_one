import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { IpLifecyclePanel } from './IpLifecyclePanel';

const connectionState = vi.hoisted(() => ({
  mode: 'error' as 'error' | 'degraded',
}));

vi.mock('@/hooks/useConnectionsByIp', () => ({
  useConnectionsByIp: () =>
    connectionState.mode === 'error'
      ? {
          data: undefined,
          isLoading: false,
          error: new Error('connections lane unavailable'),
        }
      : {
          data: {
            rows: [],
            source: 'small-analytics',
            degraded: true,
            guardrails: ['Connection evidence unavailable. Check analytics health and retry.'],
          },
          isLoading: false,
          error: null,
        },
}));

vi.mock('@/hooks/useNodes', () => ({
  useNodes: () => ({ data: [] }),
}));

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1' }),
}));

vi.mock('@/hooks/useApiClient', () => ({
  // Match the real hook's stable client so detail-sheet effects do not rerun
  // on every state update while the sheet is closed.
  useApiClient: vi.fn().mockReturnValue({ entityAction: vi.fn() }),
}));

describe('IpLifecyclePanel', () => {
  beforeEach(() => {
    connectionState.mode = 'error';
  });

  it('does not show an empty-state success message when the analytics lane errors', () => {
    render(
      <MemoryRouter>
        <IpLifecyclePanel ip="45.135.193.156" />
      </MemoryRouter>,
    );

    expect(screen.getByText('connections lane unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No lifecycles found')).not.toBeInTheDocument();
  });

  it('shows an explicit degraded state without treating missing evidence as no activity', () => {
    connectionState.mode = 'degraded';

    render(
      <MemoryRouter>
        <IpLifecyclePanel ip="45.135.193.156" />
      </MemoryRouter>,
    );

    expect(
      screen.getByText('Connection evidence unavailable. Check analytics health and retry.'),
    ).toBeInTheDocument();
    expect(screen.queryByText('No lifecycles found')).not.toBeInTheDocument();
  });
});
