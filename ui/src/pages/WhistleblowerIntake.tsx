// WhistleblowerIntake — public, unauthenticated misconduct intake form.
//
// This page mirrors the TrustCenter pattern: the route is mounted above
// MainLayout's auth guard in App.tsx, so the user never needs a session
// token. The control-plane handlers enforce per-IP and global rate limits
// plus a SHA-256 proof-of-work challenge — both are client-visible
// concerns (PoW is solved here in the browser, the limit kicks in as a
// 429 response).
//
// Privacy stance: we collect ONLY operator-supplied fields (description,
// approximate_date, subject_role). We never read window.navigator or
// IP/UA from the client side. The server stores the body sealed with
// AES-256-GCM and a bcrypt hash of the one-time token — there is no
// "recover token" path and the user is told that explicitly on success.
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  fetchMisconductChallenge,
  submitWhistleblowerReport,
  type MisconductSubmitResponse,
} from '../lib/api';

interface FormState {
  description: string;
  approximate_date: string;
  subject_role: string;
}

const ROLES = ['employee', 'manager', 'contractor', 'vendor', 'other'] as const;

// solvePoW finds a nonce s.t. SHA-256(challenge||nonce) starts with `bits`
// leading zero bits. We chunk the work and yield to the event loop so the
// page stays responsive while the PoW runs (typically ~1–3 seconds at
// 20 bits on a modern CPU). Uses Web Crypto SubtleCrypto for parity with
// the server's crypto/sha256.
async function solvePoW(challenge: string, bits: number, onProgress?: (n: number) => void): Promise<string> {
  const enc = new TextEncoder();
  // SubtleCrypto.digest is async — we batch N hashes per yield to amortize
  // the promise overhead. Empirically 1k iterations between yields keeps
  // the UI smooth even on weak devices.
  const batch = 1000;
  let nonce = 0;
  // small helper: count leading zero bits in a Uint8Array
  function leadingZeroBits(buf: Uint8Array, target: number): boolean {
    const fullBytes = Math.floor(target / 8);
    const remBits = target % 8;
    for (let i = 0; i < fullBytes; i++) if (buf[i] !== 0) return false;
    if (remBits > 0) {
      const mask = 0xff << (8 - remBits);
      if ((buf[fullBytes] & mask) !== 0) return false;
    }
    return true;
  }

  while (true) {
    for (let i = 0; i < batch; i++) {
      const candidate = nonce.toString(16);
      const hash = await crypto.subtle.digest('SHA-256', enc.encode(challenge + candidate));
      if (leadingZeroBits(new Uint8Array(hash), bits)) {
        return candidate;
      }
      nonce++;
    }
    onProgress?.(nonce);
    // Yield so the browser can paint.
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
  }
}

export function WhistleblowerIntake(): JSX.Element {
  const [form, setForm] = useState<FormState>({
    description: '',
    approximate_date: '',
    subject_role: 'employee',
  });
  const [challenge, setChallenge] = useState<{ challenge: string; difficulty: number } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [progress, setProgress] = useState(0);
  const [result, setResult] = useState<MisconductSubmitResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    fetchMisconductChallenge()
      .then((c) => {
        if (!cancelled) setChallenge(c);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'failed to load challenge');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!challenge) return;
    setError(null);
    setSubmitting(true);
    setProgress(0);
    try {
      const nonce = await solvePoW(challenge.challenge, challenge.difficulty, (n) => setProgress(n));
      const resp = await submitWhistleblowerReport({
        description: form.description,
        approximate_date: form.approximate_date,
        subject_role: form.subject_role,
        challenge: challenge.challenge,
        nonce,
      });
      setResult(resp);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'submission failed');
    } finally {
      setSubmitting(false);
    }
  }

  if (result) {
    return (
      <div className="mx-auto max-w-xl p-6">
        <h1 className="font-display text-2xl font-semibold">Report submitted</h1>
        <p className="mt-2 text-sm text-text-secondary">
          Save this token now. It is the only way to check the status of your report.
          We do not store it in plaintext and cannot recover it for you.
        </p>
        <div className="mt-4 rounded-md border border-border-subtle bg-surface-2 p-4">
          <code className="break-all font-mono text-sm">{result.token}</code>
          <button
            type="button"
            className="mt-3 rounded-md bg-brand-500 px-3 py-1.5 text-sm font-medium text-[#0f172a]"
            onClick={() => {
              void navigator.clipboard.writeText(result.token).then(() => setCopied(true));
            }}
          >
            {copied ? 'Copied' : 'Copy token'}
          </button>
        </div>
        <p className="mt-3 text-xs text-text-secondary">{result.message}</p>
        <div className="mt-6 flex gap-3 text-sm">
          <Link to="/intake-status" className="text-brand-400 underline">
            Check status with your token
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-xl p-6">
      <h1 className="font-display text-2xl font-semibold">Report misconduct anonymously</h1>
      <p className="mt-2 text-sm text-text-secondary">
        This intake is anonymous. We do not record your IP address, browser, or any
        identifying metadata. Investigators see only the description you write below.
        After submitting you will receive a one-time token to check on progress.
      </p>
      {error && (
        <div className="mt-4 rounded-md border border-state-critical/40 bg-state-critical/10 p-3 text-sm text-state-critical">
          {error}
        </div>
      )}
      <form className="mt-6 flex flex-col gap-4" onSubmit={handleSubmit}>
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium">Description</span>
          <textarea
            required
            minLength={20}
            maxLength={5000}
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.currentTarget.value })}
            className="min-h-[180px] rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm"
            placeholder="What happened? Please include enough context for an investigator to act on the report."
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium">Approximate date</span>
          <input
            type="date"
            value={form.approximate_date}
            onChange={(e) => setForm({ ...form, approximate_date: e.currentTarget.value })}
            className="rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium">Subject role</span>
          <select
            value={form.subject_role}
            onChange={(e) => setForm({ ...form, subject_role: e.currentTarget.value })}
            className="rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm"
          >
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </label>
        <button
          type="submit"
          disabled={submitting || !challenge}
          className="mt-2 inline-flex items-center justify-center rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-[#0f172a] disabled:opacity-50"
        >
          {submitting ? `Solving challenge… ${progress.toLocaleString()} attempts` : 'Submit report'}
        </button>
      </form>
    </div>
  );
}
