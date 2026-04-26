import { FormEvent, useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { buildAuthorizationUrl } from '../lib/oidc';
import { isOidcConfigured } from '../config/oidc';
import { useAuth } from '../providers/AuthProvider';
import { APIClient } from '../lib/api';

interface RedirectState {
  from?: string;
}

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
      if (!trimmed) throw new Error('Token is required');
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
    <div className="login-page">
      {/* ── Left branding panel ── */}
      <div className="login-left">
        <div className="login-left__grid" aria-hidden="true" />

        <div className="login-brand">
          <span className="login-brand__mark" aria-hidden="true">◎</span>
          <span className="login-brand__name">Control One</span>
        </div>

        <div className="login-hero">
          <h1>
            Find risk.<br />
            Fix it.<br />
            <em>Prove it.</em>
          </h1>
          <p>
            Unified compliance, threat detection, just-in-time access, and infrastructure
            provisioning across every node in your fleet.
          </p>
        </div>

        <div className="login-features">
          <div className="login-feature">
            <div className="login-feature__icon">🛡</div>
            <div className="login-feature__text">
              <strong>Continuous compliance</strong>
              <span>SOC 2, ISO 27001, CIS benchmarks — automated evidence collection.</span>
            </div>
          </div>
          <div className="login-feature">
            <div className="login-feature__icon">⚡</div>
            <div className="login-feature__text">
              <strong>Real-time threat detection</strong>
              <span>Log, port, and anomaly rules with sub-second enforcement.</span>
            </div>
          </div>
          <div className="login-feature">
            <div className="login-feature__icon">🔑</div>
            <div className="login-feature__text">
              <strong>Zero standing privilege</strong>
              <span>JIT access with full session recording and automatic expiry.</span>
            </div>
          </div>
        </div>
      </div>

      {/* ── Right form panel ── */}
      <div className="login-right">
        {/* Mobile-only brand */}
        <div className="login-mobile-brand">
          <span className="login-brand__mark" aria-hidden="true">◎</span>
          <span className="login-brand__name">Control One</span>
        </div>

        <div className="login-card">
          <div>
            <h2>Sign in</h2>
            <p>Welcome back. Sign in to your operator console.</p>
          </div>

          <form onSubmit={handleEmailSubmit}>
            <div className="login-field">
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
            </div>
            <div className="login-field">
              <label htmlFor="password">Password</label>
              <input
                id="password"
                type="password"
                autoComplete="current-password"
                placeholder="••••••••••••"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={emailLoading}
                required
              />
            </div>
            {emailError ? <span className="form-error">{emailError}</span> : null}
            <div className="login-submit">
              <button type="submit" className="primary-button" disabled={emailLoading}>
                {emailLoading ? 'Signing in…' : 'Sign in'}
              </button>
              <a href="https://control-one.cloudspacetechs.com/" className="login-back">
                ← Back to home
              </a>
            </div>
          </form>

          <button
            type="button"
            className="login-advanced-toggle"
            onClick={() => setShowAdvanced((v) => !v)}
          >
            {showAdvanced ? '↑ Hide' : '↓ More'} sign-in options
          </button>

          {showAdvanced ? (
            <>
              {oidcEnabled ? (
                <div className="login-card__section">
                  <h3>Single Sign-On</h3>
                  <button type="button" className="primary-button" onClick={handleSso} disabled={ssoLoading}>
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
                    style={{ marginTop: '0.4rem' }}
                  />
                  {localError ? <span className="form-error">{localError}</span> : null}
                  {error ? <span className="form-error">{error}</span> : null}
                  <button type="submit" className="primary-button" disabled={loading} style={{ marginTop: '0.75rem', width: '100%', justifyContent: 'center' }}>
                    {loading ? 'Signing in…' : 'Continue'}
                  </button>
                </form>
              </div>
            </>
          ) : null}
        </div>
      </div>
    </div>
  );
}
