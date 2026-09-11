import { useCallback, useEffect, useMemo, useState } from 'react';
import { Trash2 } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import {
  DataTable,
  EmptyState,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { ConfirmModal } from '../components/ConfirmModal';
import type {
  ColumnClassification,
  CreateDLPRulePayload,
  DataClassificationRule,
  PIIFinding,
} from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

// ---- helper tone maps -------------------------------------------------------

function severityTone(severity: string | undefined): StateTone {
  switch ((severity ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
      return 'info';
    default:
      return 'unknown';
  }
}

function encryptionTone(kind: string | undefined): StateTone {
  switch (kind) {
    case 'aes256_likely':
    case 'bcrypt_likely':
    case 'sha256_likely':
      return 'healthy';
    case 'plaintext':
      return 'critical';
    default:
      return 'unknown';
  }
}

// ---- FindingsTab ------------------------------------------------------------

interface FindingsTabProps {
  tenantId: string;
}

type PendingFindingResolve = {
  finding: PIIFinding;
  error?: string;
};

function FindingsTab({ tenantId }: FindingsTabProps): JSX.Element {
  const client = useApiClient();
  const [findings, setFindings] = useState<PIIFinding[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [pendingResolve, setPendingResolve] = useState<PendingFindingResolve | null>(null);
  const [resolving, setResolving] = useState(false);

  const load = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await client.listPIIFindings({ tenantId, limit: 100, offset: 0 });
      setFindings(res.data ?? []);
    } catch (err) {
      setFindings([]);
      setError(err instanceof Error ? err.message : 'Failed to load findings');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId]);

  useEffect(() => {
    load();
  }, [load]);

  const handleResolve = async () => {
    if (!pendingResolve || resolving) return;
    setResolving(true);
    try {
      await client.resolvePIIFinding(pendingResolve.finding.id);
      setPendingResolve(null);
      await load();
    } catch (err) {
      setPendingResolve({
        finding: pendingResolve.finding,
        error: `PII finding resolve failed: ${err instanceof Error ? err.message : 'Resolve failed'}`,
      });
    } finally {
      setResolving(false);
    }
  };

  const columns = useMemo<ColumnDef<PIIFinding>[]>(() => [
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)}>
          {row.original.severity}
        </StatusTag>
      ),
    },
    {
      accessorKey: 'details',
      header: 'Details',
      cell: ({ row }) => row.original.details ?? '-',
    },
    {
      accessorKey: 'created_at',
      header: 'Detected',
      cell: ({ row }) => new Date(row.original.created_at).toLocaleString(),
    },
    {
      accessorKey: 'resolved_at',
      header: 'Resolved',
      cell: ({ row }) =>
        row.original.resolved_at
          ? new Date(row.original.resolved_at).toLocaleString()
          : '-',
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) =>
        !row.original.resolved_at ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setPendingResolve({ finding: row.original })}
            aria-label={`Resolve PII finding ${row.original.details ?? row.original.id}`}
          >
            Resolve
          </Button>
        ) : null,
    },
  ], []);

  return (
    <>
      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="PII findings unavailable">
          <p role="alert">{error}</p>
        </Panel>
      )}
      {!error && (
        <Panel padding="md">
          <DataTable
            columns={columns}
            rows={findings}
            rowKey={(r) => r.id}
            loading={loading}
            empty={
              <EmptyState
                title="No findings"
                description="No PII findings have been recorded yet."
              />
            }
          />
        </Panel>
      )}

      <ConfirmModal
        open={pendingResolve !== null}
        title="Resolve finding"
        body={`Mark ${pendingResolve?.finding.details ?? 'this PII finding'} as resolved? This cannot be undone.`}
        confirmLabel={resolving ? 'Resolving...' : 'Resolve'}
        confirmDisabled={resolving}
        cancelDisabled={resolving}
        onConfirm={handleResolve}
        onCancel={() => setPendingResolve(null)}
      >
        {pendingResolve?.error ? (
          <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {pendingResolve.error}
          </div>
        ) : null}
      </ConfirmModal>
    </>
  );
}

// ---- ColumnsTab -------------------------------------------------------------

interface ColumnsTabProps {
  tenantId: string;
}

