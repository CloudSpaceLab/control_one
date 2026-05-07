import { useCallback, useEffect, useState } from 'react';
import { Play, RefreshCw, Terminal } from 'lucide-react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { SessionReplay } from '../components/SessionReplay';
import { Button } from '../components/ui/button';
import {
  DataTable,
  EmptyState,
  EntityChip,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import type { SessionRecording } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

const STATUS_TONE: Record<string, StateTone> = {
  active: 'info',
  completed: 'healthy',
  failed: 'critical',
  terminated: 'warning',
};

export function Sessions(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [sessions, setSessions] = useState<SessionRecording[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [openSession, setOpenSession] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    try {
      const resp = await client.listSessions({
        tenantId: currentTenantId,
        limit: 50,
        offset: 0,
      });
      setSessions(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const columns: ColumnDef<SessionRecording>[] = [
    {
      accessorKey: 'started_at',
      header: 'Started',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs">{new Date(getValue() as string).toLocaleString()}</span>
      ),
    },
    {
      accessorKey: 'session_type',
      header: 'Type',
      cell: ({ getValue }) => (
        <StatusTag tone="info">{(getValue() as string).toUpperCase()}</StatusTag>
      ),
    },
    {
      accessorKey: 'node_id',
      header: 'Node',
      cell: ({ getValue }) => <EntityChip type="host" value={getValue() as string} />,
    },
    {
      accessorKey: 'user_id',
      header: 'User',
      cell: ({ getValue }) => {
        const v = getValue() as string | undefined;
        return v ? <EntityChip type="user" value={v} /> : <span className="text-text-muted">—</span>;
      },
    },
    {
      accessorKey: 'duration_seconds',
      header: 'Duration',
      cell: ({ getValue }) => {
        const d = getValue() as number | undefined;
        return (
          <span className="font-mono text-xs tabular-nums">
            {d ? `${Math.round(d)}s` : 'live'}
          </span>
        );
      },
    },
    {
      accessorKey: 'status',
      header: 'State',
      cell: ({ getValue }) => {
        const s = getValue() as string;
        return <StatusTag tone={STATUS_TONE[s] ?? 'unknown'}>{s}</StatusTag>;
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          variant="primary"
          size="sm"
          onClick={() => setOpenSession(row.original.id)}
          disabled={!row.original.artifact_path}
          title={row.original.artifact_path ? 'Replay session' : 'No artifact stored'}
        >
          <Play className="h-3.5 w-3.5" /> Replay
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="ACCESS · PRIVILEGED SESSIONS"
        title="Recorded SSH & RDP sessions"
        description="Replay any privileged session to verify what happened. Search commands, scrub timeline, export transcript."
        actions={
          <Button variant="secondary" size="md" onClick={refresh} disabled={loading}>
            <RefreshCw className="h-4 w-4" /> {loading ? 'Loading…' : 'Refresh'}
          </Button>
        }
      />

      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Load failed">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      )}

      {openSession ? (
        <Panel padding="sm" eyebrow="REPLAY" title="Session playback">
          <SessionReplay sessionId={openSession} onClose={() => setOpenSession(null)} />
        </Panel>
      ) : (
        <Panel padding="sm" tone="inset" eyebrow={`SESSIONS · ${sessions.length}`} title="Recordings">
          <DataTable
            columns={columns}
            rows={sessions}
            rowKey={(r) => r.id}
            loading={loading}
            compact
            empty={
              <EmptyState
                icon={<Terminal />}
                title="No recorded sessions yet"
                description="Sessions appear here when an operator connects through the bastion. Wire up requests in /access to start capturing replays."
              />
            }
          />
        </Panel>
      )}
    </div>
  );
}
