import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Download, FileText, Plus, RefreshCw, Play, Trash2, ChevronDown, ChevronRight, Tag } from 'lucide-react';
import { ConfirmModal } from '@/components/ConfirmModal';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import {
  Alert,
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
import { useComplianceResults, useComplianceSummary, useComplianceTrends, useControlPosture } from '../hooks/useCompliance';
import { ControlPosturePanel } from '../components/compliance/ControlPosturePanel';
import {
  ScopePicker,
  buildScopedAssignmentPayload,
  describeAssignmentScope,
  type ScopePickerValue,
} from '../components/compliance/ScopePicker';
import { CoverageTruthList } from '../components/coverage/CoverageTruth';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useApiClient } from '../hooks/useApiClient';
import { useCoverageMatrix } from '../hooks/useCoverageMatrix';
import { useToast } from '../providers/ToastProvider';
import { useTenant } from '../providers/TenantProvider';
import type {
  ComplianceResult,
  Policy,
  PolicyAssignment,
  PolicyVersion,
  ComplianceEvaluateResult,
} from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';
import { ComplianceEvidence } from './ComplianceEvidence';
import { Frameworks } from './Frameworks';
import { AuditReports } from './AuditReports';

type Tab = 'posture' | 'policies' | 'evidence' | 'frameworks' | 'reports';

const COMPLIANCE_TABS = ['posture', 'policies', 'evidence', 'frameworks', 'reports'] as const;

interface InlineActionState {
  busy?: boolean;
  message?: string;
  tone?: StateTone;
}

type PendingPolicyDelete = { policy: Policy; error?: string };
type PendingAssignmentDelete = { assignment: PolicyAssignment; error?: string };

