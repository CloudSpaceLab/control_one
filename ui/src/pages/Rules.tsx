import { useCallback, useEffect, useState, lazy, Suspense } from 'react';
import { Plus, Trash2, Library } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { Skeleton } from '../components/ui/skeleton';
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
import { useEventStream } from '../hooks/useEventStream';
import { ConfirmModal } from '../components/ConfirmModal';
import { ApplyPackModal } from '../components/ApplyPackModal';
import { RULE_PACK_CATALOG, type RulePack, type Category } from '../lib/rulePacks';
import { cn } from '@/lib/utils';
import type {
  CreateLogRulePayload,
  CreatePortRulePayload,
  LogRule,
  PortRule,
} from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';
import { Recommendations } from './Recommendations';

// Lazy-load the visual builder so its drag/drop state machine doesn't slow
// the initial page render for operators who only want to author rules in the
// flat form.
const RuleBuilder = lazy(() => import('./RuleBuilder').then((m) => ({ default: m.RuleBuilder })));

type Tab = 'port' | 'log' | 'builder' | 'templates' | 'drafts';
const RULES_POLL_MS = 30_000;

function severityTone(severity: string): StateTone {
  const s = severity.toLowerCase();
  if (s === 'critical') return 'critical';
  if (s === 'high') return 'warning';
  if (s === 'medium') return 'info';
  return 'unknown';
}

