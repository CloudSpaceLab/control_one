import { FormEvent, useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { buildAuthorizationUrl } from '../lib/oidc';
import { isOidcConfigured } from '../config/oidc';
import { useAuth } from '../providers/AuthProvider';

interface RedirectState {
  from?: string;
}

export function Login(): JSX.Element {
  const { signIn, loading, error, isAuthenticated } = useAuth();
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
      navigate('/', { replace: true });
    }
  }, [isAuthenticated, navigate]);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
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
    return <Navigate to="/" replace />;
  }

  const oidcEnabled = isOidcConfigured();

  return (
    <section className="login-card">
      <h2>Sign in</h2>
      <p>Authenticate with your organization-issued identity provider to access the control plane.</p>

      {oidcEnabled ? (
        <div className="login-card__section">
          <button type="button" className="primary" onClick={handleSso} disabled={ssoLoading}>
            {ssoLoading ? 'Redirecting…' : 'Continue with Single Sign-On'}
          </button>
          {ssoError ? <span className="form-error">{ssoError}</span> : null}
        </div>
      ) : null}

      <div className="login-card__section">
        <h3>Developer token</h3>
        <form onSubmit={handleSubmit}>
          <label htmlFor="token">Bearer token</label>
          <input
            id="token"
            name="token"
            type="text"
            placeholder="Paste JWT here"
            value={token}
            onChange={(event) => setToken(event.target.value)}
            disabled={loading}
            required
          />
          {localError ? <span className="form-error">{localError}</span> : null}
          {error ? <span className="form-error">{error}</span> : null}
          <button type="submit" disabled={loading}>
            {loading ? 'Signing in…' : 'Continue'}
          </button>
        </form>
      </div>
    </section>
  );
}
