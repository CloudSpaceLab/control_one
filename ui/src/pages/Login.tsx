import { FormEvent, useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { buildAuthorizationUrl } from '../lib/oidc';
import { isOidcConfigured } from '../config/oidc';
import { useAuth } from '../providers/AuthProvider';
import { APIClient } from '../lib/api';

interface RedirectState {
  from?: string;
}

// Email/password form is the default. SSO + bearer-token paths sit beneath
// "More sign-in options" so the page stays clean for the common case.
export function Login(): JSX.Element {
  const { signIn, loading, error, isAuthenticated } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [emailError, setEmailError] = useState<string | null>(null);
  const [emailLoading, setEmailLoading] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [token, setToken] = useState('');
  const [localError, setLocalError] = useState<string | null>(null);
  const [ssoError, setSsoError] = useState<string | null>(null);
  const [ssoLoading, setSsoLoading] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const returnTo = useMemo(() => {
    const state = location.state as RedirectState | null;
    return state?.from;
  }, [location.state]);

  useEffect(() => {
    if (isAuthenticated) {
      navigate(returnTo ?? '/', { replace: true });
    }
  }, [isAuthenticated, navigate, returnTo]);

  // Email/password — primary path. Hits POST /api/v1/auth/login, stores
  // the returned session token via signIn so subsequent requests are
  // Bearer-authed.
  const handleEmailSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setEmailError(null);
    if (!email.trim() || !password) {
      setEmailError('Email and password required');
      return;
    }
    try {
      setEmailLoading(true);
      const client = new APIClient();
      const resp = await client.loginWithPassword(email.trim(), password);
      await signIn(resp.token);
    } catch (err) {
      setEmailError(err instanceof Error ? err.message : 'Sign in failed');
    } finally {
      setEmailLoading(false);
    }
  };

  const handleTokenSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    try {
      const trimmed = token.trim();
      if (!trimmed) {
        throw new Error('Token is required');
      }
      setLocalError(null);
      await signIn(trimmed);
    } catch (err) {
      setLocalError(err instanceof Error ? err.message : 'Unable to sign in');
    }
  };

  const handleSso = async () => {
    try {
      setSsoError(null);
      setSsoLoading(true);
      const url = await buildAuthorizationUrl(returnTo);
      window.location.assign(url);
    } catch (err) {
      setSsoError(err instanceof Error ? err.message : 'Unable to start sign-in');
      setSsoLoading(false);
    }
  };

  if (isAuthenticated) {
    return <Navigate to={returnTo ?? '/'} replace />;
  }

  const oidcEnabled = isOidcConfigured();

  return (
    <section className="login-card">
      <h2>Sign in to Control One</h2>
      <p>
        Welcome back. Use your email + password, your single sign-on provider, or a developer
        bearer token.
      </p>

      <div className="login-card__section">
        <form onSubmit={handleEmailSubmit}>
          <label htmlFor="email">Email</label>
          <input
            id="email"
            type="email"
            autoComplete="email"
            placeholder="you@company.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={emailLoading}
            required
          />
          <label htmlFor="password" style={{ marginTop: 12 }}>
            Password
          </label>
          <input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={emailLoading}
            required
          />
          {emailError ? <span className="form-error">{emailError}</span> : null}
          <button type="submit" className="primary" disabled={emailLoading} style={{ marginTop: 16 }}>
            {emailLoading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
        <p style={{ marginTop: 12, fontSize: 13 }}>
          <a href="/" style={{ color: 'var(--text-secondary)' }}>
            ← Back to home
          </a>
        </p>
      </div>

      <button
        type="button"
        onClick={() => setShowAdvanced((v) => !v)}
        style={{
          background: 'transparent',
          border: 'none',
          color: 'var(--state-info)',
          cursor: 'pointer',
          marginTop: 8,
          fontSize: 13,
        }}
      >
        {showAdvanced ? 'Hide' : 'More'} sign-in options
      </button>

      {showAdvanced ? (
        <>
          {oidcEnabled ? (
            <div className="login-card__section">
              <h3>Single Sign-On</h3>
              <button type="button" className="primary" onClick={handleSso} disabled={ssoLoading}>
                {ssoLoading ? 'Redirecting…' : 'Continue with SSO'}
              </button>
              {ssoError ? <span className="form-error">{ssoError}</span> : null}
            </div>
          ) : null}

          <div className="login-card__section">
            <h3>Developer bearer token</h3>
            <form onSubmit={handleTokenSubmit}>
              <label htmlFor="token">Bearer token</label>
              <input
                id="token"
                name="token"
                type="text"
                placeholder="Paste JWT or static token"
                value={token}
                onChange={(event) => setToken(event.target.value)}
                disabled={loading}
              />
              {localError ? <span className="form-error">{localError}</span> : null}
              {error ? <span className="form-error">{error}</span> : null}
              <button type="submit" disabled={loading}>
                {loading ? 'Signing in…' : 'Continue'}
              </button>
            </form>
          </div>
        </>
      ) : null}
    </section>
  );
}