function ColumnsTab({ tenantId }: ColumnsTabProps): JSX.Element {
  const client = useApiClient();
  const [cols, setCols] = useState<ColumnClassification[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await client.listColumnClassifications({ tenantId, limit: 100, offset: 0 });
      setCols(res.data ?? []);
    } catch (err) {
      setCols([]);
      setError(err instanceof Error ? err.message : 'Failed to load columns');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId]);

  useEffect(() => {
    load();
  }, [load]);

  const columns = useMemo<ColumnDef<ColumnClassification>[]>(() => [
    { accessorKey: 'database_name', header: 'Database' },
    { accessorKey: 'table_name', header: 'Table' },
    { accessorKey: 'column_name', header: 'Column' },
    {
      accessorKey: 'pii_type',
      header: 'PII type',
      cell: ({ row }) =>
        row.original.pii_type ? (
          <StatusTag tone="degraded">{row.original.pii_type}</StatusTag>
        ) : (
          <span>-</span>
        ),
    },
    {
      accessorKey: 'encryption_kind',
      header: 'Encryption',
      cell: ({ row }) => {
        const kind = row.original.encryption_kind;
        return kind ? (
          <StatusTag tone={encryptionTone(kind)}>{kind}</StatusTag>
        ) : (
          <StatusTag tone="unknown">unknown</StatusTag>
        );
      },
    },
    {
      accessorKey: 'last_scanned_at',
      header: 'Last scanned',
      cell: ({ row }) =>
        row.original.last_scanned_at
          ? new Date(row.original.last_scanned_at).toLocaleString()
          : '-',
    },
  ], []);

  return (
    <>
      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Column scans unavailable">
          <p role="alert">{error}</p>
        </Panel>
      )}
      {!error && (
        <Panel padding="md">
          <DataTable
            columns={columns}
            rows={cols}
            rowKey={(r) => r.id}
            loading={loading}
            empty={
              <EmptyState
                title="No column scans"
                description="No columns have been scanned for PII yet."
              />
            }
          />
        </Panel>
      )}
    </>
  );
}

// ---- RulesTab ---------------------------------------------------------------

interface RulesTabProps {
  tenantId: string;
}

type PendingRuleDelete = {
  rule: DataClassificationRule;
  error?: string;
};

