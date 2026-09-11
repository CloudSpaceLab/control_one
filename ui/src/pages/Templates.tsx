import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { Template, ClusterRolloutWave, TemplateAssignment } from '../lib/api';
import { SectionHeader, Panel, KpiTile, EmptyState, StatusTag, DataTable, SelectField } from '../components/kit';
import { Button } from '@/components/ui/button';
import { ConfirmModal } from '../components/ConfirmModal';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import {
  ScopePicker,
  buildScopedAssignmentPayload,
  describeAssignmentScope,
  type ScopePickerValue,
} from '../components/compliance/ScopePicker';
import { useTemplates } from '../hooks/useTemplates';
import { useTemplateVersions } from '../hooks/useTemplateVersions';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { useTenants } from '../hooks/useTenants';
import { useTenant } from '../providers/TenantProvider';
import type { ColumnDef } from '@tanstack/react-table';
import type { StateTone } from '../components/kit';
import { FileText, Layers } from 'lucide-react';

type PageTab = 'templates' | 'rollouts';
import {
  DEFAULT_METADATA_SCHEMA,
  DEFAULT_TEMPLATE_BODY,
  parseJsonInput,
  parseTemplateLabels,
  templateStatus,
} from '../lib/templateUtils';

function formatDate(value?: string): string {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function templateStatusTone(tpl: Template): StateTone {
  const s = templateStatus(tpl);
  if (s === 'archived') return 'unknown';
  if (s === 'promoted') return 'healthy';
  return 'info';
}

export function Templates(): JSX.Element {
  // Page-level tab
  const [pageTab, setPageTab] = useState<PageTab>('templates');
  const api = useApiClient();
  const { showToast } = useToast();
  const { currentTenantId } = useTenant();
  const [limit] = useState(20);
  const [offset, setOffset] = useState(0);
  const [providerFilter, setProviderFilter] = useState('');
  const [nameFilter, setNameFilter] = useState('');
  const [includeArchived, setIncludeArchived] = useState(false);
  const templateOptions = {
    tenantId: currentTenantId ?? undefined,
    provider: providerFilter.trim() || undefined,
    namePrefix: nameFilter.trim() || undefined,
    includeArchived,
    limit,
    offset,
  };
  const { data: templates, pagination, loading, error, reload } = useTemplates(templateOptions);

  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null);

  useEffect(() => {
    if (templates.length === 0) {
      setSelectedTemplateId(null);
      return;
    }
    if (!selectedTemplateId || !templates.some((tpl) => tpl.id === selectedTemplateId)) {
      setSelectedTemplateId(templates[0].id);
    }
  }, [templates, selectedTemplateId]);

  const selectedTemplate = useMemo<Template | null>(() => {
    if (!selectedTemplateId) {
      return null;
    }
    return templates.find((tpl) => tpl.id === selectedTemplateId) ?? null;
  }, [templates, selectedTemplateId]);

  const [versionOffset, setVersionOffset] = useState(0);
  const versionLimit = 10;
  useEffect(() => {
    setVersionOffset(0);
  }, [selectedTemplate?.id]);

  const {
    data: versions,
    pagination: versionPagination,
    loading: versionsLoading,
    error: versionsError,
    reload: reloadVersions,
  } = useTemplateVersions({
    templateId: selectedTemplate?.id,
    limit: versionLimit,
    offset: versionOffset,
  });

  const templateForm = useFormFeedback();
  const versionForm = useFormFeedback();
  const [creatingTemplate, setCreatingTemplate] = useState(false);
  const [creatingVersion, setCreatingVersion] = useState(false);
  const [updatingTemplate, setUpdatingTemplate] = useState(false);
  const [templateName, setTemplateName] = useState('');
  const [templateProvider, setTemplateProvider] = useState('');
  const [templateDescription, setTemplateDescription] = useState('');
  const [templateLabels, setTemplateLabels] = useState('{}');
  const [templateTenantId, setTemplateTenantId] = useState('');

  const [versionBody, setVersionBody] = useState(DEFAULT_TEMPLATE_BODY);
  const [versionChecksum, setVersionChecksum] = useState('');
  const [versionMetadata, setVersionMetadata] = useState(DEFAULT_METADATA_SCHEMA);
  const [versionNotes, setVersionNotes] = useState('');

  const [templateAssignments, setTemplateAssignments] = useState<TemplateAssignment[]>([]);
  const [templateAssignmentsLoading, setTemplateAssignmentsLoading] = useState(false);
  const [templateAssignmentsError, setTemplateAssignmentsError] = useState<string | null>(null);
  const [templateAssignmentTenantId, setTemplateAssignmentTenantId] = useState('');
  const [templateAssignmentScope, setTemplateAssignmentScope] = useState<ScopePickerValue>({ scope_type: 'tenant' });
  const [creatingTemplateAssignment, setCreatingTemplateAssignment] = useState(false);
  const [pendingDeleteAssignment, setPendingDeleteAssignment] = useState<TemplateAssignment | null>(null);
  const [deletingTemplateAssignment, setDeletingTemplateAssignment] = useState(false);
  const [templateAssignmentActionError, setTemplateAssignmentActionError] = useState<string | null>(null);

  useEffect(() => {
    if (!templateTenantId && currentTenantId) {
      setTemplateTenantId(currentTenantId);
    }
  }, [currentTenantId, templateTenantId]);

  // Rollout waves state
  const { data: tenants } = useTenants();
  const [rolloutTenantId, setRolloutTenantId] = useState('');
  const [waves, setWaves] = useState<ClusterRolloutWave[]>([]);
  const [wavesLoading, setWavesLoading] = useState(false);
  const [wavesError, setWavesError] = useState<string | null>(null);
  const [waveActionId, setWaveActionId] = useState<string | null>(null);
  const [wavesReloadToken, setWavesReloadToken] = useState(0);
  const [promotingVersion, setPromotingVersion] = useState<number | null>(null);

  useEffect(() => {
    setTemplateAssignments([]);
    setTemplateAssignmentsError(null);
    setTemplateAssignmentActionError(null);
    setPendingDeleteAssignment(null);
    setTemplateAssignmentScope({ scope_type: 'tenant' });
    setTemplateAssignmentTenantId(selectedTemplate?.tenant_id ?? currentTenantId ?? '');
  }, [currentTenantId, selectedTemplate?.id, selectedTemplate?.tenant_id]);

  const loadTemplateAssignments = useCallback(async () => {
    if (!selectedTemplate) {
      setTemplateAssignments([]);
      setTemplateAssignmentsError(null);
      return;
    }
    setTemplateAssignmentsLoading(true);
    setTemplateAssignmentsError(null);
    try {
      const response = await api.listTemplateAssignments(selectedTemplate.id);
      setTemplateAssignments(response.items ?? []);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load template assignments';
      setTemplateAssignments([]);
      setTemplateAssignmentsError(message);
      showToast(message, 'error');
    } finally {
      setTemplateAssignmentsLoading(false);
    }
  }, [api, selectedTemplate, showToast]);

  useEffect(() => {
    void loadTemplateAssignments();
  }, [loadTemplateAssignments]);

  useEffect(() => {
    let cancelled = false;
    setWavesLoading(true);
    setWavesError(null);
    api
      .listClusterRolloutWaves({ tenantId: rolloutTenantId || undefined })
      .then((r) => {
        if (!cancelled) {
          setWaves(r.data ?? []);
          setWavesError(null);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setWaves([]);
          setWavesError(err instanceof Error ? err.message : 'Failed to load rollout waves');
        }
      })
      .finally(() => { if (!cancelled) setWavesLoading(false); });
    return () => { cancelled = true; };
  }, [api, rolloutTenantId, wavesReloadToken]);

  const handleWaveAction = async (id: string, action: 'paused' | 'running' | 'aborted') => {
    const actionLabel = action === 'running' ? 'resumed' : action;
    setWaveActionId(id);
    setWavesError(null);
    try {
      await api.updateClusterRolloutWave(id, { status: action });
      setWavesReloadToken((n) => n + 1);
      showToast(`Rollout wave ${actionLabel}`, 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to ${actionLabel} rollout wave`;
      setWavesError(message);
      showToast(message, 'error');
    } finally {
      setWaveActionId(null);
    }
  };

  const handleCreateTemplate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const name = templateName.trim();
    const provider = templateProvider.trim();
    if (!name) {
      templateForm.showError('Template name is required');
      return;
    }
    if (!provider) {
      templateForm.showError('Provider is required');
      return;
    }
    if (!templateTenantId) {
      templateForm.showError('Tenant is required');
      return;
    }
    try {
      setCreatingTemplate(true);
      templateForm.reset();
      const labels = templateLabels.trim() ? parseTemplateLabels(templateLabels) : undefined;
      await api.createTemplate({
        tenant_id: templateTenantId,
        name,
        provider,
        description: templateDescription.trim() || undefined,
        labels,
      });
      setTemplateName('');
      setTemplateProvider('');
      setTemplateDescription('');
      setTemplateLabels('{}');
      setTemplateTenantId(currentTenantId ?? '');
      setOffset(0);
      reload();
      templateForm.showSuccess('Template created successfully');
      showToast('Template created', 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create template';
      templateForm.showError(message);
      showToast(message, 'error');
    } finally {
      setCreatingTemplate(false);
    }
  };

  const handleCreateVersion = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!selectedTemplate) {
      versionForm.showError('Select a template first');
      return;
    }
    const body = versionBody.trim();
    if (!body) {
      versionForm.showError('Version body is required');
      return;
    }
    try {
      setCreatingVersion(true);
      versionForm.reset();
      const payload = {
        body,
        checksum: versionChecksum.trim() || undefined,
        metadata_schema: parseJsonInput(versionMetadata),
        rollout_notes: versionNotes.trim() || undefined,
      };
      await api.createTemplateVersion(selectedTemplate.id, payload);
      reloadVersions();
      reload();
      versionForm.showSuccess('Version created');
      showToast('Template version created', 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create version';
      versionForm.showError(message);
      showToast(message, 'error');
    } finally {
      setCreatingVersion(false);
    }
  };

  const handlePromoteVersion = async (versionNumber: number) => {
    if (!selectedTemplate) {
      return;
    }
    try {
      setPromotingVersion(versionNumber);
      await api.promoteTemplateVersion(selectedTemplate.id, versionNumber);
      reloadVersions();
      reload();
      showToast(`Version ${versionNumber} promoted`, 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to promote version';
      showToast(message, 'error');
    } finally {
      setPromotingVersion(null);
    }
  };

  const summary = useMemo(() => {
    const total = pagination.total;
    const promoted = templates.filter((tpl) => tpl.promoted_version?.version).length;
    const archived = templates.filter((tpl) => Boolean(tpl.archived_at)).length;
    return { total, promoted, archived };
  }, [pagination.total, templates]);

  const handleToggleArchived = async () => {
    if (!selectedTemplate) {
      return;
    }
    setUpdatingTemplate(true);
    const nextArchived = !selectedTemplate.archived_at;
    try {
      await api.updateTemplate(selectedTemplate.id, { archived: nextArchived });
      showToast(nextArchived ? 'Template archived.' : 'Template restored.', nextArchived ? 'info' : 'success');
      reload();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to update template.';
      showToast(message, 'error');
    } finally {
      setUpdatingTemplate(false);
    }
  };

  const handleCreateTemplateAssignment = async () => {
    if (!selectedTemplate) {
      return;
    }
    const tenantId = selectedTemplate.tenant_id ?? templateAssignmentTenantId;
    const scopedAssignment = buildScopedAssignmentPayload(tenantId, templateAssignmentScope);
    if (!scopedAssignment.payload) {
      showToast(scopedAssignment.error ?? 'Assignment scope is invalid', 'error');
      return;
    }
    setCreatingTemplateAssignment(true);
    try {
      await api.createTemplateAssignment(selectedTemplate.id, scopedAssignment.payload);
      setTemplateAssignmentScope({ scope_type: 'tenant' });
      await loadTemplateAssignments();
      setTemplateAssignmentsError(null);
      showToast(`Template assigned to ${describeAssignmentScope(scopedAssignment.payload)}`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to assign template', 'error');
    } finally {
      setCreatingTemplateAssignment(false);
    }
  };

  const handleConfirmDeleteTemplateAssignment = async () => {
    if (!selectedTemplate || !pendingDeleteAssignment) {
      return;
    }
    setDeletingTemplateAssignment(true);
    setTemplateAssignmentActionError(null);
    try {
      await api.deleteTemplateAssignment(selectedTemplate.id, pendingDeleteAssignment.id);
      setTemplateAssignments((current) => current.filter((assignment) => assignment.id !== pendingDeleteAssignment.id));
      setPendingDeleteAssignment(null);
      showToast('Template assignment removed', 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to remove template assignment';
      setTemplateAssignmentActionError(message);
      showToast(message, 'error');
    } finally {
      setDeletingTemplateAssignment(false);
    }
  };

  const templateColumns: ColumnDef<Template>[] = [
    {
      header: 'Name',
      accessorKey: 'name',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
    },
    {
      header: 'Provider',
      accessorKey: 'provider',
      cell: ({ row }) => <span className="text-text-secondary">{row.original.provider || '-'}</span>,
    },
    {
      header: 'Status',
      id: 'status',
      cell: ({ row }) => (
        <StatusTag tone={templateStatusTone(row.original)}>
          {templateStatus(row.original)}
        </StatusTag>
      ),
    },
    {
      header: 'Updated',
      accessorKey: 'updated_at',
      cell: ({ row }) => (
        <span className="text-text-secondary">{formatDate(row.original.updated_at)}</span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={() => setSelectedTemplateId(row.original.id)}
        >
          View
        </Button>
      ),
    },
  ];

  const waveStatusTone = (s: ClusterRolloutWave['status']): StateTone => {
    switch (s) {
      case 'running': return 'healthy';
      case 'done': return 'info';
      case 'paused': return 'warning';
      case 'aborted': return 'critical';
      default: return 'unknown';
    }
  };

  const waveColumns: ColumnDef<ClusterRolloutWave>[] = [
    {
      header: 'Name',
      accessorKey: 'name',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
    },
    {
      header: 'Order',
      accessorKey: 'order',
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.order}</span>,
    },
    {
      header: 'Status',
      accessorKey: 'status',
      cell: ({ row }) => (
        <StatusTag tone={waveStatusTone(row.original.status)} className="capitalize">
          {row.original.status}
        </StatusTag>
      ),
    },
    {
      header: 'Progress',
      id: 'progress',
      cell: ({ row }) => {
        const pct = row.original.node_count > 0
          ? Math.round((row.original.done_count / row.original.node_count) * 100)
          : 0;
        return (
          <div className="flex items-center gap-2">
            <div className="h-1.5 w-24 overflow-hidden rounded-full bg-border-subtle">
              <div className="h-full rounded-full bg-brand-500" style={{ width: `${pct}%` }} />
            </div>
            <span className="font-mono text-xs text-text-muted">
              {row.original.done_count}/{row.original.node_count}
            </span>
          </div>
        );
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const s = row.original.status;
        return (
          <div className="flex items-center gap-1">
            {s === 'running' && (
              <Button
                variant="secondary"
                size="sm"
                onClick={() => handleWaveAction(row.original.id, 'paused')}
                disabled={waveActionId !== null}
              >
                Pause
              </Button>
            )}
            {s === 'paused' && (
              <Button
                variant="primary"
                size="sm"
                onClick={() => handleWaveAction(row.original.id, 'running')}
                disabled={waveActionId !== null}
              >
                Resume
              </Button>
            )}
            {(s === 'running' || s === 'paused') && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => handleWaveAction(row.original.id, 'aborted')}
                disabled={waveActionId !== null}
              >
                Abort
              </Button>
            )}
          </div>
        );
      },
    },
  ];

  const tabClass = (t: PageTab) =>
    `px-4 py-2 text-sm font-medium rounded-t-md transition-colors ${
      pageTab === t
        ? 'bg-surface text-foreground border border-border-subtle border-b-surface'
        : 'text-text-secondary hover:text-foreground'
    }`;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="INFRASTRUCTURE / TEMPLATES"
        title="Infrastructure templates"
        description="Version, test, and safely roll out infrastructure changes. Each template is reusable across tenants."
      />

      <div className="flex gap-1 border-b border-border-subtle">
        <button type="button" className={tabClass('templates')} onClick={() => setPageTab('templates')}>
          Templates
        </button>
        <button type="button" className={tabClass('rollouts')} onClick={() => setPageTab('rollouts')}>
          Rollouts
        </button>
      </div>

      {pageTab === 'rollouts' && (
        <>
          <div className="flex items-center gap-3">
            <select
              className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-brand-500"
              value={rolloutTenantId}
              onChange={(e) => setRolloutTenantId(e.target.value)}
            >
              <option value="">All tenants</option>
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>{t.name}</option>
              ))}
            </select>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setWavesReloadToken((n) => n + 1)}
              disabled={wavesLoading}
            >
              Refresh
            </Button>
          </div>
          <Panel padding="md" eyebrow="ROLLOUT / WAVES" title="Cluster rollout waves">
            {wavesError ? (
              <p className="mb-3 text-sm text-state-critical" role="alert">
                Rollout waves unavailable: {wavesError}
              </p>
            ) : null}
            <DataTable
              columns={waveColumns}
              rows={waves}
              rowKey={(r) => r.id}
              loading={wavesLoading}
              empty={
                wavesError ? (
                  <EmptyState
                    icon={<Layers />}
                    title="Rollout waves unavailable"
                    description="Resolve the service error or refresh to retry loading rollout progress."
                  />
                ) : (
                  <EmptyState
                    icon={<Layers />}
                    title="No rollout waves"
                    description="Cluster rollout waves appear here when a deployment is in progress."
                  />
                )
              }
            />
          </Panel>
        </>
      )}

      {pageTab === 'templates' && (
        <>
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <KpiTile label="Total templates" value={summary.total} tone="brand" />
        <KpiTile label="Promoted" value={summary.promoted} hint="stable rollouts" />
        <KpiTile label="Archived" value={summary.archived} hint="hidden from provisioning" />
      </div>

      {/* Create template */}
      <Panel padding="md" eyebrow="CREATE" title="New template" toneAccent="brand">
        <form onSubmit={handleCreateTemplate} className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="template-name">Name</Label>
              <Input
                id="template-name"
                type="text"
                value={templateName}
                onChange={(event) => setTemplateName(event.target.value)}
                placeholder="e.g. aws-foundation"
                disabled={creatingTemplate}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="template-provider">Provider</Label>
              <Input
                id="template-provider"
                type="text"
                value={templateProvider}
                onChange={(event) => setTemplateProvider(event.target.value)}
                placeholder="aws|azure|gcp|mock"
                disabled={creatingTemplate}
                required
              />
            </div>
            <SelectField
              id="template-tenant"
              label="Tenant"
              value={templateTenantId}
              onChange={(event) => setTemplateTenantId(event.target.value)}
              disabled={creatingTemplate}
            >
              <option value="" disabled>Select tenant</option>
              {tenants.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>{tenant.name}</option>
              ))}
            </SelectField>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="template-description">Description</Label>
            <textarea
              id="template-description"
              className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
              rows={3}
              value={templateDescription}
              onChange={(event) => setTemplateDescription(event.target.value)}
              placeholder="High-level purpose"
              disabled={creatingTemplate}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="template-labels">Labels (JSON)</Label>
            <textarea
              id="template-labels"
              className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
              rows={3}
              value={templateLabels}
              onChange={(event) => setTemplateLabels(event.target.value)}
              disabled={creatingTemplate}
            />
          </div>

          {templateForm.error ? <p className="text-sm text-state-critical" role="alert">{templateForm.error}</p> : null}
          {templateForm.success ? <p className="text-sm text-state-healthy" role="status">{templateForm.success}</p> : null}

          <div className="flex items-center gap-2 pt-2">
            <Button type="submit" variant="primary" disabled={creatingTemplate || !templateTenantId}>
              {creatingTemplate ? 'Saving...' : 'Create template'}
            </Button>
          </div>
        </form>
      </Panel>

      {/* Filter panel */}
      <Panel padding="md" tone="inset" eyebrow="FILTER" title="Find templates">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex flex-col gap-1.5 flex-1 min-w-[140px]">
            <Label htmlFor="name-filter">Name prefix</Label>
            <Input
              id="name-filter"
              type="text"
              value={nameFilter}
              onChange={(event) => {
                setNameFilter(event.target.value);
                setOffset(0);
              }}
              placeholder="search by name"
            />
          </div>
          <div className="flex flex-col gap-1.5 flex-1 min-w-[140px]">
            <Label htmlFor="provider-filter">Provider</Label>
            <Input
              id="provider-filter"
              type="text"
              value={providerFilter}
              onChange={(event) => {
                setProviderFilter(event.target.value);
                setOffset(0);
              }}
              placeholder="aws"
            />
          </div>
          <div className="flex items-center gap-2 pt-5">
            <input
              id="include-archived"
              type="checkbox"
              checked={includeArchived}
              onChange={(event) => {
                setIncludeArchived(event.target.checked);
                setOffset(0);
              }}
              className="h-4 w-4 rounded border border-border-subtle"
            />
            <Label htmlFor="include-archived">Include archived</Label>
          </div>
        </div>
      </Panel>

      {loading ? <p className="text-text-muted">Loading templates...</p> : null}
      {error ? <p className="text-sm text-state-critical" role="alert">Failed to load templates: {error}</p> : null}

      {!loading && !error && templates.length === 0 ? (
        <EmptyState
          title="No templates"
          description="No templates found. Create one to get started."
          icon={<FileText />}
        />
      ) : null}

      {!loading && templates.length > 0 ? (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          {/* LEFT: Template list */}
          <div className="flex flex-col gap-4">
            <DataTable
              columns={templateColumns}
              rows={templates}
              rowKey={(row) => row.id}
            />
            <div className="flex items-center justify-between gap-4 pt-2 text-sm text-text-muted">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={pagination.prevOffset === null || pagination.prevOffset === undefined}
                onClick={() => setOffset(pagination.prevOffset ?? 0)}
              >
                Previous
              </Button>
              <span>Showing {templates.length} of {pagination.total} templates</span>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
                onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
              >
                Next
              </Button>
            </div>
          </div>

          {/* RIGHT: Template detail */}
          {selectedTemplate ? (
            <Panel
              padding="md"
              eyebrow={selectedTemplate.provider.toUpperCase()}
              title={selectedTemplate.name}
              actions={
                <Button
                  type="button"
                  variant={selectedTemplate.archived_at ? 'primary' : 'danger'}
                  size="sm"
                  onClick={handleToggleArchived}
                  disabled={updatingTemplate}
                >
                  {updatingTemplate
                    ? 'Updating...'
                    : selectedTemplate.archived_at
                      ? 'Restore'
                      : 'Archive'}
                </Button>
              }
            >
              {selectedTemplate.description ? (
                <p className="text-sm text-text-secondary">{selectedTemplate.description}</p>
              ) : null}

              <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">ID</dt>
                  <dd><code className="font-mono text-xs text-text-secondary">{selectedTemplate.id}</code></dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Tenant</dt>
                  <dd className="text-foreground">
                    {tenants.find((tenant) => tenant.id === selectedTemplate.tenant_id)?.name ?? 'Platform global'}
                  </dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Created</dt>
                  <dd className="text-foreground">{formatDate(selectedTemplate.created_at)}</dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Updated</dt>
                  <dd className="text-foreground">{formatDate(selectedTemplate.updated_at)}</dd>
                </div>
                <div className="flex flex-col gap-0.5">
                  <dt className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">Archived</dt>
                  <dd className="text-foreground">
                    {selectedTemplate.archived_at ? formatDate(selectedTemplate.archived_at) : '-'}
                  </dd>
                </div>
              </dl>

              <hr className="border-border-subtle" />

              <div className="flex flex-col gap-2">
                <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Labels</p>
                {selectedTemplate.labels && Object.keys(selectedTemplate.labels).length > 0 ? (
                  <div className="flex flex-wrap gap-1.5">
                    {Object.entries(selectedTemplate.labels).map(([key, value]) => (
                      <span
                        key={key}
                        className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-surface px-2.5 py-0.5 text-xs text-text-secondary"
                      >
                        <span className="font-medium text-foreground">{key}</span>
                        <span className="text-text-muted">:</span>
                        <span>{String(value)}</span>
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-text-muted">No labels assigned.</p>
                )}
              </div>

              <hr className="border-border-subtle" />

              <div className="flex flex-col gap-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Assignments</p>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => void loadTemplateAssignments()}
                    disabled={templateAssignmentsLoading}
                  >
                    Refresh
                  </Button>
                </div>

                {templateAssignmentsLoading ? <p className="text-sm text-text-muted">Loading assignments...</p> : null}
                {templateAssignmentsError ? (
                  <p className="text-sm text-state-critical" role="alert">
                    Template assignments unavailable: {templateAssignmentsError}
                  </p>
                ) : null}
                {!templateAssignmentsLoading && !templateAssignmentsError && templateAssignments.length === 0 ? (
                  <p className="text-sm text-text-muted">No assignments.</p>
                ) : null}
                {!templateAssignmentsLoading && templateAssignments.length > 0 ? (
                  <div className="flex flex-col gap-2">
                    {templateAssignments.map((assignment) => (
                      <div
                        key={assignment.id}
                        className="flex items-center justify-between gap-3 rounded-md border border-border-subtle bg-surface px-3 py-2"
                      >
                        <div className="min-w-0">
                          <p className="text-sm font-medium text-foreground">
                            {describeAssignmentScope(assignment)}
                          </p>
                          <p className="font-mono text-[0.65rem] text-text-muted">
                            {formatDate(assignment.assigned_at)}
                          </p>
                        </div>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="text-state-critical hover:text-state-critical"
                          onClick={() => {
                            setTemplateAssignmentActionError(null);
                            setPendingDeleteAssignment(assignment);
                          }}
                        >
                          Remove
                        </Button>
                      </div>
                    ))}
                  </div>
                ) : null}

                <div className="flex flex-col gap-3 rounded-md border border-border-subtle bg-elevated p-3">
                  {!selectedTemplate.tenant_id ? (
                    <SelectField
                      id="template-assignment-tenant"
                      label="Tenant"
                      value={templateAssignmentTenantId}
                      onChange={(event) => setTemplateAssignmentTenantId(event.target.value)}
                      disabled={creatingTemplateAssignment}
                    >
                      <option value="" disabled>Select tenant</option>
                      {tenants.map((tenant) => (
                        <option key={tenant.id} value={tenant.id}>{tenant.name}</option>
                      ))}
                    </SelectField>
                  ) : null}
                  <div className="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_auto] lg:items-end">
                    <ScopePicker
                      tenantId={selectedTemplate.tenant_id ?? templateAssignmentTenantId}
                      value={templateAssignmentScope}
                      onChange={setTemplateAssignmentScope}
                      disabled={
                        creatingTemplateAssignment ||
                        !(selectedTemplate.tenant_id ?? templateAssignmentTenantId)
                      }
                      idPrefix={`template-${selectedTemplate.id}`}
                    />
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={() => void handleCreateTemplateAssignment()}
                      disabled={
                        creatingTemplateAssignment ||
                        !(selectedTemplate.tenant_id ?? templateAssignmentTenantId)
                      }
                    >
                      {creatingTemplateAssignment ? 'Assigning...' : 'Add assignment'}
                    </Button>
                  </div>
                </div>
              </div>

              <hr className="border-border-subtle" />

              <div className="flex flex-col gap-3">
                <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Versions</p>
                {versionsLoading ? <p className="text-sm text-text-muted">Loading versions...</p> : null}
                {versionsError ? (
                  <p className="text-sm text-state-critical" role="alert">
                    Template versions unavailable: {versionsError}
                  </p>
                ) : null}
                {!versionsLoading && !versionsError && versions.length === 0 ? (
                  <p className="text-sm text-text-muted">No versions published yet.</p>
                ) : null}
                {!versionsLoading && versions.length > 0 ? (
                  <ul className="flex flex-col gap-2">
                    {versions.map((version) => (
                      <li
                        key={version.id}
                        className="rounded-md border border-border-subtle bg-surface px-3 py-2"
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-2">
                            <span className="font-mono text-sm font-semibold text-foreground">
                              v{version.version}
                            </span>
                            <span className="text-xs text-text-muted">
                              {formatDate(version.created_at)}
                            </span>
                            {version.promoted_at ? (
                              <StatusTag tone="healthy">Promoted</StatusTag>
                            ) : null}
                          </div>
                          <Button
                            type="button"
                            variant="primary"
                            size="sm"
                            onClick={() => handlePromoteVersion(version.version)}
                            disabled={promotingVersion !== null}
                          >
                            {promotingVersion === version.version ? 'Promoting...' : 'Promote'}
                          </Button>
                        </div>
                        {version.rollout_notes ? (
                          <p className="mt-1 text-xs text-text-secondary">{version.rollout_notes}</p>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                ) : null}

                <div className="flex items-center justify-between gap-4 text-sm text-text-muted">
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    disabled={versionPagination.prevOffset === null || versionPagination.prevOffset === undefined}
                    onClick={() => setVersionOffset(versionPagination.prevOffset ?? 0)}
                  >
                    Previous
                  </Button>
                  <span>Showing {versions.length} of {versionPagination.total}</span>
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    disabled={versionPagination.nextOffset === null || versionPagination.nextOffset === undefined}
                    onClick={() => setVersionOffset(versionPagination.nextOffset ?? versionOffset + versionLimit)}
                  >
                    Next
                  </Button>
                </div>
              </div>

              <hr className="border-border-subtle" />

              <div className="flex flex-col gap-3">
                <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Create version</p>
                <form onSubmit={handleCreateVersion} className="flex flex-col gap-3">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="version-body">Body (JSON/YAML)</Label>
                    <textarea
                      id="version-body"
                      className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                      rows={6}
                      value={versionBody}
                      onChange={(event) => setVersionBody(event.target.value)}
                      disabled={creatingVersion}
                    />
                  </div>

                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div className="flex flex-col gap-1.5">
                      <Label htmlFor="version-checksum">Checksum</Label>
                      <Input
                        id="version-checksum"
                        type="text"
                        value={versionChecksum}
                        onChange={(event) => setVersionChecksum(event.target.value)}
                        disabled={creatingVersion}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <Label htmlFor="version-metadata">Metadata schema (JSON)</Label>
                      <textarea
                        id="version-metadata"
                        className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                        rows={4}
                        value={versionMetadata}
                        onChange={(event) => setVersionMetadata(event.target.value)}
                        disabled={creatingVersion}
                      />
                    </div>
                  </div>

                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="version-notes">Rollout notes</Label>
                    <textarea
                      id="version-notes"
                      className="flex min-h-[100px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                      rows={3}
                      value={versionNotes}
                      onChange={(event) => setVersionNotes(event.target.value)}
                      disabled={creatingVersion}
                    />
                  </div>

                  {versionForm.error ? (
                    <p className="text-sm text-state-critical" role="alert">{versionForm.error}</p>
                  ) : null}
                  {versionForm.success ? (
                    <p className="text-sm text-state-healthy" role="status">{versionForm.success}</p>
                  ) : null}

                  <div className="flex items-center gap-2 pt-2">
                    <Button type="submit" variant="primary" disabled={creatingVersion}>
                      {creatingVersion ? 'Publishing...' : 'Create version'}
                    </Button>
                  </div>
                </form>
              </div>
            </Panel>
          ) : null}
        </div>
      ) : null}
        </>
      )}
      <ConfirmModal
        open={Boolean(pendingDeleteAssignment)}
        title="Remove template assignment"
        body={
          pendingDeleteAssignment
            ? `Remove ${describeAssignmentScope(pendingDeleteAssignment)} from this template? The template and versions remain available.`
            : undefined
        }
        confirmLabel={deletingTemplateAssignment ? 'Removing...' : 'Remove assignment'}
        cancelDisabled={deletingTemplateAssignment}
        confirmDisabled={deletingTemplateAssignment}
        variant="danger"
        onConfirm={() => void handleConfirmDeleteTemplateAssignment()}
        onCancel={() => {
          if (!deletingTemplateAssignment) {
            setPendingDeleteAssignment(null);
            setTemplateAssignmentActionError(null);
          }
        }}
      >
        {templateAssignmentActionError ? (
          <p className="text-sm text-state-critical" role="alert">
            {templateAssignmentActionError}
          </p>
        ) : null}
      </ConfirmModal>
    </div>
  );
}
