import { useMemo, useState } from 'react';
import { Download, FileText, Plus, RefreshCw, Play, Trash2, ChevronDown, ChevronRight, Tag } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import {
  Chart,
  DataTable,
  EmptyState,
  ExpandableCode,
  KpiTile,
  Panel,
  PostureBar,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../components/ui/tabs';
import { useComplianceResults, useComplianceSummary, useComplianceTrends } from '../hooks/useCompliance';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useApiClient } from '../hooks/useApiClient';
import { useToast } from '../providers/ToastProvider';
import type {
  ComplianceResult,
  Policy,
  PolicyVersion,
  ComplianceEvaluateResult,
} from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

type Tab = 'posture' | 'policies';

// ── Templates for common compliance rule types ────────────────────────────────
const RULE_TYPES = [
  'port_check',
  'log_pattern',
  'process_check',
  'file_integrity',
  'network_policy',
  'user_access',
  'patch_level',
  'service_state',
  'custom',
];

const RULE_TEMPLATES: Record<string, string> = {
  port_check: JSON.stringify({
    description: 'Ensure port is in expected state',
    port: 22,
    protocol: 'tcp',
    expected_state: 'open',
    severity: 'high',
  }, null, 2),
  log_pattern: JSON.stringify({
    description: 'Detect pattern in log source',
    log_source: 'syslog',
    pattern: 'authentication failure',
    threshold: 5,
    window_seconds: 300,
    severity: 'critical',
  }, null, 2),
  process_check: JSON.stringify({
    description: 'Verify required process is running',
    process_name: 'sshd',
    expected_running: true,
    severity: 'high',
  }, null, 2),
  file_integrity: JSON.stringify({
    description: 'Monitor file for unexpected changes',
    path: '/etc/passwd',
    check_hash: true,
    severity: 'critical',
  }, null, 2),
  user_access: JSON.stringify({
    description: 'Detect unauthorised sudo usage',
    log_source: 'auth',
    pattern: 'sudo:.*COMMAND',
    severity: 'medium',
  }, null, 2),
  service_state: JSON.stringify({
    description: 'Ensure systemd service is active',
    service: 'ufw',
    expected_state: 'active',
    severity: 'high',
  }, null, 2),
  custom: JSON.stringify({
    description: 'Custom rule definition',
    severity: 'medium',
  }, null, 2),
};

const RULESETS = ['cis-linux', 'cis-docker', 'nist-800-53', 'pci-dss', 'hipaa', 'soc2', 'iso27001', 'gdpr'];

