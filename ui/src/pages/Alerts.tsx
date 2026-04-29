import { useCallback, useEffect, useMemo, useState } from 'react';
import { Bell, Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { ConfirmModal } from '../components/ConfirmModal';
import {
  DataTable,
  EmptyState,
  EntityChip,
  KpiTile,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
import { classifyValue } from '../lib/entity';
import type { Alert, CorrelationRule } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

type PageTab = 'alerts' | 'rules';

const STATE_FILTERS = ['open', 'acked', 'resolved'] as const;

function severityTone(sev: string | undefined): StateTone {
  switch ((sev ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
      return 'info';
    case 'info':
      return 'info';
    default:
      return 'unknown';
  }
}

function stateTone(state: string): StateTone {
  switch (state) {
    case 'open':
      return 'critical';
    case 'acked':
      return 'warning';
    case 'resolved':
      return 'healthy';
    default:
      return 'unknown';
  }
}

export function Alerts(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [pageTab, setPageTab] = useState<PageTab>('alerts');
  const [tenantId, setTenantId] = useState('');
  const [state, setState] = useState<typeof STATE_FILTERS[number]>('open');
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Correlation rules state
  const [rules, setRules] = useState<CorrelationRule[]>([]);
  const [rulesLoading, setRulesLoading] = useState(false);
  const [rulesReloadToken, setRulesReloadToken] = useState(0);
  const [deleteRuleId, setDeleteRuleId] = useState<string | null>(null);
  const [showCreateRule, setShowCreateRule] = useState(false);
  const [newRuleName, setNewRuleName] = useState('');
  const [newRuleSeverity, setNewRuleSeverity] = useState('medium');
  const [creatingRule, setCreatingRule] = useState(false);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    try {
      const resp = await client.listAlerts({ tenantId, state, limit: 100, offset: 0 });
      setAlerts(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId, state]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEventStream(tenantId, ['alert.opened'], () => refresh());

  const ack = async (id: string) => {
    await client.ackAlert(id);
    refresh();
  };
  const resolve = async (id: string) => {
    await client.resolveAlert(id);
    refresh();
  };

  // Correlation rules
  useEffect(() => {
    let cancelled = false;
    setRulesLoading(true);
    client
      .listCorrelationRules({ tenantId: tenantId || undefined })
      .then((r) => { if (!cancelled) setRules(r.data ?? []); })
      .catch(() => { if (!cancelled) setRules([]); })
      .finally(() => { if (!cancelled) setRulesLoading(false); });
    return () => { cancelled = true; };
  }, [client, tenantId, rulesReloadToken]);

  const handleCreateRule = async () => {
    if (!newRuleName.trim() || !tenantId) return;
    setCreatingRule(true);
    try {
      await client.createCorrelationRule({
        tenant_id: tenantId,
        name: newRuleName.trim(),
        severity: newRuleSeverity,
        conditions: {},
        enabled: true,
      });
      setNewRuleName('');
      setShowCreateRule(false);
      setRulesReloadToken((n) => n + 1);
    } finally {
      setCreatingRule(false);
    }
  };

  const handleDeleteRule = async () => {
    if (!deleteRuleId) return;
    await client.deleteCorrelationRule(deleteRuleId);
    setDeleteRuleId(null);
    setRulesReloadToken((n) => n + 1);
  };

  const counts = useMemo(() => {
    const c = { critical: 0, high: 0, total: alerts.length };
    for (const a of alerts) {
      const sev = (a.severity ?? '').toLowerCase();
      if (sev === 'critical') c.critical += 1;
      else if (sev === 'high') c.high += 1;
    }
    return c;
  }, [alerts]);

  const columns = useMemo<ColumnDef<Alert>[]>(() => [
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)} className="font-mono uppercase">
          {row.original.severity || 'unknown'}
        </StatusTag>
      ),
    },
    {
      id: 'title',
      header: 'Title',
      cell: ({ row }) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium text-foreground">{row.original.title}</span>
          {row.original.summary ? (
            <span className="text-xs text-text-muted">{row.original.summary}</span>
          ) : null}
        </div>
      ),
    },
    {
      accessorKey: 'source',
      header: 'Source',
      cell: ({ getValue }) => {
        const v = getValue() as string;
        const det = v ? classifyValue(v) : { type: 'unknown' as const };
        return det.type !== 'unknown' ? (
          <EntityChip type={det.type} value={v} />
        ) : (
          <span className="font-mono text-xs text-text-secondary">{v || '—'}</span>
        );
      },
    },
    {
      accessorKey: 'opened_at',
      header: 'Opened',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums text-text-secondary">
          {new Date(getValue() as string).toLocaleString()}
        </span>
      ),
    },
    {
      accessorKey: 'state',
      header: 'State',
      cell: ({ getValue }) => {
        const s = getValue() as string;
        return <StatusTag tone={stateTone(s)}>{s}</StatusTag>;
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          {row.original.state === 'open' ? (
            <Button variant="secondary" size="sm" onClick={() => ack(row.original.id)}>
              Ack
            </Button>
          ) : null}
          {row.original.state !== 'resolved' ? (
            <Button variant="primary" size="sm" onClick={() => resolve(row.original.id)}>
              Resolve
            </Button>
          ) : null}
        </div>
      ),
    },
    // ack/resolve don't change identity — eslint-disable: dependencies stable via closure
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], []);

  const ruleColumns: ColumnDef<CorrelationRule>[] = [
    {
      header: 'Name',
      accessorKey: 'name',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
    },
    {
      header: 'Severity',
      accessorKey: 'severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)} className="uppercase">
          {row.original.severity}
        </StatusTag>
      ),
    },
    {
      header: 'Enabled',
      accessorKey: 'enabled',
      cell: ({ row }) =>
        row.original.enabled ? (
          <StatusTag tone="healthy">On</StatusTag>
        ) : (
          <StatusTag tone="unknown">Off</StatusTag>
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
        <Button variant="ghost" size="sm" onClick={() => setDeleteRuleId(row.original.id)}>
          <Trash2 className="h-3.5 w-3.5 text-state-critical" />
        </Button>
      ),
    },
  ];

  const allClear = !loading && alerts.length === 0 && state === 'open';

  const tabClass = (t: PageTab) =>
    `px-4 py-2 text-sm font-medium rounded-t-md transition-colors ${
      pageTab === t
        ? 'bg-surface text-foreground border border-border-subtle border-b-surface'
        : 'text-text-secondary hover:text-foreground'
    }`;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="VISIBILITY · ALERTS"
        title="Alerts"
        description="Deduped inbox from correlation, rules, and compliance."
        actions={
          <Button variant="secondary" size="md" onClick={refresh} disabled={loading}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        }
      />

      <div className="flex gap-1 border-b border-border-subtle">
        <button type="button" className={tabClass('alerts')} onClick={() => setPageTab('alerts')}>
          Inbox
          {counts.total > 0 && (
            <span className="ml-1.5 rounded-full bg-state-critical/20 px-1.5 py-0.5 text-xs text-state-critical">
              {counts.total}
            </span>
          )}
        </button>
        <button type="button" className={tabClass('rules')} onClick={() => setPageTab('rules')}>
          Correlation rules
        </button>
      </div>

      {pageTab === 'alerts' && (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <KpiTile label="OPEN ALERTS" value={counts.total} tone={counts.total > 0 ? 'warning' : 'healthy'} />
            <KpiTile label="CRITICAL" value={counts.critical} tone={counts.critical > 0 ? 'critical' : 'healthy'} />
            <KpiTile label="HIGH" value={counts.high} tone={counts.high > 0 ? 'degraded' : 'healthy'} />
          </div>

          <Panel padding="md" eyebrow="FILTERS" title="Refine">
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
              <FilterSelect
                label="Tenant"
                value={tenantId}
                onChange={(v) => setTenantId(v)}
                options={tenants.map((t) => ({ label: t.name, value: t.id }))}
              />
              <FilterSelect
                label="State"
                value={state}
                onChange={(v) => setState(v as typeof STATE_FILTERS[number])}
                options={STATE_FILTERS.map((s) => ({ label: s, value: s }))}
              />
            </div>
          </Panel>

          {error && (
            <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load">
              <p className="text-sm text-state-critical">{error}</p>
            </Panel>
          )}

          <Panel padding="sm" tone="inset" eyebrow={`ALERTS · ${alerts.length}`} title="Inbox">
            <DataTable
              columns={columns}
              rows={alerts}
              rowKey={(r) => r.id}
              loading={loading}
              compact
              empty={
                allClear ? (
                  <EmptyState
                    tone="success"
                    icon={<ShieldCheck />}
                    title="All clear"
                    description="No open alerts. Detection rules are healthy and the inbox is empty."
                  />
                ) : (
                  <EmptyState
                    icon={<Bell />}
                    title="No alerts"
                    description={`No alerts in state "${state}".`}
                  />
                )
              }
            />
          </Panel>
        </>
      )}

      {pageTab === 'rules' && (
        <Panel
          padding="md"
          eyebrow="CORRELATION RULES"
          title="Detection rules"
          actions={
            <Button variant="primary" size="sm" onClick={() => setShowCreateRule(true)}>
              <Plus className="h-3.5 w-3.5" /> New rule
            </Button>
          }
        >
          {showCreateRule && (
            <div className="mb-4 rounded-md border border-border-subtle bg-elevated p-4">
              <p className="mb-3 text-sm font-medium text-foreground">New correlation rule</p>
              <div className="flex flex-wrap items-end gap-3">
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-name">Name</Label>
                  <Input
                    id="rule-name"
                    value={newRuleName}
                    onChange={(e) => setNewRuleName(e.target.value)}
                    placeholder="Brute-force SSH"
                    className="h-8 w-56"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-severity">Severity</Label>
                  <select
                    id="rule-severity"
                    className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                    value={newRuleSeverity}
                    onChange={(e) => setNewRuleSeverity(e.target.value)}
                  >
                    {['low', 'medium', 'high', 'critical'].map((s) => (
                      <option key={s} value={s}>{s}</option>
                    ))}
                  </select>
                </div>
                <div className="flex gap-2">
                  <Button variant="primary" size="sm" onClick={handleCreateRule} disabled={creatingRule || !newRuleName.trim() || !tenantId}>
                    {creatingRule ? 'Creating…' : 'Create'}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => setShowCreateRule(false)}>
                    Cancel
                  </Button>
                </div>
              </div>
              {!tenantId && (
                <p className="mt-2 text-xs text-state-warning">Select a tenant in the Inbox tab first.</p>
              )}
            </div>
          )}

          <DataTable
            columns={ruleColumns}
            rows={rules}
            rowKey={(r) => r.id}
            loading={rulesLoading}
            empty={
              <EmptyState
                icon={<ShieldCheck />}
                title="No correlation rules"
                description="Create a rule to start correlating events into alerts."
              />
            }
          />
        </Panel>
      )}

      <ConfirmModal
        open={deleteRuleId !== null}
        title="Delete correlation rule?"
        body="This rule will stop generating alerts immediately."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={handleDeleteRule}
        onCancel={() => setDeleteRuleId(null)}
      />
    </div>
  );
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { label: string; value: string }[];
}) {
  return (
    <SelectField
      label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </SelectField>
  );
}
