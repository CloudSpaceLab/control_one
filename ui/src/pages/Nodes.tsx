import { FormEvent, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { SectionHeader, Panel, KpiTile, EmptyState, DataTable, SelectField, StatusTag } from '../components/kit';
import type { StateTone } from '../components/kit/types';
import { Button } from '@/components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { ConfirmModal } from '../components/ConfirmModal';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import type {
  AtRiskFleetResponse,
  NodeHealthRiskLevel,
  NodeHealthScore,
  RegisterNodePayload,
  UpdateNodePayload,
} from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';
import { AlertTriangle, ChevronDown, ChevronUp, Info, RefreshCw, Server } from 'lucide-react';

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

// Map a predictive risk_level → StatusTag tone. We deliberately use the
// kit's narrow StateTone palette (no "brand"); calibrating maps to
// "unknown" because we explicitly do not yet know the node's health.
function riskTone(risk: NodeHealthRiskLevel): StateTone {
  switch (risk) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
      return 'healthy';
    case 'calibrating':
    default:
      return 'unknown';
  }
}

function riskLabel(risk: NodeHealthRiskLevel, score: number, calibratingSamples?: number): string {
  if (risk === 'calibrating') {
    const n = typeof calibratingSamples === 'number' ? calibratingSamples : 0;
    return `Calibrating (${n}/24 samples)`;
  }
  return `${risk.charAt(0).toUpperCase()}${risk.slice(1)} · ${score}`;
}

// HealthGauge renders a small SVG arc gauge for a 0..100 score. Pure
// presentational — color follows the same risk-tone mapping above.
function HealthGauge({ score, risk }: { score: number; risk: NodeHealthRiskLevel }): JSX.Element {
  const radius = 48;
  const circumference = Math.PI * radius;
  const clamped = Math.max(0, Math.min(100, score));
  const offset = circumference * (1 - clamped / 100);
  const strokeColor =
    risk === 'critical'
      ? 'var(--state-critical, #ef4444)'
      : risk === 'high'
        ? 'var(--state-degraded, #f97316)'
        : risk === 'medium'
          ? 'var(--state-warning, #eab308)'
          : risk === 'low'
            ? 'var(--state-healthy, #22c55e)'
            : 'var(--text-muted, #6b7280)';
  return (
    <svg width={120} height={70} viewBox="0 0 120 70" aria-label={`Health score ${clamped}`}>
      <path
        d={`M 12 60 A ${radius} ${radius} 0 0 1 108 60`}
        fill="none"
        stroke="currentColor"
        strokeOpacity={0.15}
        strokeWidth={10}
        strokeLinecap="round"
      />
      <path
        d={`M 12 60 A ${radius} ${radius} 0 0 1 108 60`}
        fill="none"
        stroke={strokeColor}
        strokeWidth={10}
        strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={offset}
      />
      <text
        x={60}
        y={56}
        textAnchor="middle"
        className="font-display fill-current text-foreground"
        style={{ fontSize: '1.25rem', fontWeight: 600 }}
      >
        {risk === 'calibrating' ? '—' : clamped}
      </text>
    </svg>
  );
}

// componentBreakdownEntries pulls the per-signal penalties out of the
// nested `breakdown` map the server emits. Falls back to top-level
// numeric entries for older schemas.
function componentBreakdownEntries(components: Record<string, unknown>): Array<{ key: string; penalty: number }> {
  const breakdown = components['breakdown'];
  if (breakdown && typeof breakdown === 'object') {
    return Object.entries(breakdown as Record<string, unknown>)
      .filter(([, v]) => typeof v === 'number')
      .map(([key, v]) => ({ key, penalty: v as number }))
      .sort((a, b) => a.penalty - b.penalty);
  }
  return Object.entries(components)
    .filter(([, v]) => typeof v === 'number')
    .map(([key, v]) => ({ key, penalty: v as number }))
    .sort((a, b) => a.penalty - b.penalty);
}

