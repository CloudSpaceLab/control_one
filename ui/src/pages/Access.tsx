import { useCallback, useEffect, useState } from 'react';
import { Check, KeyRound, X } from 'lucide-react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Tabs, TabsList, TabsTrigger } from '../components/ui/tabs';
import {
  DataTable,
  EmptyState,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import type { AccessRequest, CreateAccessRequestPayload } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

type Tab = 'pending' | 'all';

const STATUS_TONE: Record<string, StateTone> = {
  pending: 'warning',
  approved: 'healthy',
  denied: 'critical',
  revoked: 'unknown',
  expired: 'unknown',
};

export function Access(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState('');
  const [tab, setTab] = useState<Tab>('pending');
  const [items, setItems] = useState<AccessRequest[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

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

  const approve = async (id: string) => {
    await client.approveAccessRequest(id, '');
    refresh();
  };
  const deny = async (id: string) => {
    await client.denyAccessRequest(id, '');
    refresh();
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
      accessorKey: 'justification',
      header: 'Justification',
      cell: ({ getValue }) => (
        <span className="text-sm text-text-secondary">{(getValue() as string) || '—'}</span>
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
      accessorKey: 'requested_at',
      header: 'Requested',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs">{new Date(getValue() as string).toLocaleString()}</span>
      ),
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
      cell: ({ row }) =>
        row.original.status === 'pending' ? (
          <div className="flex items-center gap-1.5">
            <Button variant="primary" size="sm" onClick={() => approve(row.original.id)}>
              <Check className="h-3.5 w-3.5" /> Approve
            </Button>
            <Button variant="ghost" size="sm" onClick={() => deny(row.original.id)}>
              <X className="h-3.5 w-3.5" /> Deny
            </Button>
          </div>
        ) : null,
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="ACCESS · JUST-IN-TIME"
        title="Privileged access requests"
        description="Request, approve, auto-revoke. No standing admin credentials."
        actions={
          <select
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
            aria-label="Tenant"
            className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
          >
            {tenants.map((t) => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </select>
        }
      />

      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Load failed">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      )}

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList>
          <TabsTrigger value="pending">Pending</TabsTrigger>
          <TabsTrigger value="all">All</TabsTrigger>
        </TabsList>
      </Tabs>

      <Panel padding="md" eyebrow="REQUEST ACCESS" title="New just-in-time grant" toneAccent="brand">
        <RequestForm tenantId={tenantId} onCreated={refresh} />
      </Panel>

      <Panel padding="sm" tone="inset" eyebrow={`REQUESTS · ${items.length}`} title="Queue">
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
              description="Approved sessions show up here. Request access via the form above."
            />
          }
        />
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
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setForm((f) => ({ ...f, tenant_id: tenantId }));
  }, [tenantId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    try {
      await client.createAccessRequest(form);
      setForm({ ...form, justification: '' });
      onCreated();
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={submit} className="grid grid-cols-1 gap-3 sm:grid-cols-5 sm:items-end">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ar-type">Type</Label>
        <select
          id="ar-type"
          value={form.target_resource_type}
          onChange={(e) =>
            setForm({ ...form, target_resource_type: e.target.value as 'ssh' | 'rdp' | 'db' })
          }
          className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong"
        >
          <option value="ssh">ssh</option>
          <option value="rdp">rdp</option>
          <option value="db">db</option>
        </select>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ar-access">Access</Label>
        <Input
          id="ar-access"
          required
          value={form.requested_access}
          onChange={(e) => setForm({ ...form, requested_access: e.target.value })}
        />
      </div>
      <div className="flex flex-col gap-1.5 sm:col-span-2">
        <Label htmlFor="ar-justification">Justification</Label>
        <Input
          id="ar-justification"
          value={form.justification ?? ''}
          onChange={(e) => setForm({ ...form, justification: e.target.value })}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ar-ttl">TTL (s)</Label>
        <Input
          id="ar-ttl"
          type="number"
          min={60}
          value={form.ttl_seconds ?? 1800}
          onChange={(e) => setForm({ ...form, ttl_seconds: Number(e.target.value) })}
        />
      </div>
      <Button
        type="submit"
        variant="primary"
        size="md"
        disabled={submitting || !tenantId}
        className="sm:col-span-5"
      >
        {submitting ? 'Submitting…' : 'Request access'}
      </Button>
    </form>
  );
}