export function Rules(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tab, setTab] = useState<Tab>('port');
  const [tenantId, setTenantId] = useState<string>('');
  const [portRules, setPortRules] = useState<PortRule[]>([]);
  const [logRules, setLogRules] = useState<LogRule[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [packSearch, setPackSearch] = useState('');
  const [packCategory, setPackCategory] = useState<Category | null>(null);
  const [selectedPack, setSelectedPack] = useState<RulePack | null>(null);

  const CATEGORIES = Array.from(new Set(RULE_PACK_CATALOG.map((p) => p.category)));
  const filteredPacks = RULE_PACK_CATALOG.filter((p) => {
    if (packCategory && p.category !== packCategory) return false;
    if (packSearch) {
      const q = packSearch.toLowerCase();
      return (
        p.name.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q) ||
        p.tags.some((t) => t.includes(q))
      );
    }
    return true;
  });

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    try {
      const [p, l] = await Promise.all([
        client.listPortRules({ tenantId, limit: 100, offset: 0 }),
        client.listLogRules({ tenantId, limit: 100, offset: 0 }),
      ]);
      setPortRules(p.data);
      setLogRules(l.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    }
  }, [client, tenantId]);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, RULES_POLL_MS);
    return () => window.clearInterval(timer);
  }, [refresh]);

  useEventStream(tenantId, ['policy.updated', 'rule.triggered'], (ev) => {
    setNotice(`Realtime: ${ev.topic}`);
    refresh();
    window.setTimeout(() => setNotice(null), 3000);
  });

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE · DETECTION"
        title="Detection rules"
        description="Define what's allowed. Detect violations instantly. Real-time enforcement on every node."
        actions={
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rules-tenant" className="sr-only">
              Tenant
            </Label>
            <select
              id="rules-tenant"
              value={tenantId}
              onChange={(e) => setTenantId(e.target.value)}
              aria-label="Tenant"
              className="flex h-9 rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
            >
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
        }
      />

      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      )}
      {notice && (
        <Panel padding="sm" tone="inset" toneAccent="brand" eyebrow="REALTIME" title={notice}>
          <span className="sr-only">Realtime update received.</span>
        </Panel>
      )}

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList>
          <TabsTrigger value="port">Port rules ({portRules.length})</TabsTrigger>
          <TabsTrigger value="log">Log rules ({logRules.length})</TabsTrigger>
          <TabsTrigger value="builder" title="Compose rules visually with drag-and-drop blocks">
            Visual builder
          </TabsTrigger>
          <TabsTrigger value="templates">
            <Library className="h-3.5 w-3.5" />
            Templates
          </TabsTrigger>
          <TabsTrigger value="drafts">
            AI Drafts
          </TabsTrigger>
        </TabsList>

        <TabsContent value="port" className="mt-4">
          <PortRulesPane tenantId={tenantId} rules={portRules} onRefresh={refresh} />
        </TabsContent>
        <TabsContent value="log" className="mt-4">
          <LogRulesPane tenantId={tenantId} rules={logRules} onRefresh={refresh} />
        </TabsContent>
        <TabsContent value="builder" className="mt-4">
          <Suspense fallback={<Skeleton className="h-64 w-full" />}>
            <RuleBuilder />
          </Suspense>
        </TabsContent>

        <TabsContent value="templates" className="mt-4">
          <div className="flex flex-col gap-5">
            {/* Header */}
            <div className="flex flex-col gap-1">
              <h3 className="font-display text-base font-semibold text-foreground">
                Monitoring rule packs
              </h3>
              <p className="text-sm text-text-secondary">
                One-click rule bundles for common server applications. Applies port monitoring + log
                pattern rules directly to your tenant.
              </p>
            </div>

            {/* Search + category filter */}
            <div className="flex flex-col gap-3">
              <Input
                placeholder="Search packs… (nginx, postgres, redis, sshd…)"
                value={packSearch}
                onChange={(e) => setPackSearch(e.target.value)}
              />
              <div className="flex flex-wrap gap-2">
                {(['All', ...CATEGORIES] as const).map((cat) => (
                  <button
                    key={cat}
                    type="button"
                    onClick={() => setPackCategory(cat === 'All' ? null : (cat as Category))}
                    className={cn(
                      'rounded-full px-3 py-1 text-xs font-medium transition-colors',
                      (cat === 'All' ? packCategory === null : packCategory === cat)
                        ? 'bg-brand-500 text-[#0f172a]'
                        : 'bg-surface border border-border-subtle text-text-secondary hover:border-border-strong hover:text-foreground',
                    )}
                  >
                    {cat}
                  </button>
                ))}
              </div>
            </div>

            {/* Pack grid */}
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {filteredPacks.map((pack) => (
                <Panel
                  key={pack.id}
                  padding="md"
                  eyebrow={pack.category}
                  title={pack.name}
                  actions={
                    <Button variant="primary" size="sm" onClick={() => setSelectedPack(pack)}>
                      Apply
                    </Button>
                  }
                >
                  <p className="text-sm text-text-secondary line-clamp-2">{pack.description}</p>
                  <div className="flex flex-wrap gap-1">
                    {pack.tags.map((tag) => (
                      <StatusTag key={tag} tone="info">
                        <span className="font-mono text-[0.65rem]">{tag}</span>
                      </StatusTag>
                    ))}
                  </div>
                  <p className="text-xs text-text-muted">
                    {pack.portRules.length} port rule{pack.portRules.length !== 1 ? 's' : ''} ·{' '}
                    {pack.logRules.length} log rule{pack.logRules.length !== 1 ? 's' : ''}
                  </p>
                </Panel>
              ))}
            </div>

            {filteredPacks.length === 0 && (
              <EmptyState
                title="No packs match"
                description="Try a different search term or category."
              />
            )}
          </div>
        </TabsContent>
        <TabsContent value="drafts" className="mt-4">
          <Recommendations />
        </TabsContent>
      </Tabs>

      <ApplyPackModal
        pack={selectedPack}
        onClose={() => setSelectedPack(null)}
        tenants={tenants}
        defaultTenantId={tenantId}
        onApplied={() => {
          setSelectedPack(null);
          refresh();
        }}
      />
    </div>
  );
}

