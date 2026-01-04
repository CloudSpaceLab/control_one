import { useEffect, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { exchangeCodeForToken } from '../lib/oidc';
import { useAuth } from '../providers/AuthProvider';

export function AuthCallback(): JSX.Element {
  const location = useLocation();
  const navigate = useNavigate();
  const { signIn, isAuthenticated } = useAuth();
  const [processing, setProcessing] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const code = params.get('code');
    const state = params.get('state');

    if (!code || !state) {
      setProcessing(false);
      setError('Missing authorization parameters');
      return;
    }

    exchangeCodeForToken(code, state)
      .then(async ({ token, returnTo }) => {
        await signIn(token);
        const destination = returnTo && returnTo.startsWith('/') ? returnTo : '/';
        navigate(destination, { replace: true });
      })
      .catch((err: Error) => {
        setError(err.message);
        setProcessing(false);
      });
  }, [location.search, navigate, signIn]);

  if (isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  return (
    <section className="login-card">
      <h2>Completing sign-in</h2>
      {processing ? (
        <p>Exchanging authorization code. Please wait…</p>
      ) : (
        <>
          <p>We were unable to complete your sign-in.</p>
          {error ? <span className="form-error">{error}</span> : null}
          <button type="button" onClick={() => navigate('/login', { replace: true })}>
            Return to sign-in
          </button>
        </>
      )}
    </section>
  );
}