function complianceTabFromParams(params: URLSearchParams): Tab {
  const value = params.get('tab');
  return COMPLIANCE_TABS.includes(value as Tab) ? (value as Tab) : 'posture';
}

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
  if (!value) return '-';
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
  const [params, setParams] = useSearchParams();
  const [tab, setTab] = useState<Tab>(() => complianceTabFromParams(params));

  useEffect(() => {
    const next = complianceTabFromParams(params);
    setTab((current) => (current === next ? current : next));
  }, [params]);

  const onTabChange = (value: string) => {
    if (!COMPLIANCE_TABS.includes(value as Tab)) return;
    const next = value as Tab;
    setTab(next);
    const updated = new URLSearchParams(params);
    if (next === 'posture') updated.delete('tab');
    else updated.set('tab', next);
    setParams(updated, { replace: true });
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE / COMPLIANCE"
        title="Compliance"
        description="Define policies, run evaluations, prove continuous control."
      />
      <Tabs value={tab} onValueChange={onTabChange}>
        <TabsList className="grid h-auto w-full grid-cols-2 gap-1 overflow-visible sm:inline-flex sm:w-auto sm:grid-cols-none">
          <TabsTrigger className="w-full sm:w-auto" value="posture">Posture</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="policies">Policies</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="evidence">Evidence</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="frameworks">Frameworks</TabsTrigger>
          <TabsTrigger className="w-full sm:w-auto" value="reports">Reports</TabsTrigger>
        </TabsList>
        <TabsContent value="posture" className="mt-5">
          <PostureTab />
        </TabsContent>
        <TabsContent value="policies" className="mt-5">
          <PoliciesTab />
        </TabsContent>
        <TabsContent value="evidence" className="mt-5">
          <ComplianceEvidence />
        </TabsContent>
        <TabsContent value="frameworks" className="mt-5">
          <Frameworks />
        </TabsContent>
        <TabsContent value="reports" className="mt-5">
          <AuditReports />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ── Posture tab (existing) ────────────────────────────────────────────────────

function PostureTab(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedNode, setSelectedNode] = useState<string | undefined>(undefined);
  const [severityFilter, setSeverityFilter] = useState<string>('');
  const [passedFilter, setPassedFilter] = useState<boolean | undefined>(undefined);
  const [frameworkFilter, setFrameworkFilter] = useState<string>('');
  const [availableFrameworks, setAvailableFrameworks] = useState<string[]>([]);
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);
  const [postureFramework, setPostureFramework] = useState<string>('SOC2');

  const { data: tenants } = useTenants();
  const effectiveTenantId = selectedTenant ?? currentTenantId ?? undefined;
  const { data: nodes } = useNodes({ tenantId: effectiveTenantId, limit: 1000 });

  const { data: postureData, loading: postureLoading, error: postureError } = useControlPosture({
    framework: postureFramework,
    tenant_id: effectiveTenantId,
  });
  const coverageTenantId = effectiveTenantId;
  const {
    data: coverageMatrix,
    loading: coverageLoading,
    error: coverageError,
    unavailable: coverageUnavailable,
    reload: reloadCoverage,
  } = useCoverageMatrix({ tenantId: coverageTenantId });

  useEffect(() => {
    client.listComplianceFrameworks()
      .then((res) => setAvailableFrameworks(res.frameworks))
      .catch(() => { /* non-critical */ });
  }, [client]);

  const { data: summary, loading: summaryLoading, error: summaryError, reload: reloadSummary } =
    useComplianceSummary({ tenant_id: effectiveTenantId, node_id: selectedNode });

  const { data: trends, loading: trendsLoading } =
    useComplianceTrends({ tenant_id: effectiveTenantId, node_id: selectedNode, days: 30 });

  const { data: results, loading: resultsLoading, error: resultsError, pagination, reload: reloadResults } =
    useComplianceResults({
      tenant_id: effectiveTenantId,
      node_id: selectedNode,
      severity: severityFilter || undefined,
      framework: frameworkFilter || undefined,
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

  const handleRefresh = () => { reloadSummary(); reloadResults(); reloadCoverage(); };

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
        const nodeId = row.original.node_id;
        if (!nodeId) return <span className="text-sm text-text-muted">-</span>;
        const node = nodes.find((n) => n.id === nodeId);
        return (
          <Link
            to={`/nodes/${nodeId}`}
            className="text-sm text-foreground hover:text-link hover:underline focus:outline-none focus-visible:underline"
          >
            {node?.hostname || nodeId}
          </Link>
        );
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
        if (!sev) return <span className="text-text-muted">-</span>;
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
        return d ? <ExpandableCode label="View details" content={d} /> : <span className="text-text-muted">-</span>;
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
          value={complianceScore !== null ? `${complianceScore}%` : '-'}
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

      <Panel padding="md" eyebrow="COVERAGE TRUTH" title="Supported evidence paths">
        <CoverageTruthList
          rows={coverageMatrix?.rows ?? []}
          loading={coverageLoading}
          error={coverageError}
          unavailable={coverageUnavailable}
          generatedAt={coverageMatrix?.generated_at}
        />
      </Panel>

      <Panel padding="md" eyebrow="FILTERS" title="Refine">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
          <FilterSelect label="Tenant" value={selectedTenant ?? ''}
            onChange={(v) => { setSelectedTenant(v || undefined); setSelectedNode(undefined); setOffset(0); }}
            options={[{ label: 'Current tenant', value: '' }, ...tenants.map((t) => ({ label: t.name, value: t.id }))]}
          />
          <FilterSelect label="Node" value={selectedNode ?? ''}
            onChange={(v) => { setSelectedNode(v || undefined); setOffset(0); }}
            options={[{ label: 'All nodes', value: '' }, ...nodes.map((n) => ({ label: n.hostname, value: n.id }))]}
            disabled={!effectiveTenantId}
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
          <FilterSelect
            label="Framework"
            value={frameworkFilter}
            onChange={(v) => {
              setFrameworkFilter(v);
              setOffset(0);
            }}
            options={[
              { label: 'All frameworks', value: '' },
              ...availableFrameworks.map((f) => ({ label: f, value: f })),
            ]}
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
        <Panel padding="md" eyebrow="TRENDS / 30 DAYS" title="Compliance trend" loading={trendsLoading}>
          <div className="h-56">
            <Chart kind="line" data={trendChartData} ariaLabel="Compliance trend" />
          </div>
        </Panel>
      )}

      <ControlPosturePanel
        framework={postureFramework}
        onFrameworkChange={setPostureFramework}
        posture={postureData}
        loading={postureLoading}
        error={postureError}
        tenantSelected={Boolean(effectiveTenantId)}
      />

      <Panel padding="sm" tone="inset" eyebrow={`RESULTS / ${results.length} of ${pagination.total}`} title="Compliance results">
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
  const { currentTenantId } = useTenant();
  const { data: nodes } = useNodes({
    tenantId: currentTenantId ?? undefined,
    limit: 1000,
  });

  // Policy list state
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [policiesLoading, setPoliciesLoading] = useState(false);
  const [policiesLoaded, setPoliciesLoaded] = useState(false);
  const [policiesLoadError, setPoliciesLoadError] = useState<string | null>(null);
  const [policyDeleteState, setPolicyDeleteState] = useState<Record<string, InlineActionState>>({});
  const [pendingPolicyDelete, setPendingPolicyDelete] = useState<PendingPolicyDelete | null>(null);
  const [tenantFilter, setTenantFilter] = useState('');

  // Create policy form
  const [showCreate, setShowCreate] = useState(false);
  const [createName, setCreateName] = useState('');
  const [createDesc, setCreateDesc] = useState('');
  const [createRuleType, setCreateRuleType] = useState('port_check');
  const [createTenantId, setCreateTenantId] = useState('');
  const [createScope, setCreateScope] = useState<ScopePickerValue>({ scope_type: 'tenant' });
  const [createEnabled, setCreateEnabled] = useState(true);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    if (!createTenantId && currentTenantId) {
      setCreateTenantId(currentTenantId);
    }
  }, [createTenantId, currentTenantId]);

  useEffect(() => {
    setCreateScope((previous) => {
      if (previous.scope_type === 'tenant' || previous.scope_type === 'label_selector') {
        return previous;
      }
      return { scope_type: previous.scope_type };
    });
  }, [createTenantId]);

  useEffect(() => {
    setPolicies([]);
    setPoliciesLoaded(false);
    setPoliciesLoadError(null);
  }, [currentTenantId, tenantFilter]);

  // Expanded policy (versions + create version)
  const [expandedPolicyId, setExpandedPolicyId] = useState<string | null>(null);
  const [versionsMap, setVersionsMap] = useState<Record<string, PolicyVersion[]>>({});
  const [versionErrorsMap, setVersionErrorsMap] = useState<Record<string, string>>({});
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
    const tenantForList = tenantFilter || currentTenantId;
    if (!tenantForList) {
      setPolicies([]);
      setPoliciesLoaded(true);
      setPoliciesLoadError(null);
      return;
    }
    setPoliciesLoading(true);
    setPoliciesLoadError(null);
    try {
      const res = await api.listPolicies({ tenant_id: tenantForList, limit: 100 });
      setPolicies(res.data);
      setPoliciesLoaded(true);
      setPoliciesLoadError(null);
    } catch (err) {
      const message = errorMessage(err, 'Failed to load policies');
      setPolicies([]);
      setPoliciesLoaded(true);
      setPoliciesLoadError(message);
      showToast(message, 'error');
    } finally {
      setPoliciesLoading(false);
    }
  };

  const loadVersions = async (policyId: string) => {
    setVersionsLoadingId(policyId);
    setVersionErrorsMap((prev) => {
      const next = { ...prev };
      delete next[policyId];
      return next;
    });
    try {
      const res = await api.listPolicyVersions(policyId);
      setVersionsMap((prev) => ({ ...prev, [policyId]: res.data }));
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load versions';
      setVersionErrorsMap((prev) => ({ ...prev, [policyId]: message }));
      showToast(message, 'error');
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
    if (!createTenantId) { showToast('Tenant is required', 'error'); return; }
    const scopedAssignment = buildScopedAssignmentPayload(createTenantId, createScope);
    if (!scopedAssignment.payload) {
      showToast(scopedAssignment.error ?? 'Assignment scope is invalid', 'error');
      return;
    }
    setCreating(true);
    try {
      const policy = await api.createPolicy({
        name: createName.trim(),
        description: createDesc.trim() || undefined,
        rule_type: createRuleType,
        tenant_id: createTenantId,
        enabled: createEnabled,
      });
      try {
        await api.createPolicyAssignment(policy.id, scopedAssignment.payload);
        showToast(
          `Policy "${policy.name}" created and assigned to ${describeAssignmentScope(scopedAssignment.payload)}`,
          'success',
        );
      } catch (assignmentErr) {
        showToast(
          assignmentErr instanceof Error
            ? `Policy created, but assignment failed: ${assignmentErr.message}`
            : 'Policy created, but assignment failed',
          'error',
        );
      }
      setCreateName(''); setCreateDesc(''); setCreateRuleType('port_check');
      setCreateScope({ scope_type: 'tenant' });
      setCreateTenantId(currentTenantId ?? ''); setShowCreate(false);
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
    setPendingPolicyDelete({ policy });
  };

  const runDeletePolicy = async (policy: Policy) => {
    const key = policyActionKey(policy);
    setPolicyDeleteState((state) => ({ ...state, [key]: { busy: true } }));
    try {
      await api.deletePolicy(policy.id);
      setPolicyDeleteState((state) => {
        const next = { ...state };
        delete next[key];
        return next;
      });
      setPendingPolicyDelete(null);
      showToast(`Policy "${policy.name}" deleted`, 'success');
      await loadPolicies();
    } catch (err) {
      const message = `Failed to delete policy ${policy.name}: ${errorMessage(err, 'delete failed')}`;
      setPolicyDeleteState((state) => ({ ...state, [key]: { busy: false, message, tone: 'critical' } }));
      setPendingPolicyDelete((current) =>
        current?.policy.id === policy.id ? { ...current, error: message } : current,
      );
      showToast(message, 'error');
    }
  };

  const confirmPolicyDelete = async () => {
    if (!pendingPolicyDelete) return;
    await runDeletePolicy(pendingPolicyDelete.policy);
  };

  const pendingDeleteBusy = pendingPolicyDelete
    ? Boolean(policyDeleteState[policyActionKey(pendingPolicyDelete.policy)]?.busy)
    : false;

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
      if (res.metadata?.no_policies_assigned) {
        showToast('No policies assigned to this node - assign policies before scanning.', 'error');
      } else {
        showToast(`Evaluation complete - ${res.results.length} result(s)`, 'success');
      }
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
            <option value="">Select node...</option>
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
            {evaluating ? 'Evaluating...' : 'Run evaluation'}
          </Button>
        </div>

        {evalResults !== null && evalResults.length === 0 && (
          <div className="mt-3">
            <EmptyState
              title="No policies assigned to this node"
              description="Assign CIS-mapped or custom policies to this node before running an evaluation. Until policies are assigned, scans return no results; synthetic placeholders are no longer fabricated."
            />
          </div>
        )}
        {evalResults !== null && evalResults.length > 0 && (
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
                        ) : <span className="text-text-muted">-</span>}
                      </td>
                      <td className="max-w-xs px-3 py-2">
                        {r.details ? (
                          <ExpandableCode label="Details" content={r.details} />
                        ) : <span className="text-text-muted">-</span>}
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
              <option value="">Current tenant</option>
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
        {policiesLoadError ? (
          <Alert
            variant="critical"
            title="Compliance policies unavailable"
            actions={
              <Button type="button" variant="secondary" size="sm" onClick={() => void loadPolicies()} disabled={policiesLoading}>
                Retry
              </Button>
            }
          >
            {policiesLoadError}
          </Alert>
        ) : null}

        {/* Create policy form */}
        {showCreate && (
          <div className="rounded-md border border-brand-500/30 bg-brand-500/5 p-4">
            <p className="mb-3 font-mono text-[0.65rem] uppercase tracking-wider text-brand-400">New tenant policy</p>
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
              <SelectField id="cp-tenant" label="Tenant" value={createTenantId} onChange={(e) => setCreateTenantId(e.target.value)}>
                <option value="" disabled>Select tenant</option>
                {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
              </SelectField>
            </div>
            <div className="mt-3">
              <ScopePicker
                tenantId={createTenantId}
                value={createScope}
                onChange={setCreateScope}
                disabled={creating || !createTenantId}
                idPrefix="compliance-policy-create"
              />
            </div>
            <label className="mt-3 flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
              <input type="checkbox" checked={createEnabled} onChange={(e) => setCreateEnabled(e.target.checked)} className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer" />
              Enable immediately
            </label>
            <div className="mt-3 flex gap-2">
              <Button variant="primary" size="sm" onClick={handleCreate} disabled={creating || !createTenantId}>
                {creating ? 'Creating...' : 'Create policy'}
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
          <p className="py-4 text-center text-sm text-text-muted">Loading policies...</p>
        )}

        {policiesLoaded && policiesLoadError && policies.length === 0 && (
          <EmptyState
            title="Compliance policies unavailable"
            description="Retry after the policy inventory request succeeds."
            icon={<FileText />}
          />
        )}

        {policiesLoaded && !policiesLoadError && policies.length === 0 && (
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
            versionsError={versionErrorsMap[policy.id] ?? null}
            deleteStatus={policyDeleteState[policyActionKey(policy)]}
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

        <ConfirmModal
          open={Boolean(pendingPolicyDelete)}
          title={
            pendingPolicyDelete
              ? `Delete compliance policy ${pendingPolicyDelete.policy.name}?`
              : 'Delete compliance policy?'
          }
          body="This removes the policy from future compliance evaluation and assignment workflows. Existing scan results, audit history, and reports remain available."
          confirmLabel="Delete policy"
          cancelLabel="Cancel"
          confirmDisabled={pendingDeleteBusy}
          cancelDisabled={pendingDeleteBusy}
          variant="danger"
          onConfirm={() => void confirmPolicyDelete()}
          onCancel={() => setPendingPolicyDelete(null)}
        >
          {pendingPolicyDelete?.error ? (
            <Alert variant="critical" title="Policy deletion failed">
              {pendingPolicyDelete.error}
            </Alert>
          ) : null}
        </ConfirmModal>
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
  versionsError?: string | null;
  deleteStatus?: InlineActionState;
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
  versionsError,
  deleteStatus,
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
  const api = useApiClient();
  const { showToast } = useToast();
  const tenantName = tenants.find((t) => t.id === policy.tenant_id)?.name;
  const policyTenantId = policy.tenant_id ?? '';
  const [assignments, setAssignments] = useState<PolicyAssignment[]>([]);
  const [assignmentsLoading, setAssignmentsLoading] = useState(false);
  const [assignmentsError, setAssignmentsError] = useState<string | null>(null);
  const [assignmentScope, setAssignmentScope] = useState<ScopePickerValue>({ scope_type: 'tenant' });
  const [creatingAssignment, setCreatingAssignment] = useState(false);
  const [pendingAssignmentDelete, setPendingAssignmentDelete] = useState<PendingAssignmentDelete | null>(null);
  const [deletingAssignment, setDeletingAssignment] = useState(false);

  const loadAssignments = useCallback(async () => {
    setAssignmentsLoading(true);
    setAssignmentsError(null);
    try {
      const response = await api.listPolicyAssignments(policy.id);
      setAssignments(response.items ?? []);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load policy assignments';
      setAssignments([]);
      setAssignmentsError(message);
      showToast(message, 'error');
    } finally {
      setAssignmentsLoading(false);
    }
  }, [api, policy.id, showToast]);

  useEffect(() => {
    if (expanded) {
      void loadAssignments();
    }
  }, [expanded, loadAssignments]);

  const handleCreateAssignment = async () => {
    const scopedAssignment = buildScopedAssignmentPayload(policyTenantId, assignmentScope);
    if (!scopedAssignment.payload) {
      showToast(scopedAssignment.error ?? 'Assignment scope is invalid', 'error');
      return;
    }
    setCreatingAssignment(true);
    try {
      await api.createPolicyAssignment(policy.id, scopedAssignment.payload);
      setAssignmentScope({ scope_type: 'tenant' });
      await loadAssignments();
      setAssignmentsError(null);
      showToast(`Assignment added for ${describeAssignmentScope(scopedAssignment.payload)}`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to add assignment', 'error');
    } finally {
      setCreatingAssignment(false);
    }
  };

  const handleConfirmDeleteAssignment = async () => {
    if (!pendingAssignmentDelete) {
      return;
    }
    setDeletingAssignment(true);
    setPendingAssignmentDelete((current) => current ? { assignment: current.assignment } : current);
    try {
      await api.deletePolicyAssignment(policy.id, pendingAssignmentDelete.assignment.id);
      setAssignments((current) => current.filter((assignment) => assignment.id !== pendingAssignmentDelete.assignment.id));
      setPendingAssignmentDelete(null);
      showToast('Assignment removed', 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to remove assignment';
      setPendingAssignmentDelete((current) => current ? { ...current, error: message } : current);
      showToast(message, 'error');
    } finally {
      setDeletingAssignment(false);
    }
  };

  return (
    <div className="rounded-md border border-border-subtle bg-surface">
      {/* Header row */}
      <div className="flex items-center gap-3 px-4 py-3">
        <button
          type="button"
          onClick={onToggleExpand}
          className="shrink-0 text-text-muted hover:text-foreground"
          aria-label={`${expanded ? 'Collapse' : 'Expand'} compliance policy ${policy.name}`}
        >
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
        <div className="flex shrink-0 flex-col items-end gap-1">
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-xs"
              onClick={onToggleEnabled}
              disabled={deleteStatus?.busy}
              aria-label={`${policy.enabled ? 'Disable' : 'Enable'} compliance policy ${policy.name}`}
            >
              {policy.enabled ? 'Disable' : 'Enable'}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-xs text-state-critical hover:text-state-critical"
              loading={deleteStatus?.busy}
              onClick={onDelete}
              aria-label={`Delete compliance policy ${policy.name}`}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
          {deleteStatus?.message ? (
            <p className={`max-w-[18rem] text-right text-xs ${toneText(deleteStatus.tone)}`}>
              {deleteStatus.message}
            </p>
          ) : null}
        </div>
      </div>

      {/* Expanded: versions + create version */}
      {expanded && (
        <div className="border-t border-border-subtle px-4 py-3 flex flex-col gap-3">
          <div className="flex flex-col gap-3 rounded-md border border-border-subtle bg-elevated px-3 py-3">
            <div className="flex items-center justify-between gap-3">
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Assignments</p>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs"
                onClick={() => void loadAssignments()}
                disabled={assignmentsLoading}
              >
                <RefreshCw className="h-3 w-3" />
              </Button>
            </div>

            {assignmentsLoading ? <p className="text-xs text-text-muted">Loading assignments...</p> : null}
            {assignmentsError ? (
              <p className="text-xs text-state-critical" role="alert">
                Policy assignments unavailable: {assignmentsError}
              </p>
            ) : null}
            {!assignmentsLoading && !assignmentsError && assignments.length === 0 ? (
              <p className="text-xs text-text-muted">No assignments.</p>
            ) : null}
            {!assignmentsLoading && assignments.length > 0 ? (
              <div className="flex flex-col gap-2">
                {assignments.map((assignment) => (
                  <div
                    key={assignment.id}
                    className="flex items-center justify-between gap-3 rounded-md border border-border-subtle bg-surface px-3 py-2"
                  >
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-foreground">{describeAssignmentScope(assignment)}</p>
                      <p className="font-mono text-[0.65rem] text-text-muted">
                        {formatDate(assignment.assigned_at)}
                      </p>
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-state-critical hover:text-state-critical"
                      onClick={() => setPendingAssignmentDelete({ assignment })}
                      aria-label={`Remove policy assignment ${describeAssignmentScope(assignment)}`}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}
              </div>
            ) : null}

            <div className="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
              <ScopePicker
                tenantId={policyTenantId}
                value={assignmentScope}
                onChange={setAssignmentScope}
                disabled={creatingAssignment || !policyTenantId}
                idPrefix={`policy-${policy.id}`}
              />
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => void handleCreateAssignment()}
                disabled={creatingAssignment || !policyTenantId}
              >
                {creatingAssignment ? 'Adding...' : 'Add assignment'}
              </Button>
            </div>
          </div>

          <div className="flex items-center justify-between">
            <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Versions</p>
            <Button type="button" variant="secondary" size="sm" className="h-7 gap-1 px-2 text-xs" onClick={onOpenCreateVersion}>
              <Plus className="h-3 w-3" /> New version
            </Button>
          </div>

          {versionsLoading && <p className="text-xs text-text-muted">Loading versions...</p>}

          {versionsError ? (
            <p className="text-xs text-state-critical" role="alert">
              Policy versions unavailable: {versionsError}
            </p>
          ) : null}

          {!versionsError && versions !== null && versions.length === 0 && (
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
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-brand-400">New version / rule definition (JSON)</p>
              <textarea
                className="flex min-h-[120px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
                value={versionDef}
                onChange={(e) => onVersionDefChange(e.target.value)}
                rows={6}
                placeholder='{"port": 22, "protocol": "tcp", "expected_state": "open"}'
              />
              <div className="flex gap-2">
                <Button variant="primary" size="sm" onClick={onCreateVersion} disabled={creatingVersion}>
                  {creatingVersion ? 'Creating...' : 'Create version'}
                </Button>
                <Button variant="ghost" size="sm" onClick={onCloseCreateVersion}>Cancel</Button>
              </div>
            </div>
          )}
        </div>
      )}
      <ConfirmModal
        open={Boolean(pendingAssignmentDelete)}
        title="Remove policy assignment"
        body={
          pendingAssignmentDelete
            ? `Remove ${describeAssignmentScope(pendingAssignmentDelete.assignment)} from ${policy.name}? Existing scan results and audit history remain available.`
            : undefined
        }
        confirmLabel={deletingAssignment ? 'Removing...' : 'Remove assignment'}
        cancelLabel="Cancel"
        confirmDisabled={deletingAssignment}
        cancelDisabled={deletingAssignment}
        variant="danger"
        onConfirm={() => void handleConfirmDeleteAssignment()}
        onCancel={() => {
          if (!deletingAssignment) {
            setPendingAssignmentDelete(null);
          }
        }}
      >
        {pendingAssignmentDelete?.error ? (
          <p className="text-sm text-state-critical" role="alert">
            {pendingAssignmentDelete.error}
          </p>
        ) : null}
      </ConfirmModal>
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

function errorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message.trim()) return err.message;
  if (typeof err === 'string' && err.trim()) return err;
  return fallback;
}

function policyActionKey(policy: Policy): string {
  return `policy:${policy.id}`;
}

function toneText(tone?: StateTone): string {
  switch (tone) {
    case 'critical':
      return 'text-state-critical';
    case 'warning':
      return 'text-state-warning';
    case 'healthy':
      return 'text-state-healthy';
    default:
      return 'text-text-muted';
  }
}
