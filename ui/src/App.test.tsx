import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

const authState = vi.hoisted(() => ({
  isAuthenticated: false,
}));

vi.mock('./providers/AuthProvider', () => ({
  useAuth: () => ({
    isAuthenticated: authState.isAuthenticated,
  }),
}));

vi.mock('./components/MainLayout', () => ({
  MainLayout: () => <div data-testid="main-layout" />,
}));

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

import { App } from './App';

describe('App routing', () => {
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
});