function PortRulesPane({
  tenantId,
  rules,
  onRefresh,
}: {
  tenantId: string;
  rules: PortRule[];
  onRefresh: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [form, setForm] = useState<CreatePortRulePayload>({
    tenant_id: tenantId,
    name: '',
    port: 22,
    protocol: 'tcp',
    expected_state: 'closed',
    severity: 'medium',
    action: 'notify',
    enabled: true,
  });
  const [submitting, setSubmitting] = useState(false);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  useEffect(() => {
    setForm((f) => ({ ...f, tenant_id: tenantId }));
  }, [tenantId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    try {
      await client.createPortRule(form);
      setForm({ ...form, name: '' });
      onRefresh();
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (id: string) => {
    await client.deletePortRule(id);
    onRefresh();
  };

  const columns: ColumnDef<PortRule>[] = [
    { accessorKey: 'name', header: 'Name' },
    { accessorKey: 'port', header: 'Port' },
    { accessorKey: 'protocol', header: 'Proto' },
    { accessorKey: 'expected_state', header: 'Expected' },
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ getValue }) => {
        const s = String(getValue());
        return <StatusTag tone={severityTone(s)}>{s}</StatusTag>;
      },
    },
    {
      accessorKey: 'enabled',
      header: 'Enabled',
      cell: ({ getValue }) => (
        <StatusTag tone={getValue() ? 'healthy' : 'unknown'}>{getValue() ? 'yes' : 'no'}</StatusTag>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setConfirmDeleteId(row.original.id)}
        >
          <Trash2 className="h-4 w-4" />
          Delete
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <ConfirmModal
        open={confirmDeleteId !== null}
        title="Delete port rule?"
        body="This cannot be undone."
        variant="danger"
        confirmLabel="Delete"
        onConfirm={() => {
          if (confirmDeleteId) remove(confirmDeleteId);
          setConfirmDeleteId(null);
        }}
        onCancel={() => setConfirmDeleteId(null)}
      />

      <Panel padding="md" eyebrow="NEW RULE" title="Add a port rule">
        <form onSubmit={submit} className="grid grid-cols-2 gap-3 lg:grid-cols-6">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="pr-name">Name</Label>
            <Input
              id="pr-name"
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="pr-port">Port</Label>
            <Input
              id="pr-port"
              type="number"
              min={1}
              max={65535}
              required
              value={form.port}
              onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="pr-protocol">Protocol</Label>
            <select
              id="pr-protocol"
              value={form.protocol}
              onChange={(e) =>
                setForm({ ...form, protocol: e.target.value as 'tcp' | 'udp' })
              }
              className="flex h-9 rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
            >
              <option value="tcp">tcp</option>
              <option value="udp">udp</option>
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="pr-expected">Expected</Label>
            <select
              id="pr-expected"
              value={form.expected_state}
              onChange={(e) =>
                setForm({
                  ...form,
                  expected_state: e.target.value as 'open' | 'closed',
                })
              }
              className="flex h-9 rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
            >
              <option value="closed">closed</option>
              <option value="open">open</option>
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="pr-severity">Severity</Label>
            <select
              id="pr-severity"
              value={form.severity}
              onChange={(e) => setForm({ ...form, severity: e.target.value })}
              className="flex h-9 rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
            >
              <option value="low">low</option>
              <option value="medium">medium</option>
              <option value="high">high</option>
              <option value="critical">critical</option>
            </select>
          </div>
          <div className="flex items-end">
            <Button type="submit" variant="primary" disabled={submitting} className="w-full">
              <Plus className="h-4 w-4" /> Add rule
            </Button>
          </div>
        </form>
      </Panel>

      <Panel
        padding="sm"
        tone="inset"
        eyebrow={`PORT RULES · ${rules.length}`}
        title="Active rules"
      >
        <DataTable
          columns={columns}
          rows={rules}
          rowKey={(r) => r.id}
          compact
          empty={
            <EmptyState
              title="No port rules yet"
              description="Add one above to start monitoring open/closed ports."
            />
          }
        />
      </Panel>
    </div>
  );
}

function LogRulesPane({
  tenantId,
  rules,
  onRefresh,
}: {
  tenantId: string;
  rules: LogRule[];
  onRefresh: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [form, setForm] = useState<CreateLogRulePayload>({
    tenant_id: tenantId,
    name: '',
    log_source: 'auth',
    pattern: '',
    severity: 'high',
    window_seconds: 60,
    threshold: 3,
    action: 'notify',
    enabled: true,
  });
  const [submitting, setSubmitting] = useState(false);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  useEffect(() => {
    setForm((f) => ({ ...f, tenant_id: tenantId }));
  }, [tenantId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!tenantId) return;
    setSubmitting(true);
    try {
      await client.createLogRule(form);
      setForm({ ...form, name: '', pattern: '' });
      onRefresh();
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (id: string) => {
    await client.deleteLogRule(id);
    onRefresh();
  };

  const columns: ColumnDef<LogRule>[] = [
    { accessorKey: 'name', header: 'Name' },
    { accessorKey: 'log_source', header: 'Source' },
    {
      accessorKey: 'pattern',
      header: 'Pattern',
      cell: ({ getValue }) => (
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] text-text-secondary">
          {String(getValue())}
        </code>
      ),
    },
    {
      accessorKey: 'window_seconds',
      header: 'Win',
      cell: ({ getValue }) => `${getValue()}s`,
    },
    { accessorKey: 'threshold', header: 'Thresh' },
    {
      accessorKey: 'severity',
      header: 'Sev',
      cell: ({ getValue }) => {
        const s = String(getValue());
        return <StatusTag tone={severityTone(s)}>{s}</StatusTag>;
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setConfirmDeleteId(row.original.id)}
        >
          <Trash2 className="h-4 w-4" />
          Delete
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <ConfirmModal
        open={confirmDeleteId !== null}
        title="Delete log rule?"
        body="This cannot be undone."
        variant="danger"
        confirmLabel="Delete"
        onConfirm={() => {
          if (confirmDeleteId) remove(confirmDeleteId);
          setConfirmDeleteId(null);
        }}
        onCancel={() => setConfirmDeleteId(null)}
      />

      <Panel padding="md" eyebrow="NEW RULE" title="Add a log rule">
        <form onSubmit={submit} className="grid grid-cols-2 gap-3 lg:grid-cols-7">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="lr-name">Name</Label>
            <Input
              id="lr-name"
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="lr-source">Source</Label>
            <Input
              id="lr-source"
              required
              value={form.log_source}
              onChange={(e) => setForm({ ...form, log_source: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1.5 lg:col-span-2">
            <Label htmlFor="lr-pattern">Pattern (regex)</Label>
            <Input
              id="lr-pattern"
              required
              value={form.pattern}
              onChange={(e) => setForm({ ...form, pattern: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="lr-window">Window (s)</Label>
            <Input
              id="lr-window"
              type="number"
              min={1}
              value={form.window_seconds}
              onChange={(e) =>
                setForm({ ...form, window_seconds: Number(e.target.value) })
              }
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="lr-threshold">Threshold</Label>
            <Input
              id="lr-threshold"
              type="number"
              min={1}
              value={form.threshold}
              onChange={(e) => setForm({ ...form, threshold: Number(e.target.value) })}
            />
          </div>
          <div className="flex items-end">
            <Button type="submit" variant="primary" disabled={submitting} className="w-full">
              <Plus className="h-4 w-4" /> Add rule
            </Button>
          </div>
        </form>
      </Panel>

      <Panel
        padding="sm"
        tone="inset"
        eyebrow={`LOG RULES · ${rules.length}`}
        title="Active rules"
      >
        <DataTable
          columns={columns}
          rows={rules}
          rowKey={(r) => r.id}
          compact
          empty={
            <EmptyState
              title="No log rules yet"
              description="Add one above to start matching log patterns."
            />
          }
        />
      </Panel>
    </div>
  );
}