function formatDate(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function severityTone(severity?: string): StateTone {
  switch ((severity ?? '').toLowerCase()) {
    case 'critical': return 'critical';
    case 'high': return 'degraded';
    case 'medium': return 'warning';
    case 'low': return 'info';
    default: return 'unknown';
  }
}

function exportToCSV(results: ComplianceResult[]): void {
  const headers = ['ID', 'Rule ID', 'Node ID', 'Passed', 'Severity', 'Checked At', 'Details'];
  const rows = results.map((r) => [
    r.id, r.rule_id, r.node_id || '', r.passed ? 'Yes' : 'No',
    r.severity || '', r.checked_at || '', r.details || '',
  ]);
  const csv = [
    headers.join(','),
    ...rows.map((row) =>
      row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','),
    ),
  ].join('\n');
  const blob = new Blob([csv], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `compliance-results-${new Date().toISOString().split('T')[0]}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

export function Compliance(): JSX.Element {
  const [tab, setTab] = useState<Tab>('posture');

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE · COMPLIANCE"
        title="Compliance"
        description="Define policies, run evaluations, prove continuous control."
      />
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList>
          <TabsTrigger value="posture">Posture</TabsTrigger>
          <TabsTrigger value="policies">Policies</TabsTrigger>
        </TabsList>
        <TabsContent value="posture" className="mt-5">
          <PostureTab />
        </TabsContent>
        <TabsContent value="policies" className="mt-5">
          <PoliciesTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ── Posture tab (existing) ────────────────────────────────────────────────────

function PostureTab(): JSX.Element {
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedNode, setSelectedNode] = useState<string | undefined>(undefined);
  const [severityFilter, setSeverityFilter] = useState<string>('');
  const [passedFilter, setPassedFilter] = useState<boolean | undefined>(undefined);
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);

  const { data: tenants } = useTenants();
  const { data: nodes } = useNodes({ tenantId: selectedTenant, limit: 1000 });

  const { data: summary, loading: summaryLoading, error: summaryError, reload: reloadSummary } =
    useComplianceSummary({ tenant_id: selectedTenant, node_id: selectedNode });

  const { data: trends, loading: trendsLoading } =
    useComplianceTrends({ tenant_id: selectedTenant, node_id: selectedNode, days: 30 });

  const { data: results, loading: resultsLoading, error: resultsError, pagination, reload: reloadResults } =
    useComplianceResults({
      tenant_id: selectedTenant,
      node_id: selectedNode,
      severity: severityFilter || undefined,
      passed: passedFilter,
      limit,
      offset,
    });

  const complianceScore = useMemo(() => {
    if (!summary || summary.total === 0) return null;
    return Math.round((summary.passed / summary.total) * 100);
  }, [summary]);

  const severityBreakdown = useMemo(() => {
    if (!summary) return [];
    return Object.entries(summary.by_severity || {})
      .map(([severity, count]) => ({ severity, count }))
      .sort((a, b) => {
        const order: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3, info: 4 };
        return (order[a.severity.toLowerCase()] ?? 99) - (order[b.severity.toLowerCase()] ?? 99);
      });
  }, [summary]);

  const handleRefresh = () => { reloadSummary(); reloadResults(); };

  const trendChartData = useMemo(() => {
    if (!trends.length) return null;
    const labels = trends.map((t) =>
      new Date(t.date).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
    );
    return {
      labels,
      datasets: [
        { label: 'Passed', data: trends.map((t) => t.passed), borderColor: 'var(--state-healthy)', backgroundColor: 'var(--state-healthy)' },
        { label: 'Failed', data: trends.map((t) => t.failed), borderColor: 'var(--state-critical)', backgroundColor: 'var(--state-critical)' },
      ],
    };
  }, [trends]);

  const columns = useMemo<ColumnDef<ComplianceResult>[]>(() => [
    {
      accessorKey: 'rule_id',
      header: 'Rule ID',
      cell: ({ getValue }) => (
        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] text-text-secondary">
          {getValue() as string}
        </code>
      ),
    },
    {
      id: 'node',
      header: 'Node',
      cell: ({ row }) => {
        const node = nodes.find((n) => n.id === row.original.node_id);
        return <span className="text-sm text-foreground">{node?.hostname || row.original.node_id || '—'}</span>;
      },
    },
    {
      accessorKey: 'passed',
      header: 'Status',
      cell: ({ getValue }) => {
        const passed = getValue() as boolean;
        return <StatusTag tone={passed ? 'healthy' : 'critical'}>{passed ? 'Passed' : 'Failed'}</StatusTag>;
      },
    },
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ getValue }) => {
        const sev = getValue() as string | undefined;
        if (!sev) return <span className="text-text-muted">—</span>;
        return <StatusTag tone={severityTone(sev)} className="font-mono uppercase">{sev}</StatusTag>;
      },
    },
    {
      accessorKey: 'checked_at',
      header: 'Checked At',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums text-text-secondary">{formatDate(getValue() as string)}</span>
      ),
    },
    {
      accessorKey: 'details',
      header: 'Details',
      cell: ({ getValue }) => {
        const d = getValue() as string | undefined;
        return d ? <ExpandableCode label="View details" content={d} /> : <span className="text-text-muted">—</span>;
      },
    },
  ], [nodes]);

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-end gap-2">
        <Button variant="secondary" size="md" onClick={handleRefresh} disabled={summaryLoading || resultsLoading}>
          <RefreshCw className="h-4 w-4" /> Refresh
        </Button>
        <Button variant="primary" size="md" onClick={() => exportToCSV(results)} disabled={results.length === 0}>
          <Download className="h-4 w-4" /> Export CSV
        </Button>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <KpiTile
          label="COMPLIANCE SCORE"
          value={complianceScore !== null ? `${complianceScore}%` : '—'}
          tone={complianceScore === null ? 'unknown' : complianceScore >= 80 ? 'healthy' : complianceScore >= 60 ? 'warning' : 'critical'}
          loading={summaryLoading}
          hint={summary ? `${summary.passed} of ${summary.total} checks passed` : undefined}
        />
        <KpiTile label="TOTAL CHECKS" value={summary?.total ?? 0} tone="brand" loading={summaryLoading} />
        <KpiTile label="PASSED" value={summary?.passed ?? 0} tone="healthy" loading={summaryLoading} />
        <KpiTile label="FAILED" value={summary?.failed ?? 0} tone={summary && summary.failed > 0 ? 'critical' : 'healthy'} loading={summaryLoading} />
      </div>

      {complianceScore !== null && (
        <Panel padding="md" eyebrow="FRAMEWORK SCORE" title="Posture">
          <PostureBar score={complianceScore} ariaLabel={`Compliance score ${complianceScore}%`} showLabels />
        </Panel>
      )}

      <Panel padding="md" eyebrow="FILTERS" title="Refine">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <FilterSelect label="Tenant" value={selectedTenant ?? ''}
            onChange={(v) => { setSelectedTenant(v || undefined); setSelectedNode(undefined); setOffset(0); }}
            options={[{ label: 'All tenants', value: '' }, ...tenants.map((t) => ({ label: t.name, value: t.id }))]}
          />
          <FilterSelect label="Node" value={selectedNode ?? ''}
            onChange={(v) => { setSelectedNode(v || undefined); setOffset(0); }}
            options={[{ label: 'All nodes', value: '' }, ...nodes.map((n) => ({ label: n.hostname, value: n.id }))]}
            disabled={!selectedTenant}
          />
          <FilterSelect label="Severity" value={severityFilter}
            onChange={(v) => { setSeverityFilter(v); setOffset(0); }}
            options={[
              { label: 'All severities', value: '' }, { label: 'Critical', value: 'critical' },
              { label: 'High', value: 'high' }, { label: 'Medium', value: 'medium' }, { label: 'Low', value: 'low' },
            ]}
          />
          <FilterSelect label="Status" value={passedFilter === undefined ? '' : passedFilter.toString()}
            onChange={(v) => { setPassedFilter(v === '' ? undefined : v === 'true'); setOffset(0); }}
            options={[{ label: 'All', value: '' }, { label: 'Passed', value: 'true' }, { label: 'Failed', value: 'false' }]}
          />
        </div>
      </Panel>

      {summaryError && (
        <Panel padding="md" toneAccent="critical" eyebrow="ERROR" title="Failed to load summary">
          <p className="text-sm text-state-critical">{summaryError}</p>
        </Panel>
      )}

      {severityBreakdown.length > 0 && (
        <Panel padding="md" eyebrow="VIOLATIONS" title="By severity">
          <div className="flex flex-wrap gap-3">
            {severityBreakdown.map(({ severity, count }) => (
              <div key={severity} className="flex items-center gap-2 rounded-md border border-border-subtle bg-surface px-3 py-1.5">
                <StatusTag tone={severityTone(severity)} className="font-mono uppercase">{severity}</StatusTag>
                <span className="font-mono text-sm tabular-nums text-foreground">{count}</span>
              </div>
            ))}
          </div>
        </Panel>
      )}

      {trendChartData && (
        <Panel padding="md" eyebrow="TRENDS · 30 DAYS" title="Compliance trend" loading={trendsLoading}>
          <div className="h-56">
            <Chart kind="line" data={trendChartData} ariaLabel="Compliance trend" />
          </div>
        </Panel>
      )}

      <Panel padding="sm" tone="inset" eyebrow={`RESULTS · ${results.length} of ${pagination.total}`} title="Compliance results">
        <DataTable
          columns={columns} rows={results} rowKey={(r) => r.id}
          loading={resultsLoading} compact
          empty={<EmptyState icon={<FileText />} title="No compliance results" description="No results match the current filters." />}
        />
        <div className="flex items-center justify-between gap-2 border-t border-border-subtle p-3">
          <Button variant="secondary" size="sm" onClick={() => setOffset(Math.max(0, offset - limit))} disabled={offset === 0 || resultsLoading}>Previous</Button>
          <span className="font-mono text-xs text-text-muted">Page {Math.floor(offset / limit) + 1} of {Math.ceil(pagination.total / limit) || 1}</span>
          <Button variant="secondary" size="sm" onClick={() => setOffset(offset + limit)} disabled={offset + limit >= pagination.total || resultsLoading}>Next</Button>
        </div>
      </Panel>

      {resultsError && (
        <Panel padding="md" toneAccent="critical" eyebrow="ERROR" title="Failed to load results">
          <p className="text-sm text-state-critical">{resultsError}</p>
        </Panel>
      )}
    </div>
  );
}

// ── Policies tab ─────────────────────────────────────────────────────────────

function PoliciesTab(): JSX.Element {
  const api = useApiClient();
  const { showToast } = useToast();
  const { data: tenants } = useTenants();
  const { data: nodes } = useNodes({ limit: 1000 });

  // Policy list state
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [policiesLoading, setPoliciesLoading] = useState(false);
  const [policiesLoaded, setPoliciesLoaded] = useState(false);
  const [tenantFilter, setTenantFilter] = useState('');

  // Create policy form
  const [showCreate, setShowCreate] = useState(false);
  const [createName, setCreateName] = useState('');
  const [createDesc, setCreateDesc] = useState('');
  const [createRuleType, setCreateRuleType] = useState('port_check');
  const [createTenantId, setCreateTenantId] = useState('');
  const [createEnabled, setCreateEnabled] = useState(true);
  const [creating, setCreating] = useState(false);

  // Expanded policy (versions + create version)
  const [expandedPolicyId, setExpandedPolicyId] = useState<string | null>(null);
  const [versionsMap, setVersionsMap] = useState<Record<string, PolicyVersion[]>>({});
  const [versionsLoadingId, setVersionsLoadingId] = useState<string | null>(null);

  // Create version form
  const [versionPolicyId, setVersionPolicyId] = useState<string | null>(null);
  const [versionDef, setVersionDef] = useState('');
  const [creatingVersion, setCreatingVersion] = useState(false);

  // Evaluate form
  const [evalPolicyId, setEvalPolicyId] = useState<string | null>(null);
  const [evalNodeId, setEvalNodeId] = useState('');
  const [evalRegion, setEvalRegion] = useState('us-east-1');
  const [evalRulesets, setEvalRulesets] = useState<string[]>(['cis-linux']);
  const [evaluating, setEvaluating] = useState(false);
  const [evalResults, setEvalResults] = useState<ComplianceEvaluateResult[] | null>(null);

  const loadPolicies = async () => {
    setPoliciesLoading(true);
    try {
      const res = await api.listPolicies({ tenant_id: tenantFilter || undefined, limit: 100 });
      setPolicies(res.data);
      setPoliciesLoaded(true);
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to load policies', 'error');
    } finally {
      setPoliciesLoading(false);
    }
  };

  const loadVersions = async (policyId: string) => {
    setVersionsLoadingId(policyId);
    try {
      const res = await api.listPolicyVersions(policyId);
      setVersionsMap((prev) => ({ ...prev, [policyId]: res.data }));
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to load versions', 'error');
    } finally {
      setVersionsLoadingId(null);
    }
  };

  const toggleExpand = (policyId: string) => {
    if (expandedPolicyId === policyId) {
      setExpandedPolicyId(null);
    } else {
      setExpandedPolicyId(policyId);
      if (!versionsMap[policyId]) {
        loadVersions(policyId);
      }
    }
  };

  const handleCreate = async () => {
    if (!createName.trim()) { showToast('Name is required', 'error'); return; }
    setCreating(true);
    try {
      const policy = await api.createPolicy({
        name: createName.trim(),
        description: createDesc.trim() || undefined,
        rule_type: createRuleType,
        tenant_id: createTenantId || undefined,
        enabled: createEnabled,
      });
      showToast(`Policy "${policy.name}" created`, 'success');
      setCreateName(''); setCreateDesc(''); setCreateRuleType('port_check');
      setCreateTenantId(''); setShowCreate(false);
      await loadPolicies();
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to create policy', 'error');
    } finally {
      setCreating(false);
    }
  };

  const handleToggleEnabled = async (policy: Policy) => {
    try {
      await api.updatePolicy(policy.id, { enabled: !policy.enabled });
      showToast(`Policy ${policy.enabled ? 'disabled' : 'enabled'}`, 'success');
      await loadPolicies();
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to update policy', 'error');
    }
  };

  const handleDelete = async (policy: Policy) => {
    if (!window.confirm(`Delete policy "${policy.name}"?`)) return;
    try {
      await api.deletePolicy(policy.id);
      showToast(`Policy "${policy.name}" deleted`, 'success');
      await loadPolicies();
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to delete policy', 'error');
    }
  };

  const handleCreateVersion = async () => {
    if (!versionPolicyId || !versionDef.trim()) {
      showToast('Rule definition is required', 'error'); return;
    }
    setCreatingVersion(true);
    try {
      const ver = await api.createPolicyVersion(versionPolicyId, { rule_definition: versionDef.trim() });
      showToast(`Version ${ver.version} created`, 'success');
      setVersionDef(''); setVersionPolicyId(null);
      // Refresh versions for this policy
      await loadVersions(versionPolicyId);
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to create version', 'error');
    } finally {
      setCreatingVersion(false);
    }
  };

  const handlePromoteVersion = async (policyId: string, version: number) => {
    try {
      await api.promotePolicyVersion(policyId, version);
      showToast(`Version ${version} promoted`, 'success');
      await loadVersions(policyId);
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to promote version', 'error');
    }
  };

  const handleEvaluate = async () => {
    if (!evalNodeId) { showToast('Select a node to evaluate', 'error'); return; }
    if (evalRulesets.length === 0) { showToast('Select at least one ruleset', 'error'); return; }
    setEvaluating(true);
    setEvalResults(null);
    try {
      const res = await api.evaluateCompliance({
        node_id: evalNodeId,
        region: evalRegion || 'us-east-1',
        rulesets: evalRulesets,
        use_real_scan: true,
      });
      setEvalResults(res.results);
      showToast(`Evaluation complete — ${res.results.length} result(s)`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Evaluation failed', 'error');
    } finally {
      setEvaluating(false);
    }
  };

  const toggleRuleset = (rs: string) => {
    setEvalRulesets((prev) =>
      prev.includes(rs) ? prev.filter((r) => r !== rs) : [...prev, rs],
    );
  };

  return (
    <div className="flex flex-col gap-5">
      {/* Run evaluation panel */}
      <Panel padding="md" eyebrow="EVALUATE" title="Run compliance scan" toneAccent="brand">
        <p className="text-sm text-text-secondary">
          Trigger an on-demand compliance evaluation against a node. Select rulesets and a target node.
        </p>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <SelectField id="eval-node" label="Target node" value={evalNodeId} onChange={(e) => setEvalNodeId(e.target.value)}>
            <option value="">Select node…</option>
            {nodes.map((n) => (
              <option key={n.id} value={n.id}>{n.hostname}</option>
            ))}
          </SelectField>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="eval-region">Region</Label>
            <Input id="eval-region" type="text" value={evalRegion} onChange={(e) => setEvalRegion(e.target.value)} placeholder="us-east-1" />
          </div>
          <div className="flex flex-col gap-1.5 sm:col-span-1">
            <span className="text-sm font-medium leading-none text-foreground">Rulesets</span>
            <div className="flex flex-wrap gap-1.5">
              {RULESETS.map((rs) => (
                <button
                  key={rs}
                  type="button"
                  onClick={() => toggleRuleset(rs)}
                  className={`rounded-full px-2.5 py-1 font-mono text-[0.65rem] uppercase tracking-wider transition-colors ${
                    evalRulesets.includes(rs)
                      ? 'bg-brand-500 text-white'
                      : 'border border-border-subtle bg-surface text-text-muted hover:border-brand-500/50 hover:text-foreground'
                  }`}
                >
                  {rs}
                </button>
              ))}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2 pt-1">
          <Button variant="primary" onClick={handleEvaluate} disabled={evaluating || !evalNodeId}>
            <Play className="h-4 w-4" />
            {evaluating ? 'Evaluating…' : 'Run evaluation'}
          </Button>
        </div>

        {evalResults !== null && (
          <div className="mt-3 flex flex-col gap-2">
            <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
              Results ({evalResults.length})
            </p>
            <div className="overflow-x-auto rounded-md border border-border-subtle">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border-subtle bg-surface/60">
                    {['Rule ID', 'Status', 'Severity', 'Details'].map((h) => (
                      <th key={h} className="px-3 py-2 text-left font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {evalResults.map((r, i) => (
                    <tr key={i} className="border-b border-border-subtle last:border-0">
                      <td className="px-3 py-2">
                        <code className="font-mono text-xs text-text-secondary">{r.rule_id}</code>
                      </td>
                      <td className="px-3 py-2">
                        <StatusTag tone={r.passed ? 'healthy' : 'critical'}>{r.passed ? 'Passed' : 'Failed'}</StatusTag>
                      </td>
                      <td className="px-3 py-2">
                        {r.severity ? (
                          <StatusTag tone={severityTone(r.severity)} className="font-mono uppercase">{r.severity}</StatusTag>
                        ) : <span className="text-text-muted">—</span>}
                      </td>
                      <td className="max-w-xs px-3 py-2">
                        {r.details ? (
                          <ExpandableCode label="Details" content={r.details} />
                        ) : <span className="text-text-muted">—</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </Panel>

      {/* Policy list + create */}
      <Panel
        padding="md"
        eyebrow="POLICIES"
        title="Compliance policies"
        actions={
          <div className="flex items-center gap-2">
            <SelectField value={tenantFilter} onChange={(e) => setTenantFilter(e.target.value)}>
              <option value="">All tenants</option>
              {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
            </SelectField>
            <Button variant="secondary" size="sm" onClick={loadPolicies} disabled={policiesLoading}>
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
            <Button variant="primary" size="sm" onClick={() => setShowCreate((s) => !s)}>
              <Plus className="h-3.5 w-3.5" /> New policy
            </Button>
          </div>
        }
      >
        {/* Create policy form */}
        {showCreate && (
          <div className="rounded-md border border-brand-500/30 bg-brand-500/5 p-4">
            <p className="mb-3 font-mono text-[0.65rem] uppercase tracking-wider text-brand-400">New policy</p>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="cp-name">Name</Label>
                <Input id="cp-name" type="text" value={createName} onChange={(e) => setCreateName(e.target.value)} placeholder="SSH port open" />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="cp-desc">Description (optional)</Label>
                <Input id="cp-desc" type="text" value={createDesc} onChange={(e) => setCreateDesc(e.target.value)} placeholder="Verify SSH is accessible" />
              </div>
              <SelectField id="cp-type" label="Rule type" value={createRuleType} onChange={(e) => {
                setCreateRuleType(e.target.value);
              }}>
                {RULE_TYPES.map((rt) => <option key={rt} value={rt}>{rt}</option>)}
              </SelectField>
              <SelectField id="cp-tenant" label="Tenant (optional)" value={createTenantId} onChange={(e) => setCreateTenantId(e.target.value)}>
                <option value="">Global (all tenants)</option>
                {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
              </SelectField>
            </div>
            <label className="mt-3 flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
              <input type="checkbox" checked={createEnabled} onChange={(e) => setCreateEnabled(e.target.checked)} className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer" />
              Enable immediately
            </label>
            <div className="mt-3 flex gap-2">
              <Button variant="primary" size="sm" onClick={handleCreate} disabled={creating}>
                {creating ? 'Creating…' : 'Create policy'}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setShowCreate(false)}>Cancel</Button>
            </div>
          </div>
        )}

        {/* Load prompt */}
        {!policiesLoaded && !policiesLoading && (
          <EmptyState
            title="Load policies"
            description="Click the refresh button to load compliance policies."
            icon={<Tag />}
            action={<Button variant="secondary" onClick={loadPolicies}>Load policies</Button>}
          />
        )}

        {policiesLoading && (
          <p className="py-4 text-center text-sm text-text-muted">Loading policies…</p>
        )}

        {policiesLoaded && policies.length === 0 && (
          <EmptyState
            title="No policies"
            description="Create a policy to start defining compliance checks."
            icon={<FileText />}
          />
        )}

        {/* Policy rows */}
        {policies.map((policy) => (
          <PolicyRow
            key={policy.id}
            policy={policy}
            tenants={tenants}
            expanded={expandedPolicyId === policy.id}
            versions={versionsMap[policy.id] ?? null}
            versionsLoading={versionsLoadingId === policy.id}
            versionPolicyId={versionPolicyId}
            versionDef={versionDef}
            creatingVersion={creatingVersion}
            onToggleExpand={() => toggleExpand(policy.id)}
            onToggleEnabled={() => handleToggleEnabled(policy)}
            onDelete={() => handleDelete(policy)}
            onOpenCreateVersion={() => {
              setVersionPolicyId(policy.id);
              setVersionDef(RULE_TEMPLATES[policy.rule_type] ?? RULE_TEMPLATES.custom);
            }}
            onCloseCreateVersion={() => setVersionPolicyId(null)}
            onVersionDefChange={setVersionDef}
            onCreateVersion={handleCreateVersion}
            onPromoteVersion={(ver) => handlePromoteVersion(policy.id, ver)}
            onEvaluate={() => setEvalPolicyId(evalPolicyId === policy.id ? null : policy.id)}
          />
        ))}
      </Panel>
    </div>
  );
}

interface PolicyRowProps {
  policy: Policy;
  tenants: Array<{ id: string; name: string }>;
  expanded: boolean;
  versions: PolicyVersion[] | null;
  versionsLoading: boolean;
  versionPolicyId: string | null;
  versionDef: string;
  creatingVersion: boolean;
  onToggleExpand: () => void;
  onToggleEnabled: () => void;
  onDelete: () => void;
  onOpenCreateVersion: () => void;
  onCloseCreateVersion: () => void;
  onVersionDefChange: (v: string) => void;
  onCreateVersion: () => void;
  onPromoteVersion: (version: number) => void;
  onEvaluate: () => void;
}

function PolicyRow({
  policy,
  tenants,
  expanded,
  versions,
  versionsLoading,
  versionPolicyId,
  versionDef,
  creatingVersion,
  onToggleExpand,
  onToggleEnabled,
  onDelete,
  onOpenCreateVersion,
  onCloseCreateVersion,
  onVersionDefChange,
  onCreateVersion,
  onPromoteVersion,
}: PolicyRowProps): JSX.Element {
  const tenantName = tenants.find((t) => t.id === policy.tenant_id)?.name;

  return (
    <div className="rounded-md border border-border-subtle bg-surface">
      {/* Header row */}
      <div className="flex items-center gap-3 px-4 py-3">
        <button type="button" onClick={onToggleExpand} className="shrink-0 text-text-muted hover:text-foreground">
          {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        </button>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-foreground">{policy.name}</span>
            <StatusTag tone={policy.enabled ? 'healthy' : 'unknown'}>
              {policy.enabled ? 'enabled' : 'disabled'}
            </StatusTag>
            <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[0.65rem] text-text-secondary">
              {policy.rule_type}
            </code>
            {tenantName && (
              <span className="text-xs text-text-muted">{tenantName}</span>
            )}
          </div>
          {policy.description && (
            <p className="mt-0.5 text-xs text-text-secondary">{policy.description}</p>
          )}
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={onToggleEnabled}>
            {policy.enabled ? 'Disable' : 'Enable'}
          </Button>
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-state-critical hover:text-state-critical" onClick={onDelete}>
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Expanded: versions + create version */}
      {expanded && (
        <div className="border-t border-border-subtle px-4 py-3 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Versions</p>
            <Button type="button" variant="secondary" size="sm" className="h-7 gap-1 px-2 text-xs" onClick={onOpenCreateVersion}>
              <Plus className="h-3 w-3" /> New version
            </Button>
          </div>

          {versionsLoading && <p className="text-xs text-text-muted">Loading versions…</p>}

          {versions !== null && versions.length === 0 && (
            <p className="text-xs text-text-muted">No versions yet. Create a version to activate this policy.</p>
          )}

          {versions !== null && versions.map((ver) => (
            <div key={ver.id} className="flex items-start gap-3 rounded-md border border-border-subtle bg-elevated px-3 py-2">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs text-text-secondary">v{ver.version}</span>
                  {ver.promoted_at && (
                    <StatusTag tone="healthy">active</StatusTag>
                  )}
                  <span className="text-xs text-text-muted">{formatDate(ver.created_at)}</span>
                  {ver.created_by && (
                    <span className="text-xs text-text-muted">by {ver.created_by}</span>
                  )}
                </div>
                <ExpandableCode label="Rule definition" content={ver.rule_definition} />
              </div>
              {!ver.promoted_at && (
                <Button type="button" variant="secondary" size="sm" className="h-7 shrink-0 text-xs" onClick={() => onPromoteVersion(ver.version)}>
                  Promote
                </Button>
              )}
            </div>
          ))}

          {/* Create version inline form */}
          {versionPolicyId === policy.id && (
            <div className="rounded-md border border-brand-500/30 bg-brand-500/5 p-3 flex flex-col gap-2">
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-brand-400">New version · rule definition (JSON)</p>
              <textarea
                className="flex min-h-[120px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
                value={versionDef}
                onChange={(e) => onVersionDefChange(e.target.value)}
                rows={6}
                placeholder='{"port": 22, "protocol": "tcp", "expected_state": "open"}'
              />
              <div className="flex gap-2">
                <Button variant="primary" size="sm" onClick={onCreateVersion} disabled={creatingVersion}>
                  {creatingVersion ? 'Creating…' : 'Create version'}
                </Button>
                <Button variant="ghost" size="sm" onClick={onCloseCreateVersion}>Cancel</Button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { label: string; value: string }[];
  disabled?: boolean;
}) {
  return (
    <SelectField label={label} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>{o.label}</option>
      ))}
    </SelectField>
  );
}
