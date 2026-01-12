import { FormEvent, useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { buildAuthorizationUrl } from '../lib/oidc';
import { isOidcConfigured } from '../config/oidc';
import { useAuth } from '../providers/AuthProvider';
import { useTenants } from '../hooks/useTenants';

interface RedirectState {
  from?: string;
}

interface LoginCredentials {
  username: string;
  password: string;
}

const DEFAULT_ADMIN_CREDENTIALS = {
  username: 'admin',
  password: 'admin123'
};

export function Login(): JSX.Element {
  const { signIn, loading, error, isAuthenticated } = useAuth();
  const { pagination: tenantPagination, loading: tenantsLoading } = useTenants({ limit: 1, offset: 0 });
  const [token, setToken] = useState('demo-admin-token');
  const [credentials, setCredentials] = useState<LoginCredentials>({ username: '', password: '' });
  const [localError, setLocalError] = useState<string | null>(null);
  const [ssoError, setSsoError] = useState<string | null>(null);
  const [ssoLoading, setSsoLoading] = useState(false);
  const [loginMethod, setLoginMethod] = useState<'token' | 'credentials'>('credentials');
  const navigate = useNavigate();
  const location = useLocation();
  const returnTo = useMemo(() => {
    const state = location.state as RedirectState | null;
    return state?.from;
  }, [location.state]);

  useEffect(() => {
    if (isAuthenticated && !tenantsLoading) {
      // If no tenants exist, redirect to setup wizard
      if (tenantPagination.total === 0) {
        navigate('/setup', { replace: true });
      } else {
        navigate(returnTo || '/', { replace: true });
      }
    }
  }, [isAuthenticated, navigate, returnTo, tenantPagination.total, tenantsLoading]);

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

  const handleCredentialsSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    try {
      if (!credentials.username.trim() || !credentials.password.trim()) {
        throw new Error('Username and password are required');
      }
      
      // Check against default admin credentials
      if (credentials.username === DEFAULT_ADMIN_CREDENTIALS.username && 
          credentials.password === DEFAULT_ADMIN_CREDENTIALS.password) {
        setLocalError(null);
        // Sign in with a mock admin token
        await signIn('demo-admin-token');
      } else {
        throw new Error('Invalid credentials. Use admin/admin123 for default access.');
      }
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
    <div className="auth-container">
      <div className="auth-card">
        <div className="auth-header">
          <div className="auth-brand">
            <div className="auth-logo">
              <span>C1</span>
            </div>
            <div>
              <h1>Control One</h1>
              <p>Enterprise Control Plane</p>
            </div>
          </div>
        </div>

        <div className="auth-content">
          <div className="auth-welcome">
            <h2>Welcome back</h2>
            <p>Sign in to access your control plane</p>
          </div>

          {/* Compact Login Method Toggle */}
          <div className="auth-methods-compact">
            <div className="toggle-group">
              <button
                type="button"
                className={`toggle-button ${loginMethod === 'credentials' ? 'active' : ''}`}
                onClick={() => setLoginMethod('credentials')}
              >
                Username & Password
              </button>
              <button
                type="button"
                className={`toggle-button ${loginMethod === 'token' ? 'active' : ''}`}
                onClick={() => setLoginMethod('token')}
              >
                Bearer Token
              </button>
            </div>
          </div>

          {/* SSO Section */}
          {oidcEnabled && (
            <div className="auth-divider-compact">
              <span>OR</span>
            </div>
          )}
          
          {oidcEnabled && (
            <div className="auth-sso-compact">
              <button type="button" className="sso-button" onClick={handleSso} disabled={ssoLoading}>
                <div className="sso-icon">
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3"/>
                  </svg>
                </div>
                <span>{ssoLoading ? 'Connecting to SSO…' : 'Continue with SSO'}</span>
              </button>
              {ssoError ? <div className="auth-error-compact">{ssoError}</div> : null}
            </div>
          )}

          {/* Credentials Login */}
          {loginMethod === 'credentials' && (
            <div className="auth-form">
              <form onSubmit={handleCredentialsSubmit}>
                <div className="form-field">
                  <label htmlFor="username">Username</label>
                  <input
                    id="username"
                    name="username"
                    type="text"
                    placeholder="Enter your username"
                    value={credentials.username}
                    onChange={(event) => setCredentials(prev => ({ ...prev, username: event.target.value }))}
                    disabled={loading}
                    required
                    autoComplete="username"
                  />
                </div>
                
                <div className="form-field">
                  <label htmlFor="password">Password</label>
                  <input
                    id="password"
                    name="password"
                    type="password"
                    placeholder="Enter your password"
                    value={credentials.password}
                    onChange={(event) => setCredentials(prev => ({ ...prev, password: event.target.value }))}
                    disabled={loading}
                    required
                    autoComplete="current-password"
                  />
                </div>
                
                {(localError || error) && (
                  <div className="auth-error-compact">
                    {localError || error}
                  </div>
                )}
                
                <button type="submit" className="auth-button" disabled={loading}>
                  {loading ? (
                    <>
                      <div className="spinner"></div>
                      Signing in…
                    </>
                  ) : (
                    'Sign In'
                  )}
                </button>
              </form>
              
              <div className="auth-hint">
                <small>Default credentials: admin / admin123</small>
              </div>
            </div>
          )}

          {/* Token Login */}
          {loginMethod === 'token' && (
            <div className="auth-form">
              <form onSubmit={handleTokenSubmit}>
                <div className="form-field">
                  <label htmlFor="token">Bearer Token</label>
                  <textarea
                    id="token"
                    name="token"
                    placeholder="Paste your JWT token here"
                    value={token}
                    onChange={(event) => setToken(event.target.value)}
                    disabled={loading}
                    required
                    rows={4}
                    className="token-textarea"
                  />
                </div>
                
                {(localError || error) && (
                  <div className="auth-error-compact">
                    {localError || error}
                  </div>
                )}
                
                <button type="submit" className="auth-button" disabled={loading}>
                  {loading ? (
                    <>
                      <div className="spinner"></div>
                      Authenticating…
                    </>
                  ) : (
                    'Authenticate'
                  )}
                </button>
              </form>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