export function Nodes(): JSX.Element {
  const api = useApiClient();
  const navigate = useNavigate();
  const { data: tenants, reload: reloadTenants } = useTenants();
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [hostnameFilter, setHostnameFilter] = useState('');
  const [limit] = useState(12);
  const [offset, setOffset] = useState(0);

  const { data: nodes, loading, error, pagination, reload: reloadNodes } = useNodes({
    tenantId: selectedTenant,
    hostnamePrefix: hostnameFilter.trim() || undefined,
    limit,
    offset,
  });

  // Registration form state — simplified: only token + tenant required.
  // OS, architecture, IP, and hostname are auto-reported by the agent.
  const [formTenantId, setFormTenantId] = useState('');
  const [formTenantName, setFormTenantName] = useState('');
  const [hostnameHint, setHostnameHint] = useState('');
  const [bootstrapToken, setBootstrapToken] = useState('');
  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [registering, setRegistering] = useState(false);

  // Detail panel state
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [detailHostname, setDetailHostname] = useState('');
  const [detailOs, setDetailOs] = useState('');
  const [detailArch, setDetailArch] = useState('');
  const [detailPublicIp, setDetailPublicIp] = useState('');
  const [updating, setUpdating] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [agentUpdateNodeId, setAgentUpdateNodeId] = useState<string | null>(null);
  const [agentUpdating, setAgentUpdating] = useState(false);

  // Predictive server downtime — Use Case 5 (PR 31).
  const [healthScores, setHealthScores] = useState<Record<string, NodeHealthScore | null>>({});
  const [atRiskFleet, setAtRiskFleet] = useState<AtRiskFleetResponse | null>(null);
  const [atRiskCollapsed, setAtRiskCollapsed] = useState(false);
  const [detailHealth, setDetailHealth] = useState<NodeHealthScore | null>(null);

  // Load at-risk fleet roll-up whenever the tenant filter changes.
  useEffect(() => {
    let cancelled = false;
    const tenantParam = selectedTenant ?? undefined;
    api
      .listAtRiskNodes(tenantParam)
      .then((resp) => {
        if (!cancelled) {
          setAtRiskFleet(resp);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAtRiskFleet(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [api, selectedTenant, nodes]);

  // Bulk-fetch per-node scores for the visible page. We tolerate per-row
  // errors silently — the row simply renders as "unknown".
  useEffect(() => {
    let cancelled = false;
    const ids = nodes.map((n) => n.id);
    Promise.all(
      ids.map((id) =>
        api
          .getNodeHealth(id)
          .then((score) => [id, score] as const)
          .catch(() => [id, null] as const),
      ),
    ).then((entries) => {
      if (cancelled) return;
      const next: Record<string, NodeHealthScore | null> = {};
      for (const [id, score] of entries) {
        next[id] = score;
      }
      setHealthScores(next);
    });
    return () => {
      cancelled = true;
    };
  }, [api, nodes]);

  // Load the slide-over health detail whenever a node is selected.
  useEffect(() => {
    if (!selectedNodeId) {
      setDetailHealth(null);
      return;
    }
    let cancelled = false;
    api
      .getNodeHealth(selectedNodeId)
      .then((score) => {
        if (!cancelled) setDetailHealth(score);
      })
      .catch(() => {
        if (!cancelled) setDetailHealth(null);
      });
    return () => {
      cancelled = true;
    };
  }, [api, selectedNodeId]);

  const tenantOptions = useMemo(() => tenants, [tenants]);
  const tenantNames = useMemo(() => {
    const entries = new Map<string, string>();
    for (const tenant of tenants) {
      entries.set(tenant.id, tenant.name);
    }
    return entries;
  }, [tenants]);

  const selectedNode = useMemo(
    () => nodes.find((node) => node.id === selectedNodeId) ?? null,
    [nodes, selectedNodeId],
  );

  const summary = useMemo(() => {
    return {
      total: pagination.total,
      filtered: nodes.length,
    };
  }, [pagination.total, nodes.length]);

  const handleRegisterNode = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedToken = bootstrapToken.trim();
    const trimmedTenantName = formTenantName.trim();

    if (!trimmedToken) {
      showError('Bootstrap token is required');
      return;
    }
    if (!formTenantId && !trimmedTenantName) {
      showError('Select an existing tenant or provide a new tenant name');
      return;
    }

    setRegistering(true);
    resetFeedback();

    try {
      const payload: RegisterNodePayload = {
        bootstrap_token: trimmedToken,
      };
      if (formTenantId) {
        payload.tenant_id = formTenantId;
      } else if (trimmedTenantName) {
        payload.tenant_name = trimmedTenantName;
      }
      if (hostnameHint.trim()) {
        payload.hostname = hostnameHint.trim();
      }

      const response = await api.registerNode(payload);
      const successMessage = `Node ${response.node_id} registered for tenant ${response.tenant_id}.`;
      showSuccess(successMessage);
      showToast(successMessage, 'success');
      setHostnameHint('');
      setBootstrapToken('');
      setFormTenantName('');
      setSelectedTenant(response.tenant_id);
      setFormTenantId(response.tenant_id);
      reloadNodes();
      reloadTenants();
    } catch (err) {
      if (err instanceof Error) {
        showError(err.message);
        showToast(err.message, 'error');
      } else {
        const fallback = 'Failed to register node';
        showError(fallback);
        showToast(fallback, 'error');
      }
    } finally {
      setRegistering(false);
    }
  };

  const openNodeDetails = (nodeId: string) => {
    setSelectedNodeId((current) => (current === nodeId ? null : nodeId));
    const node = nodes.find((n) => n.id === nodeId);
    setDetailHostname(node?.hostname ?? '');
    setDetailOs(node?.os ?? '');
    setDetailArch(node?.arch ?? '');
    setDetailPublicIp(node?.public_ip ?? '');
  };

  const handleUpdateNode = async () => {
    if (!selectedNode) {
      return;
    }
    const payload: UpdateNodePayload = {};
    const trimmedHostname = detailHostname.trim();
    const trimmedOs = detailOs.trim();
    const trimmedArch = detailArch.trim();
    const trimmedPublicIp = detailPublicIp.trim();

    if (trimmedHostname && trimmedHostname !== selectedNode.hostname) {
      payload.hostname = trimmedHostname;
    }
    if (trimmedOs !== (selectedNode.os ?? '')) {
      payload.os = trimmedOs;
    }
    if (trimmedArch !== (selectedNode.arch ?? '')) {
      payload.arch = trimmedArch;
    }
    if (trimmedPublicIp !== (selectedNode.public_ip ?? '')) {
      payload.public_ip = trimmedPublicIp;
    }

    if (
      !payload.hostname &&
      payload.os === undefined &&
      payload.arch === undefined &&
      payload.public_ip === undefined
    ) {
      showToast('No changes to save.', 'info');
      return;
    }

    setUpdating(true);
    try {
      await api.updateNode(selectedNode.id, payload);
      showToast('Node updated.', 'success');
      await reloadNodes();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to update node.';
      showToast(message, 'error');
    } finally {
      setUpdating(false);
    }
  };

  const handleDeleteNode = async () => {
    if (!selectedNode) {
      return;
    }
    const confirmed = window.confirm(`Delete node "${selectedNode.hostname}"?`);
    if (!confirmed) {
      return;
    }
    setDeleting(true);
    try {
      await api.deleteNode(selectedNode.id);
      showToast('Node deleted.', 'success');
      setSelectedNodeId(null);
      await reloadNodes();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to delete node.';
      showToast(message, 'error');
    } finally {
      setDeleting(false);
    }
  };

  const handleAgentUpdate = async () => {
    if (!agentUpdateNodeId) return;
    setAgentUpdating(true);
    try {
      await api.updateAgent(agentUpdateNodeId);
      showToast('Agent update queued. Will apply on next heartbeat.', 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to queue agent update.';
      showToast(message, 'error');
    } finally {
      setAgentUpdating(false);
      setAgentUpdateNodeId(null);
    }
  };

  type NodeRow = (typeof nodes)[number];

  const nodeColumns: ColumnDef<NodeRow>[] = [
    {
      header: 'Hostname',
      accessorKey: 'hostname',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.hostname}</span>,
    },
    {
      header: 'Tenant',
      accessorKey: 'tenant_id',
      cell: ({ row }) => (
        <span className="text-text-secondary">
          {tenantNames.get(row.original.tenant_id) ?? row.original.tenant_id}
        </span>
      ),
    },
    {
      header: 'OS',
      accessorKey: 'os',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.os ?? '—'}</span>,
    },
    {
      header: 'Public IP',
      accessorKey: 'public_ip',
      cell: ({ row }) => (
        <code className="font-mono text-xs text-text-secondary">{row.original.public_ip ?? '—'}</code>
      ),
    },
    {
      header: 'Agent version',
      accessorKey: 'agent_version',
      cell: ({ row }) => {
        const v = row.original.agent_version;
        if (!v) return <span className="text-text-muted">—</span>;
        return <StatusTag tone="healthy">{v}</StatusTag>;
      },
    },
    {
      id: 'health',
      header: 'Health',
      cell: ({ row }) => {
        const score = healthScores[row.original.id];
        if (!score) {
          return <span className="text-text-muted">—</span>;
        }
        const cal =
          typeof score.components?.['calibrating_samples'] === 'number'
            ? (score.components['calibrating_samples'] as number)
            : undefined;
        return (
          <StatusTag tone={riskTone(score.risk_level)}>
            {riskLabel(score.risk_level, score.score, cal)}
          </StatusTag>
        );
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex items-center gap-1.5">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={() => navigate(`/nodes/${row.original.id}`)}
          >
            Open
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            title="Queue agent self-update"
            onClick={() => setAgentUpdateNodeId(row.original.id)}
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="INFRASTRUCTURE · NODES"
        title="Nodes"
        description="Connected agents reporting into the control plane."
      />

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <KpiTile
          label="Total nodes"
          value={summary.total}
          tone="brand"
          hint={selectedTenant ? 'Filtered by tenant' : 'All tenants'}
        />
        <KpiTile
          label="Visible"
          value={summary.filtered}
          hint="matching filters"
        />
      </div>

      {/* At-Risk Fleet roll-up — Use Case 5. Hide when zero risky nodes
          to keep the page calm. Collapsible because the list can grow
          large in noisy fleets. */}
      {atRiskFleet && atRiskFleet.total_count > 0 ? (
        <Panel
          padding="md"
          eyebrow="PREDICTIVE · AT-RISK FLEET"
          title={`${atRiskFleet.total_count} node${atRiskFleet.total_count === 1 ? '' : 's'} at risk`}
          toneAccent="brand"
          actions={
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setAtRiskCollapsed((c) => !c)}
            >
              {atRiskCollapsed ? <ChevronDown className="h-4 w-4" /> : <ChevronUp className="h-4 w-4" />}
              {atRiskCollapsed ? 'Expand' : 'Collapse'}
            </Button>
          }
        >
          <div className="flex flex-wrap items-center gap-3 text-sm">
            <StatusTag tone="critical" icon={<AlertTriangle className="h-3.5 w-3.5" />}>
              {atRiskFleet.critical} critical
            </StatusTag>
            <StatusTag tone="degraded">{atRiskFleet.high} high</StatusTag>
          </div>
          {!atRiskCollapsed ? (
            <ul className="mt-3 flex flex-col gap-1.5">
              {atRiskFleet.data.map((row) => (
                <li
                  key={row.node_id}
                  className="flex items-center justify-between gap-3 rounded-md border border-border-subtle bg-surface-2 px-3 py-2"
                >
                  <div className="flex flex-col">
                    <span className="font-medium text-foreground">{row.hostname}</span>
                    <code className="font-mono text-[0.65rem] text-text-muted">{row.node_id}</code>
                  </div>
                  <div className="flex items-center gap-2">
                    <StatusTag tone={riskTone(row.risk_level)}>
                      {riskLabel(row.risk_level, row.score)}
                    </StatusTag>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => openNodeDetails(row.node_id)}
                    >
                      View
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          ) : null}
        </Panel>
      ) : null}

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* LEFT: Register form */}
        <Panel padding="md" eyebrow="REGISTER" title="Register node" toneAccent="brand">
          {/* Info banner */}
          <div className="flex items-start gap-2 rounded-md border border-brand-500/20 bg-brand-500/5 px-3 py-2.5 text-sm text-text-secondary">
            <Info className="mt-0.5 h-4 w-4 shrink-0 text-brand-400" />
            <span>
              The Control One agent self-registers and automatically reports its hostname, OS,
              architecture, and IP address on first connection. Only a bootstrap token is required
              to create the node slot.
            </span>
          </div>

          <form onSubmit={handleRegisterNode} className="flex flex-col gap-3">
            <SelectField
              id="register-tenant"
              label="Existing tenant"
              value={formTenantId}
              onChange={(event) => setFormTenantId(event.target.value)}
              disabled={registering}
            >
              <option value="">— Select tenant —</option>
              {tenantOptions.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.name}
                </option>
              ))}
            </SelectField>
            <p className="text-xs text-text-muted -mt-2">
              Or provide a new tenant name below to auto-create one.
            </p>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="new-tenant-name">New tenant name</Label>
              <Input
                id="new-tenant-name"
                type="text"
                placeholder="e.g. Edge Cluster"
                value={formTenantName}
                onChange={(event) => setFormTenantName(event.target.value)}
                disabled={registering}
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="bootstrap-token">Bootstrap token</Label>
              <Input
                id="bootstrap-token"
                type="text"
                value={bootstrapToken}
                onChange={(event) => setBootstrapToken(event.target.value)}
                placeholder="control-one-bootstrap-token"
                disabled={registering}
                required
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="hostname-hint">
                Hostname hint{' '}
                <span className="text-text-muted font-normal">(optional)</span>
              </Label>
              <Input
                id="hostname-hint"
                type="text"
                value={hostnameHint}
                onChange={(event) => setHostnameHint(event.target.value)}
                placeholder="node-01.example.com"
                disabled={registering}
              />
              <p className="text-xs text-text-muted">
                Leave blank — the agent will self-report its real hostname on first connect.
              </p>
            </div>

            {formError ? (
              <p className="text-sm text-state-critical" role="alert">
                {formError}
              </p>
            ) : null}
            {formSuccess ? (
              <p className="text-sm text-state-healthy" role="status">
                {formSuccess}
              </p>
            ) : null}

            <div className="flex items-center gap-2 pt-2">
              <Button type="submit" variant="primary" disabled={registering}>
                {registering ? 'Registering…' : 'Register node'}
              </Button>
            </div>
          </form>
        </Panel>

        {/* RIGHT: Node list */}
        <Panel
          padding="md"
          eyebrow="NODES"
          title="Registered nodes"
          actions={
            <Button type="button" variant="secondary" size="sm" onClick={reloadNodes} disabled={loading}>
              {loading ? 'Refreshing…' : 'Refresh'}
            </Button>
          }
        >
          {/* Filter row */}
          <div className="flex flex-wrap items-center gap-3">
            <SelectField
              id="tenant-filter"
              value={selectedTenant ?? ''}
              onChange={(event) => {
                const value = event.target.value;
                setSelectedTenant(value === '' ? undefined : value);
                setOffset(0);
              }}
            >
              <option value="">All tenants</option>
              {tenantOptions.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.name}
                </option>
              ))}
            </SelectField>
            <Input
              id="hostname-filter"
              type="search"
              placeholder="Search hostname…"
              value={hostnameFilter}
              onChange={(event) => {
                setHostnameFilter(event.target.value);
                setOffset(0);
              }}
              className="h-9 flex-1"
            />
          </div>

          {error ? (
            <p className="text-sm text-state-critical" role="alert">
              Failed to load nodes: {error}
            </p>
          ) : null}

          <DataTable
            columns={nodeColumns}
            rows={nodes}
            loading={loading}
            rowKey={(row) => row.id}
            empty={
              <EmptyState
                title="No nodes"
                description="No nodes match the current filters."
                icon={<Server />}
              />
            }
          />

          <div className="flex items-center justify-between gap-4 pt-2 text-sm text-text-muted">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={pagination.prevOffset === null || pagination.prevOffset === undefined}
              onClick={() => setOffset(pagination.prevOffset ?? 0)}
            >
              ← Previous
            </Button>
            <span>
              Showing {nodes.length} of {pagination.total}
            </span>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
              onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
            >
              Next →
            </Button>
          </div>
        </Panel>
      </div>

      <ConfirmModal
        open={agentUpdateNodeId !== null}
        title="Queue agent self-update?"
        body="The node agent will download the latest binary and restart on its next heartbeat cycle."
        confirmLabel={agentUpdating ? 'Queuing…' : 'Update agent'}
        onConfirm={handleAgentUpdate}
        onCancel={() => setAgentUpdateNodeId(null)}
      />

      {/* Node detail aside panel */}
      {selectedNode ? (
        <>
          {/* Backdrop overlay */}
          <div
            className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm"
            onClick={() => setSelectedNodeId(null)}
          />
          <aside className="fixed inset-y-0 right-0 z-50 flex w-[min(560px,90vw)] flex-col gap-5 overflow-y-auto border-l border-border-subtle bg-surface p-6 shadow-2xl">
          <header className="flex items-start justify-between gap-4">
            <div>
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">NODE</p>
              <h3 className="mt-0.5 font-display text-lg font-semibold text-foreground">
                {selectedNode.hostname}
              </h3>
            </div>
            <Button variant="ghost" size="sm" onClick={() => setSelectedNodeId(null)}>
              ✕
            </Button>
          </header>

          <hr className="border-border-subtle" />

          {/* Predictive health — Use Case 5. Renders the arc gauge + a
              horizontal breakdown of per-signal penalties when scored.
              For "calibrating", we show "Calibrating (N/24 samples)" and
              never fake a numeric score. */}
          {detailHealth ? (
            <div className="flex flex-col gap-3">
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                Predictive health
              </p>
              <div className="flex items-center gap-4">
                <HealthGauge score={detailHealth.score} risk={detailHealth.risk_level} />
                <div className="flex flex-col gap-1">
                  <StatusTag tone={riskTone(detailHealth.risk_level)}>
                    {riskLabel(
                      detailHealth.risk_level,
                      detailHealth.score,
                      typeof detailHealth.components?.['calibrating_samples'] === 'number'
                        ? (detailHealth.components['calibrating_samples'] as number)
                        : undefined,
                    )}
                  </StatusTag>
                  {detailHealth.computed_at ? (
                    <span className="text-xs text-text-muted">
                      Updated {formatDate(detailHealth.computed_at)}
                    </span>
                  ) : null}
                </div>
              </div>

              {detailHealth.risk_level === 'calibrating' ? (
                <EmptyState
                  title="Calibrating health score"
                  description={`Need ${24 - (typeof detailHealth.components?.['calibrating_samples'] === 'number' ? (detailHealth.components['calibrating_samples'] as number) : 0)} more samples before we can score this node.`}
                />
              ) : (
                (() => {
                  const entries = componentBreakdownEntries(detailHealth.components);
                  if (entries.length === 0) {
                    return (
                      <p className="text-sm text-text-secondary">
                        No penalties applied — node is operating within all baselines.
                      </p>
                    );
                  }
                  const maxAbs = Math.max(...entries.map((e) => Math.abs(e.penalty)));
                  return (
                    <ul className="flex flex-col gap-1.5">
                      {entries.map(({ key, penalty }) => {
                        const widthPct = maxAbs > 0 ? (Math.abs(penalty) / maxAbs) * 100 : 0;
                        return (
                          <li key={key} className="flex flex-col gap-0.5">
                            <div className="flex items-center justify-between text-xs">
                              <span className="font-mono text-text-secondary">{key}</span>
                              <span className="font-medium text-state-critical">{penalty}</span>
                            </div>
                            <div className="h-1.5 w-full rounded-full bg-surface-2">
                              <div
                                className="h-full rounded-full bg-state-critical/70"
                                style={{ width: `${widthPct}%` }}
                              />
                            </div>
                          </li>
                        );
                      })}
                    </ul>
                  );
                })()
              )}
            </div>
          ) : null}

          <hr className="border-border-subtle" />

          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
            <div className="flex flex-col gap-0.5">
              <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Hostname</dt>
              <dd className="text-foreground">{selectedNode.hostname}</dd>
            </div>
            <div className="flex flex-col gap-0.5">
              <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Node ID</dt>
              <dd>
                <code className="font-mono text-xs text-text-secondary">{selectedNode.id}</code>
              </dd>
            </div>
            <div className="flex flex-col gap-0.5">
              <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Tenant</dt>
              <dd className="text-foreground">
                {tenantNames.get(selectedNode.tenant_id) ?? selectedNode.tenant_id}
              </dd>
            </div>
            <div className="flex flex-col gap-0.5">
              <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Created</dt>
              <dd className="text-foreground">{formatDate(selectedNode.created_at)}</dd>
            </div>
            <div className="flex flex-col gap-0.5">
              <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Updated</dt>
              <dd className="text-foreground">{formatDate(selectedNode.updated_at)}</dd>
            </div>
          </dl>

          <hr className="border-border-subtle" />

          {/* Override fields — agent-reported values can be corrected manually */}
          <div>
            <p className="mb-3 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
              Override agent-reported values
            </p>
            <div className="flex flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="detail-hostname">Hostname</Label>
                <Input
                  id="detail-hostname"
                  type="text"
                  value={detailHostname}
                  onChange={(event) => setDetailHostname(event.target.value)}
                />
              </div>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="detail-os">Operating system</Label>
                  <Input
                    id="detail-os"
                    type="text"
                    value={detailOs}
                    onChange={(event) => setDetailOs(event.target.value)}
                    placeholder="Ubuntu 24.04"
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="detail-arch">Architecture</Label>
                  <Input
                    id="detail-arch"
                    type="text"
                    value={detailArch}
                    onChange={(event) => setDetailArch(event.target.value)}
                    placeholder="x86_64"
                  />
                </div>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="detail-ip">Public IP</Label>
                <Input
                  id="detail-ip"
                  type="text"
                  value={detailPublicIp}
                  onChange={(event) => setDetailPublicIp(event.target.value)}
                  placeholder="203.0.113.10"
                />
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2 pt-2">
            <Button type="button" variant="primary" onClick={handleUpdateNode} disabled={updating}>
              {updating ? 'Saving…' : 'Save changes'}
            </Button>
            <Button type="button" variant="danger" onClick={handleDeleteNode} disabled={deleting}>
              {deleting ? 'Deleting…' : 'Delete'}
            </Button>
            <Button type="button" variant="ghost" onClick={() => setSelectedNodeId(null)}>
              Close
            </Button>
          </div>
        </aside>
        </>
      ) : null}
    </div>
  );
}
