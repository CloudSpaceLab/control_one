import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { Login } from './Login';

const authState = vi.hoisted(() => ({
  error: null as string | null,
  isAuthenticated: false,
}));

vi.mock('../providers/AuthProvider', () => ({
  useAuth: () => ({
    signIn: vi.fn(),
    loading: false,
    error: authState.error,
    isAuthenticated: authState.isAuthenticated,
  }),
}));

vi.mock('../config/oidc', () => ({
  isOidcConfigured: () => false,
}));

describe('Login', () => {
  it('shows expired-session guidance on the primary email sign-in form', () => {
    authState.error = 'Session has expired. Please sign in again.';
    authState.isAuthenticated = false;

    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>,
    );

    expect(screen.getByRole('alert')).toHaveTextContent('Session has expired. Please sign in again.');
  });
});
