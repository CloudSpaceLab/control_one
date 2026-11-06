import { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { clearOidcSession, exchangeCodeForToken } from '../lib/oidc';
import { useAuth } from '../providers/AuthProvider';
import { Button } from '../components/ui/button';

export function AuthCallback(): JSX.Element {
  const location = useLocation();
  const navigate = useNavigate();
  const { signIn } = useAuth();
  const [processing, setProcessing] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const exchange = useRef<{ search: string; promise: ReturnType<typeof exchangeCodeForToken> } | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const code = params.get('code');
    const state = params.get('state');

    if (params.has('error')) {
      clearOidcSession();
      setProcessing(false);
      setError('Organization sign-in was cancelled or refused. Start a new sign-in or contact your administrator.');
      return;
    }

    if (!code || !state) {
      clearOidcSession();
      setProcessing(false);
      setError('Missing authorization parameters');
      return;
    }
    if (params.getAll('code').length !== 1 || params.getAll('state').length !== 1) {
      clearOidcSession();
      setProcessing(false);
      setError('Invalid authorization parameters. Start a new sign-in.');
      return;
    }

    // StrictMode replays effects; the authorization transaction is single-use.
    // Reuse only this mounted callback's exchange, never across page reloads.
    if (!exchange.current || exchange.current.search !== location.search) {
      exchange.current = { search: location.search, promise: exchangeCodeForToken(code, state) };
    }
    let active = true;
    exchange.current.promise
      .then(async ({ token, returnTo }) => {
        if (!active) return;
        await signIn(token);
        if (!active) return;
        const safeLocalPath = typeof returnTo === 'string' && returnTo.startsWith('/') && !returnTo.startsWith('//') &&
          !Array.from(returnTo).some(character => character === '\\' || character.charCodeAt(0) <= 32);
        const destination = safeLocalPath ? returnTo : '/';
        navigate(destination, { replace: true });
      })
      .catch((err: Error) => {
        if (!active) return;
        setError(err.message);
        setProcessing(false);
      });
    return () => { active = false; };
  }, [location.search, navigate, signIn]);

  return (
    <main className="flex min-h-screen items-center justify-center bg-surface px-4 py-10 text-foreground">
      <section aria-labelledby="sso-callback-title" className="w-full max-w-lg space-y-5 rounded-2xl border border-border-subtle bg-elevated p-6 shadow-lg sm:p-8">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-widest text-text-secondary">Control One · Organization sign-in</p>
          <h2 id="sso-callback-title" className="text-2xl font-semibold tracking-tight">{processing ? 'Completing sign-in' : 'Sign-in could not be completed'}</h2>
        </div>
        {processing ? (
          <p role="status" className="text-sm text-text-secondary">Confirming your organization sign-in. Please wait…</p>
        ) : (
          <>
            {error ? <p role="alert" className="rounded-xl border border-border-subtle bg-surface p-4 text-sm leading-relaxed text-foreground">{error}</p> : null}
            <p className="text-sm text-text-secondary">Return to sign-in to start a fresh attempt.</p>
            <Button type="button" className="w-full" onClick={() => navigate('/login', { replace: true })}>
              Return to sign-in
            </Button>
          </>
        )}
      </section>
    </main>
  );
}
