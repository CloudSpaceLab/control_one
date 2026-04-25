import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { Badge, stateToVariant } from '../components/Badge';
import { EmptyState } from '../components/EmptyState';
import { SessionReplay } from '../components/SessionReplay';
import type { SessionRecording } from '../lib/api';

export function Sessions(): JSX.Element {
  const client = useApiClient();
  const [sessions, setSessions] = useState<SessionRecording[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [openSession, setOpenSession] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await client.listSessions({ limit: 50, offset: 0 });
      setSessions(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Privileged sessions</p>
          <h2>Recorded SSH &amp; RDP sessions</h2>
          <p className="subtitle">Replay any privileged session to verify what happened. Search commands, scrub
            timeline, export transcript for incident review.</p>
        </div>
        <button type="button" className="secondary-button" onClick={refresh} disabled={loading}>
          {loading ? 'Loading…' : 'Refresh'}
        </button>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      {openSession ? (
        <SessionReplay sessionId={openSession} onClose={() => setOpenSession(null)} />
      ) : sessions.length === 0 && !loading ? (
        <EmptyState
          title="No recorded sessions yet"
          description="Sessions appear here when an operator connects through the Control One bastion. Wire up access requests in /access to start capturing replays."
        />
      ) : (
        <table className="data-table" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th>Started</th>
              <th>Type</th>
              <th>Node</th>
              <th>User</th>
              <th>Duration</th>
              <th>State</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((s) => (
              <tr key={s.id}>
                <td>{new Date(s.started_at).toLocaleString()}</td>
                <td>{s.session_type}</td>
                <td><code>{s.node_id.slice(0, 8)}</code></td>
                <td>{s.user_id ? <code>{s.user_id.slice(0, 8)}</code> : '—'}</td>
                <td>{s.duration_seconds ? `${Math.round(s.duration_seconds)}s` : 'live'}</td>
                <td><Badge variant={stateToVariant(s.status)} size="sm">{s.status}</Badge></td>
                <td>
                  <button
                    type="button"
                    className="primary-button"
                    onClick={() => setOpenSession(s.id)}
                    disabled={!s.artifact_path}
                    title={s.artifact_path ? 'Replay session' : 'No artifact stored'}
                  >
                    Replay
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
