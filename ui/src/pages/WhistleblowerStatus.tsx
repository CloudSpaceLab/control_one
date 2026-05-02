// WhistleblowerStatus — public, unauthenticated. Submitter pastes their
// one-time token; we show the badge only. The server never returns the
// body, never returns a created_at, never returns whether the token is
// even valid (an invalid token returns `unknown`, the same shape as a
// real-but-not-yet-reviewed submission, so a malicious caller cannot
// enumerate tokens by status).
import { useState } from 'react';
import { fetchIntakeStatus, type MisconductStatusResponse } from '../lib/api';

const TONE: Record<MisconductStatusResponse['status'], string> = {
  received: 'bg-state-info/15 text-state-info',
  under_review: 'bg-state-warning/15 text-state-warning',
  closed: 'bg-state-healthy/15 text-state-healthy',
  unknown: 'bg-surface-2 text-text-secondary',
};

const LABEL: Record<MisconductStatusResponse['status'], string> = {
  received: 'Received',
  under_review: 'Under review',
  closed: 'Closed',
  unknown: 'Unknown — token not recognised',
};

export function WhistleblowerStatus(): JSX.Element {
  const [token, setToken] = useState('');
  const [status, setStatus] = useState<MisconductStatusResponse['status'] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleCheck(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const r = await fetchIntakeStatus(token.trim());
      setStatus(r.status);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'lookup failed');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="mx-auto max-w-xl p-6">
      <h1 className="font-display text-2xl font-semibold">Check report status</h1>
      <p className="mt-2 text-sm text-text-secondary">
        Paste the one-time token you received when you submitted a report. We will
        return only the current status. We cannot show you the body of the report.
      </p>
      <form className="mt-6 flex flex-col gap-4" onSubmit={handleCheck}>
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium">Token</span>
          <input
            type="text"
            value={token}
            onChange={(e) => setToken(e.currentTarget.value)}
            required
            className="rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-sm"
          />
        </label>
        <button
          type="submit"
          disabled={loading || !token.trim()}
          className="inline-flex items-center justify-center rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-[#0f172a] disabled:opacity-50"
        >
          {loading ? 'Checking…' : 'Check status'}
        </button>
      </form>
      {error && (
        <div className="mt-4 rounded-md border border-state-critical/40 bg-state-critical/10 p-3 text-sm text-state-critical">
          {error}
        </div>
      )}
      {status && (
        <div className="mt-6 flex flex-col items-start gap-2">
          <span className={`inline-flex rounded-md px-3 py-1 text-sm font-medium ${TONE[status]}`}>
            {LABEL[status]}
          </span>
        </div>
      )}
    </div>
  );
}
