import { FormEvent, useState } from 'react';
import { useAuth } from '../providers/AuthProvider';

export function Login(): JSX.Element {
  const { signIn } = useAuth();
  const [token, setToken] = useState('');
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    try {
      const trimmed = token.trim();
      if (!trimmed) {
        throw new Error('Token is required');
      }
      signIn(trimmed);
      setError(null);
    } catch (err) {
      if (err instanceof Error) {
        setError(err.message);
      } else {
        setError('Unable to sign in');
      }
    }
  };

  return (
    <section className="login-card">
      <h2>Sign in</h2>
      <p>Authenticate with your organization-issued OIDC token to access the control plane.</p>
      <form onSubmit={handleSubmit}>
        <label htmlFor="token">Bearer token</label>
        <input
          id="token"
          name="token"
          type="text"
          placeholder="Paste JWT here"
          value={token}
          onChange={(event) => setToken(event.target.value)}
          required
        />
        {error ? <span className="form-error">{error}</span> : null}
        <button type="submit">Continue</button>
      </form>
    </section>
  );
}
