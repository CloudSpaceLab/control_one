import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthProvider, useAuth } from './AuthProvider';

function SignOutHarness(): JSX.Element {
  const { isAuthenticated, signOut } = useAuth();
  return (
    <button type="button" onClick={() => { void signOut(); }}>
      {isAuthenticated ? 'Sign out' : 'Signed out'}
    </button>
  );
}

describe('AuthProvider', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  it('revokes the active backend session before clearing local sign-in state', async () => {
    window.localStorage.setItem('control-one-token', 'active-session-token');
    const fetchMock = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
      void init;
      const href = String(url);
      if (href.endsWith('/api/v1/me')) {
        return new Response(
          JSON.stringify({
            subject: 'user-1',
            email: 'admin@local',
            display_name: 'Default Admin',
            roles: ['admin'],
            groups: [],
            permissions: [],
            type: 'user',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      if (href.endsWith('/api/v1/auth/logout')) {
        return new Response(null, { status: 200 });
      }
      return new Response('not found', { status: 404 });
    });
    vi.stubGlobal('fetch', fetchMock);

    render(
      <AuthProvider>
        <SignOutHarness />
      </AuthProvider>,
    );

    await screen.findByText('Sign out');
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }));

    await waitFor(() => {
      const logoutCall = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/auth/logout'));
      expect(logoutCall?.[1]?.method).toBe('POST');
      expect((logoutCall?.[1]?.headers as Record<string, string>).Authorization).toBe('Bearer active-session-token');
    });
    await waitFor(() => expect(window.localStorage.getItem('control-one-token')).toBeNull());
    expect(await screen.findByText('Signed out')).toBeInTheDocument();
  });
});
