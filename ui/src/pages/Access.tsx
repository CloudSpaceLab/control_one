import { useCallback, useEffect, useState } from 'react';
import { Check, KeyRound, Plus, ShieldCheck, Trash2, X } from 'lucide-react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { ConfirmModal } from '../components/ConfirmModal';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../components/ui/tabs';
import {
  DataTable,
  EmptyState,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import type { AccessRequest, CreateAccessRequestPayload, CommandACL } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

type Tab = 'pending' | 'all' | 'command-policy';

const STATUS_TONE: Record<string, StateTone> = {
  pending: 'warning',
  approved: 'healthy',
  denied: 'critical',
  revoked: 'unknown',
  expired: 'unknown',
};

const TTL_PRESETS = [
  { label: '30 min', seconds: 1800 },
  { label: '1 hr', seconds: 3600 },
  { label: '4 hr', seconds: 14400 },
  { label: '8 hr', seconds: 28800 },
];

function fmtTTL(s: number): string {
  if (s < 3600) return `${Math.round(s / 60)}m`;
  if (s % 3600 === 0) return `${s / 3600}h`;
  return `${Math.floor(s / 3600)}h ${Math.round((s % 3600) / 60)}m`;
}

// Inline decision panel shown below the row when an operator clicks Approve/Deny
interface DecisionPanelProps {
  request: AccessRequest;
  intent: 'approve' | 'deny';
  onConfirm: (reason: string) => Promise<void>;
  onCancel: () => void;
}

function DecisionPanel({ request, intent, onConfirm, onCancel }: DecisionPanelProps) {
  const [reason, setReason] = useState('');
  const [loading, setLoading] = useState(false);
  const isApprove = intent === 'approve';

  const handleConfirm = async () => {
    setLoading(true);
    try {
      await onConfirm(reason);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="rounded-lg border border-border-subtle bg-elevated p-4 shadow-md">
      <p className="mb-3 text-sm text-foreground">
        <span className="font-semibold">{isApprove ? 'Approve' : 'Deny'}</span>{' '}
        <code className="rounded bg-surface-2 px-1 py-0.5 font-mono text-[0.7rem]">
          {request.requested_access}
        </code>{' '}
        {request.target_resource_type} access
        {request.justification ? (
          <span className="text-text-muted"> — &ldquo;{request.justification}&rdquo;</span>
        ) : null}
      </p>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="decision-reason">
          Reason <span className="text-text-muted">(optional)</span>
        </Label>
        <Input
          id="decision-reason"
          autoFocus
          placeholder={isApprove ? 'e.g. confirmed with team lead' : 'e.g. no active incident'}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handleConfirm()}
        />
      </div>
      <div className="mt-3 flex gap-2">
        <Button
          variant={isApprove ? 'primary' : 'danger'}
          size="sm"
          disabled={loading}
          onClick={handleConfirm}
        >
          {loading ? 'Saving…' : isApprove ? 'Confirm approve' : 'Confirm deny'}
        </Button>
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

export function Access(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState('');
  const [tab, setTab] = useState<Tab>('pending');
  const [items, setItems] = useState<AccessRequest[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [deciding, setDeciding] = useState<{ id: string; intent: 'approve' | 'deny' } | null>(null);

  // Command ACL state
  const [acls, setAcls] = useState<CommandACL[]>([]);
  const [aclsLoading, setAclsLoading] = useState(false);
  const [aclsReloadToken, setAclsReloadToken] = useState(0);
  const [deleteAclId, setDeleteAclId] = useState<string | null>(null);
  const [showCreateAcl, setShowCreateAcl] = useState(false);
  const [aclName, setAclName] = useState('');
  const [aclRole, setAclRole] = useState('operator');
  const [aclPattern, setAclPattern] = useState('');
  const [aclAction, setAclAction] = useState<'allow' | 'deny'>('deny');
  const [creatingAcl, setCreatingAcl] = useState(false);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    try {
      const status = tab === 'pending' ? 'pending' : undefined;
      const resp = await client.listAccessRequests({ tenantId, status, limit: 100, offset: 0 });
      setItems(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId, tab]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleDecision = async (id: string, intent: 'approve' | 'deny', reason: string) => {
    if (intent === 'approve') {
      await client.approveAccessRequest(id, reason);
    } else {
      await client.denyAccessRequest(id, reason);
    }
    setDeciding(null);
    refresh();
  };

  // Command ACL effects + handlers
  useEffect(() => {
    let cancelled = false;
    if (!tenantId) {
      setAcls([]);
      setAclsLoading(false);
      return () => {
        cancelled = true;
      };
    }
    setAclsLoading(true);
    client
      .listCommandACLs({ tenantId })
      .then((r) => { if (!cancelled) setAcls(r.data ?? []); })
      .catch(() => { if (!cancelled) setAcls([]); })
      .finally(() => { if (!cancelled) setAclsLoading(false); });
    return () => { cancelled = true; };
  }, [client, tenantId, aclsReloadToken]);

  const handleCreateAcl = async () => {
    if (!aclPattern.trim() || !aclName.trim() || !tenantId) return;
    setCreatingAcl(true);
    try {
      await client.createCommandACL({
        tenant_id: tenantId,
        name: aclName.trim(),
        pattern: aclPattern.trim(),
        action: aclAction,
        roles: [aclRole.trim() || 'operator'],
      });
      setAclName('');
      setAclRole('operator');
      setAclPattern('');
      setShowCreateAcl(false);
      setAclsReloadToken((n) => n + 1);
    } finally {
      setCreatingAcl(false);
    }
  };

  const handleDeleteAcl = async () => {
    if (!deleteAclId) return;
    await client.deleteCommandACL(deleteAclId);
    setDeleteAclId(null);
    setAclsReloadToken((n) => n + 1);
  };

  const columns: ColumnDef<AccessRequest>[] = [
    {
      accessorKey: 'target_resource_type',
      header: 'Type',
      cell: ({ getValue }) => (
        <StatusTag tone="info">{(getValue() as string).toUpperCase()}</StatusTag>
      ),
    },
    {
      accessorKey: 'requested_access',
      header: 'Access',
      cell: ({ getValue }) => (
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem]">
          {getValue() as string}
        </code>
      ),
    },
    {
      accessorKey: 'target_node_id',
      header: 'Node',
      cell: ({ getValue }) => {
        const v = getValue() as string | undefined;
        return v ? (
          <code className="rounded bg-surface-2 px-1 py-0.5 font-mono text-[0.65rem] text-text-secondary">
            {v.slice(0, 8)}…
          </code>
        ) : (
          <span className="text-text-muted">—</span>
        );
      },
    },
    {
      accessorKey: 'justification',
      header: 'Justification',
      cell: ({ getValue }) => (
        <span className="max-w-[200px] truncate text-sm text-text-secondary">
          {(getValue() as string) || '—'}
        </span>
      ),
    },
    {
      accessorKey: 'status',
      header: 'Status',
      cell: ({ getValue }) => {
        const s = getValue() as string;
        return <StatusTag tone={STATUS_TONE[s] ?? 'unknown'}>{s}</StatusTag>;
      },
    },
    {
      id: 'ttl',
      header: 'TTL',
      cell: ({ row }) => (
        <span className="font-mono text-xs text-text-muted">{fmtTTL(row.original.ttl_seconds)}</span>
      ),
    },
    {
      accessorKey: 'requested_at',
      header: 'Requested',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs">{new Date(getValue() as string).toLocaleString()}</span>
      ),
    },
    {
      id: 'decision',
      header: 'Decision',
      cell: ({ row }) => {
        const r = row.original;
        if (!r.decided_at) return <span className="text-text-muted">—</span>;
        return (
          <div className="flex flex-col gap-0.5">
            <span className="font-mono text-[0.65rem] text-text-muted">
              {new Date(r.decided_at).toLocaleString()}
            </span>
            {r.decided_by && (
              <span className="text-xs text-text-secondary">{r.decided_by}</span>
            )}
            {r.decision_reason && (
              <span className="max-w-[160px] truncate text-xs italic text-text-muted">
                &ldquo;{r.decision_reason}&rdquo;
              </span>
            )}
          </div>
        );
      },
    },
    {
      accessorKey: 'expires_at',
      header: 'Expires',
      cell: ({ getValue }) => {
        const v = getValue() as string | undefined;
        return (
          <span className="font-mono text-xs">{v ? new Date(v).toLocaleString() : '—'}</span>
        );
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        if (row.original.status !== 'pending') return null;
        const isDeciding = deciding?.id === row.original.id;
        return (
          <div className="flex items-center gap-1.5">
            <Button
              variant="primary"
              size="sm"
              onClick={() =>
                setDeciding(isDeciding && deciding?.intent === 'approve' ? null : { id: row.original.id, intent: 'approve' })
              }
            >
              <Check className="h-3.5 w-3.5" /> Approve
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() =>
                setDeciding(isDeciding && deciding?.intent === 'deny' ? null : { id: row.original.id, intent: 'deny' })
              }
            >
              <X className="h-3.5 w-3.5" /> Deny
            </Button>
          </div>
        );
      },
    },
  ];

  const aclColumns: ColumnDef<CommandACL>[] = [
    {
      header: 'Name',
      accessorKey: 'name',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
    },
    {
      header: 'Role',
      accessorKey: 'roles',
      cell: ({ row }) => (
        <span className="font-mono text-xs text-text-secondary">
          {row.original.roles?.[0] ?? row.original.role ?? 'operator'}
        </span>
      ),
    },
    {
      header: 'Pattern',
      accessorKey: 'pattern',
      cell: ({ row }) => (
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-text-secondary">
          {row.original.pattern}
        </code>
      ),
    },
    {
      header: 'Action',
      accessorKey: 'action',
      cell: ({ row }) => (
        <StatusTag tone={row.original.action === 'allow' ? 'healthy' : 'critical'} className="uppercase">
          {row.original.action}
        </StatusTag>
      ),
    },
    {
      header: 'Created',
      accessorKey: 'created_at',
      cell: ({ row }) => (
        <span className="font-mono text-xs text-text-muted">
          {new Date(row.original.created_at).toLocaleDateString()}
        </span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button variant="ghost" size="sm" onClick={() => setDeleteAclId(row.original.id)}>
          <Trash2 className="h-3.5 w-3.5 text-state-critical" />
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="ACCESS · JUST-IN-TIME"
        title="Privileged access requests"
        description="Request time-limited access to SSH, RDP, or DB resources. Approvers review requests here — sessions auto-revoke when the TTL expires. No standing admin credentials."
        actions={
          <SelectField
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
            aria-label="Tenant"
          >
            {tenants.map((t) => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </SelectField>
        }
      />

      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Load failed">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      )}

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList>
          <TabsTrigger value="pending">
            Pending{items.filter((i) => i.status === 'pending').length > 0
              ? ` (${items.filter((i) => i.status === 'pending').length})`
              : ''}
          </TabsTrigger>
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="command-policy">Command policy</TabsTrigger>
        </TabsList>

        <TabsContent value="pending" className="mt-4 flex flex-col gap-4">
          <Panel padding="md" eyebrow="REQUEST ACCESS" title="New just-in-time grant" toneAccent="brand">
            <RequestForm tenantId={tenantId} onCreated={refresh} />
          </Panel>
          <Panel padding="sm" tone="inset" eyebrow={`REQUESTS · ${items.length}`} title="Queue">
            {deciding && (() => {
              const req = items.find((i) => i.id === deciding.id);
              return req ? (
                <div className="border-b border-border-subtle p-4">
                  <DecisionPanel
                    request={req}
                    intent={deciding.intent}
                    onConfirm={(reason) => handleDecision(deciding.id, deciding.intent, reason)}
                    onCancel={() => setDeciding(null)}
                  />
                </div>
              ) : null;
            })()}
            <DataTable
              columns={columns}
              rows={items.filter((i) => i.status === 'pending')}
              rowKey={(r) => r.id}
              loading={loading}
              compact
              empty={
                <EmptyState
                  icon={<KeyRound />}
                  title="No pending requests"
                  description="No pending access requests. Approved sessions appear here once someone requests access."
                />
              }
            />
          </Panel>
        </TabsContent>

        <TabsContent value="all" className="mt-4 flex flex-col gap-4">
          <Panel padding="sm" tone="inset" eyebrow={`REQUESTS · ${items.length}`} title="All requests">
            {deciding && (() => {
              const req = items.find((i) => i.id === deciding.id);
              return req ? (
                <div className="border-b border-border-subtle p-4">
                  <DecisionPanel
                    request={req}
                    intent={deciding.intent}
                    onConfirm={(reason) => handleDecision(deciding.id, deciding.intent, reason)}
                    onCancel={() => setDeciding(null)}
                  />
                </div>
              ) : null;
            })()}
            <DataTable
              columns={columns}
              rows={items}
              rowKey={(r) => r.id}
              loading={loading}
              compact
              empty={
                <EmptyState
                  icon={<KeyRound />}
                  title="No requests"
                  description="No access requests found for this tenant."
                />
              }
            />
          </Panel>
        </TabsContent>

        <TabsContent value="command-policy" className="mt-4">
          <Panel
            padding="md"
            eyebrow="COMMAND ACL"
            title="Command policy rules"
            actions={
              <Button variant="primary" size="sm" onClick={() => setShowCreateAcl(true)}>
                <Plus className="h-3.5 w-3.5" /> New rule
              </Button>
            }
          >
            {showCreateAcl && (
              <div className="mb-4 rounded-md border border-border-subtle bg-elevated p-4">
                <p className="mb-3 text-sm font-medium text-foreground">New command ACL</p>
                <div className="flex flex-wrap items-end gap-3">
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="acl-name">Name</Label>
                    <Input
                      id="acl-name"
                      value={aclName}
                      onChange={(e) => setAclName(e.target.value)}
                      placeholder="Block rm -rf"
                      className="h-8 w-44"
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="acl-role">Role</Label>
                    <select
                      id="acl-role"
                      className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                      value={aclRole}
                      onChange={(e) => setAclRole(e.target.value)}
                    >
                      <option value="operator">Operator</option>
                      <option value="admin">Admin</option>
                      <option value="investigator">Investigator</option>
                    </select>
                  </div>
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="acl-pattern">Regex pattern</Label>
                    <Input
                      id="acl-pattern"
                      value={aclPattern}
                      onChange={(e) => setAclPattern(e.target.value)}
                      placeholder="^rm\s+-rf"
                      className="h-8 w-44 font-mono text-xs"
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="acl-action">Action</Label>
                    <select
                      id="acl-action"
                      className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                      value={aclAction}
                      onChange={(e) => setAclAction(e.target.value as 'allow' | 'deny')}
                    >
                      <option value="deny">Deny</option>
                      <option value="allow">Allow</option>
                    </select>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={handleCreateAcl}
                      disabled={creatingAcl || !aclName.trim() || !aclPattern.trim() || !tenantId}
                    >
                      {creatingAcl ? 'Creating…' : 'Create'}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => setShowCreateAcl(false)}>
                      Cancel
                    </Button>
                  </div>
                </div>
              </div>
            )}
            <DataTable
              columns={aclColumns}
              rows={acls}
              rowKey={(r) => r.id}
              loading={aclsLoading}
              empty={
                <EmptyState
                  icon={<ShieldCheck />}
                  title="No command policy rules"
                  description="Create allow/deny rules to control which shell commands agents may execute."
                />
              }
            />
          </Panel>
        </TabsContent>
      </Tabs>

      <ConfirmModal
        open={deleteAclId !== null}
        title="Delete command ACL rule?"
        body="This policy will stop being enforced immediately."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={handleDeleteAcl}
        onCancel={() => setDeleteAclId(null)}
      />

      <Panel padding="md" eyebrow="HOW IT WORKS" title="JIT access workflow">
        <ol className="flex flex-col gap-2 text-sm text-text-secondary">
          <li className="flex items-start gap-2">
            <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-500/15 text-xs font-bold text-brand-400">1</span>
            <span><strong className="text-foreground">Request</strong> — Fill in the form above. Choose the resource type (SSH, RDP, DB), specify the exact access needed (e.g. <code className="rounded bg-surface-2 px-1 font-mono text-[0.7rem]">root@prod-db-01</code>), and pick a TTL.</span>
          </li>
          <li className="flex items-start gap-2">
            <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-500/15 text-xs font-bold text-brand-400">2</span>
            <span><strong className="text-foreground">Approve</strong> — An operator clicks Approve and optionally adds a reason. The request status changes to <StatusTag tone="healthy" className="inline-flex">approved</StatusTag>.</span>
          </li>
          <li className="flex items-start gap-2">
            <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-500/15 text-xs font-bold text-brand-400">3</span>
            <span><strong className="text-foreground">Auto-revoke</strong> — When the TTL expires the session moves to <StatusTag tone="unknown" className="inline-flex">expired</StatusTag> and the credential is invalidated. No manual cleanup needed.</span>
          </li>
        </ol>
      </Panel>
    </div>
  );
}

function RequestForm({
  tenantId,
  onCreated,
}: {
  tenantId: string;
  onCreated: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [form, setForm] = useState<CreateAccessRequestPayload>({
    tenant_id: tenantId,
    target_resource_type: 'ssh',
    requested_access: 'root',
    justification: '',
    ttl_seconds: 1800,
  });
  const [customTTL, setCustomTTL] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    setForm((f) => ({ ...f, tenant_id: tenantId }));
  }, [tenantId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    try {
      await client.createAccessRequest(form);
      setForm((f) => ({ ...f, justification: '' }));
      setSuccess(true);
      setTimeout(() => setSuccess(false), 3000);
      onCreated();
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <SelectField
          id="ar-type"
          label="Resource type"
          value={form.target_resource_type}
          onChange={(e) =>
            setForm({ ...form, target_resource_type: e.target.value as 'ssh' | 'rdp' | 'db' })
          }
        >
          <option value="ssh">SSH — shell access</option>
          <option value="rdp">RDP — desktop access</option>
          <option value="db">Database — query access</option>
        </SelectField>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="ar-access">Requested access</Label>
          <Input
            id="ar-access"
            required
            placeholder="e.g. root@prod-db-01"
            value={form.requested_access}
            onChange={(e) => setForm({ ...form, requested_access: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="ar-justification">Justification</Label>
          <Input
            id="ar-justification"
            placeholder="Why do you need this access?"
            value={form.justification ?? ''}
            onChange={(e) => setForm({ ...form, justification: e.target.value })}
          />
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label>Duration</Label>
        <div className="flex flex-wrap items-center gap-2">
          {TTL_PRESETS.map((p) => (
            <button
              key={p.seconds}
              type="button"
              onClick={() => { setCustomTTL(false); setForm({ ...form, ttl_seconds: p.seconds }); }}
              className={
                !customTTL && form.ttl_seconds === p.seconds
                  ? 'rounded-md border border-brand-500 bg-brand-500/15 px-3 py-1.5 text-sm font-medium text-brand-400'
                  : 'rounded-md border border-border-subtle bg-surface px-3 py-1.5 text-sm text-foreground hover:border-border-strong hover:bg-hover'
              }
            >
              {p.label}
            </button>
          ))}
          <button
            type="button"
            onClick={() => setCustomTTL(true)}
            className={
              customTTL
                ? 'rounded-md border border-brand-500 bg-brand-500/15 px-3 py-1.5 text-sm font-medium text-brand-400'
                : 'rounded-md border border-border-subtle bg-surface px-3 py-1.5 text-sm text-foreground hover:border-border-strong hover:bg-hover'
            }
          >
            Custom
          </button>
          {customTTL && (
            <div className="flex items-center gap-1.5">
              <Input
                type="number"
                min={60}
                className="w-24"
                value={form.ttl_seconds ?? 1800}
                onChange={(e) => setForm({ ...form, ttl_seconds: Number(e.target.value) })}
              />
              <span className="text-sm text-text-muted">seconds</span>
            </div>
          )}
          <span className="text-xs text-text-muted">
            Access expires after {fmtTTL(form.ttl_seconds ?? 1800)}
          </span>
        </div>
      </div>

      <div className="flex items-center gap-3">
        <Button
          type="submit"
          variant="primary"
          disabled={submitting || !tenantId}
        >
          {submitting ? 'Submitting…' : 'Request access'}
        </Button>
        {success && (
          <span className="flex items-center gap-1 text-sm text-state-healthy">
            <Check className="h-4 w-4" /> Request submitted
          </span>
        )}
      </div>
    </form>
  );
}
