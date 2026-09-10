import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const authState = vi.hoisted(() => ({
  isAuthenticated: false,
}));

vi.mock('./providers/AuthProvider', () => ({
  useAuth: () => ({
    isAuthenticated: authState.isAuthenticated,
  }),
}));

vi.mock('./components/MainLayout', async () => {
  const { Outlet } = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    MainLayout: () => <Outlet />,
  };
});

vi.mock('./pages/ControlRoom', () => ({
  ControlRoom: () => <div data-testid="control-room" />,
}));

vi.mock('./pages/ControlRoomDrilldown', () => ({
  ControlRoomDrilldown: () => <div data-testid="control-room-drilldown" />,
}));

vi.mock('./pages/Login', async () => {
  const { useLocation } = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    Login: () => {
      const location = useLocation();
      return <div data-testid="login-state">{JSON.stringify(location.state)}</div>;
    },
  };
});

vi.mock('./pages/Observability', () => ({
  Observability: () => {
    throw new Error('Observability render failed');
  },
}));

import { App } from './App';

describe('App routing', () => {
  beforeEach(() => {
    authState.isAuthenticated = false;
  });

  it('preserves protected deep links through login redirects', async () => {
    authState.isAuthenticated = false;

    render(
      <MemoryRouter initialEntries={['/security/network?tab=connections#row-7']}>
        <App />
      </MemoryRouter>,
    );

    await expect(screen.findByTestId('login-state')).resolves.toHaveTextContent(
      JSON.stringify({ from: '/security/network?tab=connections#row-7' }),
    );
  });

  it('shows route recovery when a console module fails', async () => {
    authState.isAuthenticated = true;
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined);

    try {
      render(
        <MemoryRouter initialEntries={['/observability']}>
          <App />
        </MemoryRouter>,
      );

      expect(await screen.findByRole('heading', { name: 'Console module failed' })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Reload console' })).toBeInTheDocument();
    } finally {
      consoleError.mockRestore();
    }
  });
});
