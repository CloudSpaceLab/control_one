import { FormEvent, useMemo, useState } from 'react';
import { AlertTriangle, KeyRound } from 'lucide-react';
import { useSecretGroups, useSecretSyncs } from '../hooks/useSecrets';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { CreateSecretGroupPayload } from '../lib/api';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import {
  DataTable,
  EmptyState,
  KpiTile,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import type { ColumnDef } from '@tanstack/react-table';

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

function syncStatusTone(status: string): StateTone {
  const s = status.toLowerCase();
  if (s === 'success' || s === 'succeeded' || s === 'ok') return 'healthy';
  if (s === 'failed' || s === 'error') return 'critical';
  if (s === 'pending' || s === 'queued') return 'warning';
  if (s === 'running' || s === 'syncing') return 'info';
  return 'unknown';
}

interface GroupRow {
  id: string;
  name: string;
  endpoint?: string;
  backend: string;
  sync_status: string;
  sync_error?: string;
  last_sync_at?: string;
}

interface SyncRow {
  id: string;
  secret_path: string;
  secret_version?: string | number;
  sync_status: string;
  synced_at?: string;
}

export function Secrets(): JSX.Element {
  const api = useApiClient();
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);
  const [selectedTenant] = useState<string | undefined>(undefined);
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [isSyncing, setIsSyncing] = useState(false);

  useTenants();
  const {
    data: groups,
    loading: groupsLoading,
    error: groupsError,
    pagination,
    reload: reloadGroups,
  } = useSecretGroups({
    tenant_id: selectedTenant,
    limit,
    offset,
  });

  const {
    data: syncs,
    loading: syncsLoading,
    error: syncsError,
    reload: reloadSyncs,
  } = useSecretSyncs(selectedGroupId, { limit: 20, offset: 0 });

  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [saving, setSaving] = useState(false);

  const [formData, setFormData] = useState<CreateSecretGroupPayload>({
    name: '',
    backend: 'vault',
    endpoint: '',
    sync_interval_seconds: 900,
  });

  const handleCreate = () => {
    setIsCreating(true);
    setFormData({
      name: '',
      backend: 'vault',
      endpoint: '',
      sync_interval_seconds: 900,
    });
    resetFeedback();
  };

  const handleCancel = () => {
    setIsCreating(false);
    setFormData({
      name: '',
      backend: 'vault',
      endpoint: '',
      sync_interval_seconds: 900,
    });
    resetFeedback();
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!formData.name.trim()) {
      showError('Name is required');
      return;
    }

    setSaving(true);
    resetFeedback();

    try {
      const payload: CreateSecretGroupPayload = {
        ...formData,
        tenant_id: selectedTenant || undefined,
      };
      await api.createSecretGroup(payload);
      showSuccess('Secret group created successfully');
      setIsCreating(false);
      reloadGroups();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to create secret group';
      showError(message);
      showToast(message, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (groupId: string) => {
    if (!confirm('Are you sure you want to delete this secret group?')) {
      return;
    }

    try {
      await api.deleteSecretGroup(groupId);
      showToast('Secret group deleted successfully', 'success');
      reloadGroups();
      if (selectedGroupId === groupId) {
        setSelectedGroupId(null);
      }
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to delete secret group';
      showToast(message, 'error');
    }
  };

  const handleSync = async (groupId: string) => {
    setIsSyncing(true);
    try {
      await api.syncSecretGroup(groupId);
      showToast('Secret sync triggered successfully', 'success');
      reloadGroups();
      if (selectedGroupId === groupId) {
        reloadSyncs();
      }
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to sync secrets';
      showToast(message, 'error');
    } finally {
      setIsSyncing(false);
    }
  };

  const selectedGroup = groups.find((g) => g.id === selectedGroupId) || null;

  const groupColumns = useMemo<ColumnDef<GroupRow>[]>(() => [
    {
      id: 'name',
      header: 'Name',
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="font-medium">{row.original.name}</span>
          {row.original.endpoint && (
            <span className="font-mono text-[0.65rem] text-text-muted">{row.original.endpoint}</span>
          )}
        </div>
      ),
    },
    {
      accessorKey: 'backend',
      header: 'Backend',
      cell: ({ getValue }) => <StatusTag tone="info">{getValue() as string}</StatusTag>,
    },
    {
      id: 'sync_status',
      header: 'Sync Status',
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          <StatusTag tone={syncStatusTone(row.original.sync_status)}>{row.original.sync_status}</StatusTag>
          {row.original.sync_error && (
            <span title={row.original.sync_error} className="text-state-warning">
              <AlertTriangle className="h-3.5 w-3.5" />
            </span>
          )}
        </div>
      ),
    },
    {
      accessorKey: 'last_sync_at',
      header: 'Last Sync',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums">{formatDate(getValue() as string)}</span>
      ),
    },
    {
      id: 'actions',
      header: 'Actions',
      cell: ({ row }) => (
        <div className="flex gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={(e) => {
              e.stopPropagation();
              handleSync(row.original.id);
            }}
            disabled={isSyncing}
          >
            Sync
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="text-state-critical hover:text-state-critical"
            onClick={(e) => {
              e.stopPropagation();
              handleDelete(row.original.id);
            }}
          >
            Delete
          </Button>
        </div>
      ),
    },
  ], [isSyncing]);

  const syncColumns = useMemo<ColumnDef<SyncRow>[]>(() => [
    {
      accessorKey: 'secret_path',
      header: 'Secret Path',
      cell: ({ getValue }) => <span className="font-mono text-xs">{getValue() as string}</span>,
    },
    {
      accessorKey: 'secret_version',
      header: 'Version',
      cell: ({ getValue }) => <span className="font-mono text-xs">{(getValue() as string) || '—'}</span>,
    },
    {
      accessorKey: 'sync_status',
      header: 'Status',
      cell: ({ getValue }) => (
        <StatusTag tone={syncStatusTone(getValue() as string)}>{getValue() as string}</StatusTag>
      ),
    },
    {
      accessorKey: 'synced_at',
      header: 'Synced At',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums">{formatDate(getValue() as string)}</span>
      ),
    },
  ], []);

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE · SECRETS"
        title="Secrets vault"
        description="Encrypted credentials. Tracked rotations. Audit-ready access logs."
        actions={
          <Button variant="primary" size="md" onClick={handleCreate}>
            Create Secret Group
          </Button>
        }
      />

      {groupsError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load secret groups">
          <p className="text-sm text-state-critical">{groupsError}</p>
        </Panel>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="TOTAL GROUPS" value={pagination.total} tone="brand" />
        <KpiTile
          label="SYNCED"
          value={groups.filter((g) => g.sync_status === 'success').length}
          tone="healthy"
        />
        <KpiTile
          label="FAILED"
          value={groups.filter((g) => g.sync_status === 'failed').length}
          tone="critical"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[2fr,1fr]">
        <Panel padding="sm" tone="inset" eyebrow={`SECRET GROUPS · ${groups.length} of ${pagination.total}`} title="Groups">
          <DataTable
            columns={groupColumns}
            rows={groups as unknown as GroupRow[]}
            rowKey={(r) => r.id}
            loading={groupsLoading}
            compact
            onRowClick={(r) => setSelectedGroupId(r.id)}
            empty={
              <EmptyState icon={<KeyRound />} title="No secret groups found" />
            }
          />
          <div className="flex items-center justify-between gap-2 border-t border-border-subtle p-3">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setOffset(Math.max(0, offset - limit))}
              disabled={offset === 0 || groupsLoading}
            >
              Previous
            </Button>
            <span className="font-mono text-xs text-text-muted">
              Page {Math.floor(offset / limit) + 1} of {Math.ceil(pagination.total / limit) || 1}
            </span>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setOffset(offset + limit)}
              disabled={offset + limit >= pagination.total || groupsLoading}
            >
              Next
            </Button>
          </div>
        </Panel>

        {selectedGroup && (
          <Panel
            padding="md"
            eyebrow="SYNC HISTORY"
            title={selectedGroup.name}
            actions={
              <Button variant="ghost" size="sm" onClick={() => setSelectedGroupId(null)}>
                Close
              </Button>
            }
          >
            <dl className="grid grid-cols-1 gap-2 text-sm">
              <div className="flex justify-between gap-2">
                <dt className="text-text-muted">Backend</dt>
                <dd>{selectedGroup.backend}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-text-muted">Endpoint</dt>
                <dd className="font-mono text-xs">{selectedGroup.endpoint || '—'}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-text-muted">Sync Status</dt>
                <dd>
                  <StatusTag tone={syncStatusTone(selectedGroup.sync_status)}>{selectedGroup.sync_status}</StatusTag>
                </dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-text-muted">Last Sync</dt>
                <dd className="font-mono text-xs">{formatDate(selectedGroup.last_sync_at)}</dd>
              </div>
            </dl>

            {syncsError ? (
              <Panel padding="sm" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load sync history">
                <p className="text-sm text-state-critical">{syncsError}</p>
              </Panel>
            ) : (
              <DataTable
                columns={syncColumns}
                rows={syncs as unknown as SyncRow[]}
                rowKey={(r) => r.id}
                loading={syncsLoading}
                compact
                empty={<EmptyState title="No sync history available" />}
              />
            )}
          </Panel>
        )}
      </div>

      {isCreating && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
          onClick={handleCancel}
        >
          <div
            className="w-full max-w-lg rounded-lg border border-border-subtle bg-elevated shadow-[var(--shadow-panel)]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-border-subtle p-4">
              <h2 className="font-display text-base font-semibold">Create Secret Group</h2>
              <Button variant="ghost" size="sm" onClick={handleCancel}>×</Button>
            </div>

            <form onSubmit={handleSubmit}>
              <div className="flex flex-col gap-3 p-4">
                {formError && <p className="text-xs text-state-critical">{formError}</p>}
                {formSuccess && <p className="text-xs text-state-healthy">{formSuccess}</p>}

                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="name">Name *</Label>
                  <Input
                    id="name"
                    type="text"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    required
                  />
                </div>

                <SelectField
                  id="backend"
                  label="Backend *"
                  value={formData.backend}
                  onChange={(e) => setFormData({ ...formData, backend: e.target.value })}
                  required
                >
                  <option value="vault">Vault</option>
                </SelectField>

                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="endpoint">Endpoint</Label>
                  <Input
                    id="endpoint"
                    type="text"
                    value={formData.endpoint}
                    onChange={(e) => setFormData({ ...formData, endpoint: e.target.value })}
                    placeholder="secret/data/app"
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="sync_interval">Sync Interval (seconds)</Label>
                  <Input
                    id="sync_interval"
                    type="number"
                    value={formData.sync_interval_seconds}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        sync_interval_seconds: parseInt(e.target.value, 10) || 900,
                      })
                    }
                    min={60}
                  />
                </div>
              </div>

              <div className="flex items-center justify-end gap-2 border-t border-border-subtle p-4">
                <Button variant="secondary" size="md" type="button" onClick={handleCancel} disabled={saving}>
                  Cancel
                </Button>
                <Button variant="primary" size="md" type="submit" disabled={saving}>
                  {saving ? 'Creating...' : 'Create'}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