function RulesTab({ tenantId }: RulesTabProps): JSX.Element {
  const client = useApiClient();
  const [rules, setRules] = useState<DataClassificationRule[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<PendingRuleDelete | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [seeding, setSeeding] = useState(false);
  const [creating, setCreating] = useState(false);

  // New rule form state
  const [newName, setNewName] = useState('');
  const [newPIIType, setNewPIIType] = useState('');
  const [newRegex, setNewRegex] = useState('');
  const [newSeverity, setNewSeverity] = useState('medium');
  const [formError, setFormError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    setLoadError(null);
    try {
      const res = await client.listDLPRules(tenantId);
      setRules(res.data ?? []);
    } catch (err) {
      setRules([]);
      setLoadError(err instanceof Error ? err.message : 'Failed to load rules');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId]);

  useEffect(() => {
    load();
  }, [load]);

  const handleSeedDefaults = async () => {
    if (seeding) return;
    setSeeding(true);
    setActionError(null);
    try {
      const { seeded } = await client.seedDLPRules(tenantId);
      setNotice(`Seeded ${seeded} default rule${seeded === 1 ? '' : 's'}.`);
      window.setTimeout(() => setNotice(null), 4000);
      await load();
    } catch (err) {
      setActionError(`Seed default DLP rules failed: ${err instanceof Error ? err.message : 'Seed failed'}`);
    } finally {
      setSeeding(false);
    }
  };

  const handleCreateRule = async () => {
    setFormError(null);
    if (!newName.trim() || !newPIIType.trim() || !newRegex.trim()) {
      setFormError('Name, PII type, and regex are required.');
      return;
    }
    const payload: CreateDLPRulePayload = {
      tenant_id: tenantId,
      name: newName.trim(),
      pii_type: newPIIType.trim(),
      regex: newRegex.trim(),
      severity: newSeverity,
    };
    setCreating(true);
    try {
      await client.createDLPRule(payload);
      setNewName('');
      setNewPIIType('');
      setNewRegex('');
      setNewSeverity('medium');
      setActionError(null);
      await load();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Create failed');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async () => {
    if (!pendingDelete || deleting) return;
    setDeleting(true);
    try {
      await client.deleteDLPRule(pendingDelete.rule.id);
      setPendingDelete(null);
      await load();
    } catch (err) {
      setPendingDelete({
        rule: pendingDelete.rule,
        error: `DLP rule delete failed: ${err instanceof Error ? err.message : 'Delete failed'}`,
      });
    } finally {
      setDeleting(false);
    }
  };

  const columns = useMemo<ColumnDef<DataClassificationRule>[]>(() => [
    { accessorKey: 'name', header: 'Name' },
    { accessorKey: 'pii_type', header: 'PII type' },
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)}>
          {row.original.severity}
        </StatusTag>
      ),
    },
    {
      accessorKey: 'enabled',
      header: 'Enabled',
      cell: ({ row }) => (row.original.enabled ? 'Yes' : 'No'),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          size="sm"
          variant="outline"
          onClick={() => setPendingDelete({ rule: row.original })}
          aria-label={`Delete classification rule ${row.original.name}`}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      ),
    },
  ], []);

  return (
    <>
      {loadError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Classification rules unavailable">
          <p role="alert">{loadError}</p>
        </Panel>
      )}
      {actionError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="DLP rule action failed">
          <p role="alert">{actionError}</p>
        </Panel>
      )}
      {notice && (
        <Panel padding="md" tone="inset" eyebrow="NOTICE" title={notice} />
      )}

      <Panel padding="md" eyebrow="ACTIONS" title="Default rules">
        <Button variant="outline" onClick={handleSeedDefaults} disabled={seeding}>
          {seeding ? 'Seeding...' : 'Seed default rules'}
        </Button>
      </Panel>

      <Panel padding="md" eyebrow="NEW RULE" title="Add classification rule">
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <div className="flex flex-col gap-1">
              <Label htmlFor="dlp-name">Name</Label>
              <Input
                id="dlp-name"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="e.g. Email address"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="dlp-pii-type">PII type</Label>
              <Input
                id="dlp-pii-type"
                value={newPIIType}
                onChange={(e) => setNewPIIType(e.target.value)}
                placeholder="e.g. email"
              />
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="dlp-regex">Regex pattern</Label>
            <Input
              id="dlp-regex"
              value={newRegex}
              onChange={(e) => setNewRegex(e.target.value)}
              placeholder={String.raw`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`}
              className="font-mono text-sm"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="dlp-severity">Severity</Label>
            <select
              id="dlp-severity"
              value={newSeverity}
              onChange={(e) => setNewSeverity(e.target.value)}
              className="h-9 rounded-md border border-input bg-transparent px-3 py-1 text-sm"
            >
              <option value="low">Low</option>
              <option value="medium">Medium</option>
              <option value="high">High</option>
              <option value="critical">Critical</option>
            </select>
          </div>
          {formError && <p role="alert" className="text-sm text-destructive">{formError}</p>}
          <Button onClick={handleCreateRule} disabled={creating}>
            {creating ? 'Adding...' : 'Add rule'}
          </Button>
        </div>
      </Panel>

      {!loadError && (
        <Panel padding="md">
          <DataTable
            columns={columns}
            rows={rules}
            rowKey={(r) => r.id}
            loading={loading}
            empty={
              <EmptyState
                title="No classification rules"
                description='No rules yet. Click "Seed default rules" to add built-in PII patterns.'
              />
            }
          />
        </Panel>
      )}

      <ConfirmModal
        open={pendingDelete !== null}
        title="Delete rule"
        body={`Permanently delete ${pendingDelete?.rule.name ?? 'this classification rule'}?`}
        confirmLabel={deleting ? 'Deleting...' : 'Delete'}
        confirmDisabled={deleting}
        cancelDisabled={deleting}
        variant="danger"
        onConfirm={handleDelete}
        onCancel={() => setPendingDelete(null)}
      >
        {pendingDelete?.error ? (
          <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {pendingDelete.error}
          </div>
        ) : null}
      </ConfirmModal>
    </>
  );
}

// ---- Main page --------------------------------------------------------------

export function DataSecurity(): JSX.Element {
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState<string>('');

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  return (
    <div className="flex flex-col gap-6">
      <SectionHeader
        eyebrow="DETECT & RESPOND / DATA SECURITY"
        title="Data security"
        description="Classify columns, detect PII exposure, and manage DLP rules."
      />

      <Panel padding="md" eyebrow="TENANT" title="Select tenant">
        <div className="flex flex-col gap-1">
          <Label htmlFor="ds-tenant">Tenant</Label>
          <select
            id="ds-tenant"
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
            className="h-9 rounded-md border border-input bg-transparent px-3 py-1 text-sm"
          >
            <option value="">- select -</option>
            {tenants.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </div>
      </Panel>

      {tenantId && (
        <Tabs defaultValue="findings">
          <TabsList>
            <TabsTrigger value="findings">Findings</TabsTrigger>
            <TabsTrigger value="columns">Columns</TabsTrigger>
            <TabsTrigger value="rules">Rules</TabsTrigger>
          </TabsList>

          <TabsContent value="findings">
            <FindingsTab tenantId={tenantId} />
          </TabsContent>
          <TabsContent value="columns">
            <ColumnsTab tenantId={tenantId} />
          </TabsContent>
          <TabsContent value="rules">
            <RulesTab tenantId={tenantId} />
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}
