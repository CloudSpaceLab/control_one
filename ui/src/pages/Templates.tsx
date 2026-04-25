import { FormEvent, useEffect, useMemo, useState } from 'react';
import { Template } from '../lib/api';
import { useTemplates } from '../hooks/useTemplates';
import { useTemplateVersions } from '../hooks/useTemplateVersions';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import {
  DEFAULT_METADATA_SCHEMA,
  DEFAULT_TEMPLATE_BODY,
  parseJsonInput,
  parseTemplateLabels,
  templateStatus,
} from '../lib/templateUtils';

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function Templates(): JSX.Element {
  const api = useApiClient();
  const { showToast } = useToast();
  const [limit] = useState(20);
  const [offset, setOffset] = useState(0);
  const [providerFilter, setProviderFilter] = useState('');
  const [nameFilter, setNameFilter] = useState('');
  const [includeArchived, setIncludeArchived] = useState(false);
  const templateOptions = {
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

  const [versionBody, setVersionBody] = useState(DEFAULT_TEMPLATE_BODY);
  const [versionChecksum, setVersionChecksum] = useState('');
  const [versionMetadata, setVersionMetadata] = useState(DEFAULT_METADATA_SCHEMA);
  const [versionNotes, setVersionNotes] = useState('');

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
    try {
      setCreatingTemplate(true);
      templateForm.reset();
      const labels = templateLabels.trim() ? parseTemplateLabels(templateLabels) : undefined;
      await api.createTemplate({
        name,
        provider,
        description: templateDescription.trim() || undefined,
        labels,
      });
      setTemplateName('');
      setTemplateProvider('');
      setTemplateDescription('');
      setTemplateLabels('{}');
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
      await api.promoteTemplateVersion(selectedTemplate.id, versionNumber);
      reloadVersions();
      reload();
      showToast(`Version ${versionNumber} promoted`, 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to promote version';
      showToast(message, 'error');
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

  return (
    <section className="templates-page">
      <div className="page-header">
        <div>
          <h2>Infrastructure templates</h2>
          <p>
            Version, test, and safely roll out infrastructure changes. Each template is reusable across tenants.
          </p>
        </div>
      </div>

      <div className="stat-card-grid">
        <article className="stat-card">
          <span className="muted">Total templates</span>
          <strong>{summary.total}</strong>
        </article>
        <article className="stat-card">
          <span className="muted">Promoted</span>
          <strong>{summary.promoted}</strong>
          <small className="muted">stable rollouts</small>
        </article>
        <article className="stat-card">
          <span className="muted">Archived</span>
          <strong>{summary.archived}</strong>
          <small className="muted">hidden from provisioning</small>
        </article>
      </div>

      <form className="panel templates-form" onSubmit={handleCreateTemplate}>
        <h3>Create template</h3>
        <div className="grid two-col">
          <label htmlFor="template-name">
            Name
            <input
              id="template-name"
              type="text"
              value={templateName}
              onChange={(event) => setTemplateName(event.target.value)}
              placeholder="e.g. aws-foundation"
              disabled={creatingTemplate}
              required
            />
          </label>
          <label htmlFor="template-provider">
            Provider
            <input
              id="template-provider"
              type="text"
              value={templateProvider}
              onChange={(event) => setTemplateProvider(event.target.value)}
              placeholder="aws|azure|gcp|mock"
              disabled={creatingTemplate}
              required
            />
          </label>
        </div>
        <label htmlFor="template-description">
          Description
          <textarea
            id="template-description"
            rows={3}
            value={templateDescription}
            onChange={(event) => setTemplateDescription(event.target.value)}
            placeholder="High-level purpose"
            disabled={creatingTemplate}
          />
        </label>
        <label htmlFor="template-labels">
          Labels (JSON)
          <textarea
            id="template-labels"
            rows={3}
            value={templateLabels}
            onChange={(event) => setTemplateLabels(event.target.value)}
            disabled={creatingTemplate}
          />
        </label>
        {templateForm.error ? <p className="form-error">{templateForm.error}</p> : null}
        {templateForm.success ? <p className="form-success">{templateForm.success}</p> : null}
        <button type="submit" disabled={creatingTemplate}>
          {creatingTemplate ? 'Saving…' : 'Create template'}
        </button>
      </form>

      <div className="panel toolbar templates-toolbar">
        <label htmlFor="name-filter">
          Name prefix
          <input
            id="name-filter"
            type="text"
            value={nameFilter}
            onChange={(event) => {
              setNameFilter(event.target.value);
              setOffset(0);
            }}
            placeholder="search by name"
          />
        </label>
        <label htmlFor="provider-filter">
          Provider
          <input
            id="provider-filter"
            type="text"
            value={providerFilter}
            onChange={(event) => {
              setProviderFilter(event.target.value);
              setOffset(0);
            }}
            placeholder="aws"
          />
        </label>
        <label htmlFor="include-archived" className="checkbox-inline">
          <input
            id="include-archived"
            type="checkbox"
            checked={includeArchived}
            onChange={(event) => {
              setIncludeArchived(event.target.checked);
              setOffset(0);
            }}
          />
          Include archived
        </label>
      </div>

      {loading ? <p className="muted">Loading templates…</p> : null}
      {error ? <p className="form-error">Failed to load templates: {error}</p> : null}
      {!loading && !error && templates.length === 0 ? (
        <div className="empty-state">
          <p className="muted">No templates found. Create one to get started.</p>
        </div>
      ) : null}

      {!loading && templates.length > 0 ? (
        <div className="templates-layout">
          <div className="templates-list">
            <table className="templates-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Provider</th>
                  <th>Status</th>
                  <th>Updated</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {templates.map((template) => (
                  <tr key={template.id} className={template.id === selectedTemplateId ? 'active-row' : ''}>
                    <td>{template.name}</td>
                    <td>{template.provider || '—'}</td>
                    <td>
                      <span className="badge">{templateStatus(template)}</span>
                    </td>
                    <td>{formatDate(template.updated_at)}</td>
                    <td>
                      <button type="button" onClick={() => setSelectedTemplateId(template.id)}>
                        View
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="pagination">
              <button
                type="button"
                disabled={pagination.prevOffset === null || pagination.prevOffset === undefined}
                onClick={() => setOffset(pagination.prevOffset ?? 0)}
              >
                Previous
              </button>
              <span>
                Showing {templates.length} of {pagination.total} templates
              </span>
              <button
                type="button"
                disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
                onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
              >
                Next
              </button>
            </div>
          </div>
          <aside className="panel templates-detail">
            {selectedTemplate ? (
              <>
                <header>
                  <div>
                    <p className="eyebrow">{selectedTemplate.provider.toUpperCase()}</p>
                    <h3>{selectedTemplate.name}</h3>
                  </div>
                  <span className="badge">{templateStatus(selectedTemplate)}</span>
                </header>
                {selectedTemplate.description ? <p>{selectedTemplate.description}</p> : null}
                <dl className="meta-grid">
                  <div>
                    <dt>Template ID</dt>
                    <dd>{selectedTemplate.id}</dd>
                  </div>
                  <div>
                    <dt>Created</dt>
                    <dd>{formatDate(selectedTemplate.created_at)}</dd>
                  </div>
                  <div>
                    <dt>Updated</dt>
                    <dd>{formatDate(selectedTemplate.updated_at)}</dd>
                  </div>
                  <div>
                    <dt>Archived</dt>
                    <dd>{selectedTemplate.archived_at ? formatDate(selectedTemplate.archived_at) : '—'}</dd>
                  </div>
                </dl>
                <section>
                  <h4>Labels</h4>
                  {selectedTemplate.labels && Object.keys(selectedTemplate.labels).length > 0 ? (
                    <ul className="label-list">
                      {Object.entries(selectedTemplate.labels).map(([key, value]) => (
                        <li key={key}>
                          <strong>{key}</strong>
                          <span>{value}</span>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="muted">No labels assigned.</p>
                  )}
                </section>
                <section>
                  <h4>Versions</h4>
                  {versionsLoading ? <p className="muted">Loading versions…</p> : null}
                  {versionsError ? <p className="form-error">{versionsError}</p> : null}
                  {!versionsLoading && versions.length === 0 ? (
                    <p className="muted">No versions published yet.</p>
                  ) : null}
                  {!versionsLoading && versions.length > 0 ? (
                    <ul className="versions-list">
                      {versions.map((version) => (
                        <li key={version.id}>
                          <div>
                            <strong>v{version.version}</strong> • {formatDate(version.created_at)}
                            {version.promoted_at ? (
                              <span className="badge badge-success">Promoted</span>
                            ) : null}
                          </div>
                          {version.rollout_notes ? <p>{version.rollout_notes}</p> : null}
                          <div className="version-actions">
                            <button type="button" onClick={() => handlePromoteVersion(version.version)}>
                              Promote
                            </button>
                          </div>
                        </li>
                      ))}
                    </ul>
                  ) : null}
                  <div className="pagination">
                    <button
                      type="button"
                      disabled={
                        versionPagination.prevOffset === null || versionPagination.prevOffset === undefined
                      }
                      onClick={() => setVersionOffset(versionPagination.prevOffset ?? 0)}
                    >
                      Previous
                    </button>
                    <span>
                      Showing {versions.length} of {versionPagination.total} versions
                    </span>
                    <button
                      type="button"
                      disabled={
                        versionPagination.nextOffset === null || versionPagination.nextOffset === undefined
                      }
                      onClick={() => setVersionOffset(versionPagination.nextOffset ?? versionOffset + versionLimit)}
                    >
                      Next
                    </button>
                  </div>
                </section>
                <section>
                  <h4>Create version</h4>
                  <form onSubmit={handleCreateVersion} className="version-form">
                    <label htmlFor="version-body">
                      Body (JSON/YAML)
                      <textarea
                        id="version-body"
                        rows={6}
                        value={versionBody}
                        onChange={(event) => setVersionBody(event.target.value)}
                        disabled={creatingVersion}
                      />
                    </label>
                    <label htmlFor="version-checksum">
                      Checksum
                      <input
                        id="version-checksum"
                        type="text"
                        value={versionChecksum}
                        onChange={(event) => setVersionChecksum(event.target.value)}
                        disabled={creatingVersion}
                      />
                    </label>
                    <label htmlFor="version-metadata">
                      Metadata schema (JSON)
                      <textarea
                        id="version-metadata"
                        rows={4}
                        value={versionMetadata}
                        onChange={(event) => setVersionMetadata(event.target.value)}
                        disabled={creatingVersion}
                      />
                    </label>
                    <label htmlFor="version-notes">
                      Rollout notes
                      <textarea
                        id="version-notes"
                        rows={3}
                        value={versionNotes}
                        onChange={(event) => setVersionNotes(event.target.value)}
                        disabled={creatingVersion}
                      />
                    </label>
                    {versionForm.error ? <p className="form-error">{versionForm.error}</p> : null}
                    {versionForm.success ? <p className="form-success">{versionForm.success}</p> : null}
                    <button type="submit" disabled={creatingVersion}>
                      {creatingVersion ? 'Publishing…' : 'Create version'}
                    </button>
                  </form>
                </section>
                <div className="detail-actions">
                  <button type="button" className="ghost-button" onClick={reload} disabled={loading}>
                    {loading ? 'Refreshing…' : 'Refresh list'}
                  </button>
                  <button
                    type="button"
                    className={selectedTemplate.archived_at ? 'primary-button' : 'danger-button'}
                    onClick={handleToggleArchived}
                    disabled={updatingTemplate}
                  >
                    {updatingTemplate
                      ? 'Updating…'
                      : selectedTemplate.archived_at
                        ? 'Restore template'
                        : 'Archive template'}
                  </button>
                </div>
              </>
            ) : (
              <p className="muted">Select a template to inspect versions and metadata.</p>
            )}
          </aside>
        </div>
      ) : null}
    </section>
  );
}
