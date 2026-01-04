import { FormEvent, useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function Tenants(): JSX.Element {
  const api = useApiClient();
  const [offset, setOffset] = useState(0);
  const [limit] = useState(20);
  const [nameFilter, setNameFilter] = useState('');
  const { data, pagination, loading, error, reload } = useTenants({
    limit,
    offset,
    namePrefix: nameFilter.trim() || undefined,
  });
  const [tenantName, setTenantName] = useState('');
  const { error: formError, success: formSuccess, showError, showSuccess, reset } = useFormFeedback();
  const { showToast } = useToast();
  const [submitting, setSubmitting] = useState(false);
  const [selectedTenantId, setSelectedTenantId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const rows = useMemo(() => data, [data]);
  const selectedTenant = useMemo(
    () => rows.find((tenant) => tenant.id === selectedTenantId) ?? null,
    [rows, selectedTenantId],
  );

  const summary = useMemo(() => {
    const total = pagination.total;
    const newest = rows[0];
    return {
      total,
      newestName: newest?.name ?? '—',
      newestDate: newest ? formatDate(newest.created_at) : '—',
    };
  }, [pagination.total, rows]);

  const handleCreateTenant = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const name = tenantName.trim();
    if (!name) {
      showError('Tenant name is required');
      return;
    }

    setSubmitting(true);
    reset();
    try {
      await api.createTenant({ name });
      setTenantName('');
      setOffset(0);
      reload();
      const successMessage = 'Tenant created successfully.';
      showSuccess(successMessage);
      showToast(successMessage, 'success');
    } catch (err) {
      if (err instanceof Error) {
        showError(err.message);
        showToast(err.message, 'error');
      } else {
        const fallback = 'Failed to create tenant';
        showError(fallback);
        showToast(fallback, 'error');
      }
    } finally {
      setSubmitting(false);
    }
  };

  const openTenantDetails = (tenantId: string) => {
    setSelectedTenantId((current) => (current === tenantId ? null : tenantId));
    const tenant = rows.find((t) => t.id === tenantId);
    setRenameValue(tenant?.name ?? '');
  };

  const handleRenameTenant = async () => {
    if (!selectedTenant) {
      return;
    }
    const next = renameValue.trim();
    if (!next) {
      showToast('Tenant name cannot be empty.', 'error');
      return;
    }
    if (next === selectedTenant.name) {
      showToast('No changes detected.', 'info');
      return;
    }
    setRenaming(true);
    try {
      await api.updateTenant(selectedTenant.id, { name: next });
      showToast('Tenant renamed.', 'success');
      reload();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to rename tenant.';
      showToast(message, 'error');
    } finally {
      setRenaming(false);
    }
  };

  const handleDeleteTenant = async () => {
    if (!selectedTenant) {
      return;
    }
    const confirmed = window.confirm(
      `Delete tenant “${selectedTenant.name}”? Nodes and jobs referencing this tenant may become orphaned.`,
    );
    if (!confirmed) {
      return;
    }
    setDeleting(true);
    try {
      await api.deleteTenant(selectedTenant.id);
      showToast('Tenant deleted.', 'success');
      setSelectedTenantId(null);
      reload();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to delete tenant.';
      showToast(message, 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <section className="tenants-page">
      <div className="page-header">
        <div>
          <h2>Tenants</h2>
          <p>Tenants represent isolation boundaries for infrastructure, policy, and compliance scope.</p>
        </div>
      </div>

      <div className="stat-card-grid">
        <article className="stat-card">
          <span className="muted">Total tenants</span>
          <strong>{summary.total}</strong>
        </article>
        <article className="stat-card">
          <span className="muted">Most recent</span>
          <strong>{summary.newestName}</strong>
          <small className="muted">{summary.newestDate}</small>
        </article>
      </div>

      <div className="tenants-layout">
        <form className="panel tenants-form" onSubmit={handleCreateTenant}>
          <h3>Create tenant</h3>
          <label htmlFor="tenant-name">Name</label>
          <input
            id="tenant-name"
            name="tenant-name"
            type="text"
            value={tenantName}
            onChange={(event) => setTenantName(event.target.value)}
            placeholder="e.g. Production Cluster"
            disabled={submitting}
            required
          />
          {formError ? <p className="form-error">{formError}</p> : null}
          {formSuccess ? <p className="form-success">{formSuccess}</p> : null}
          <button type="submit" disabled={submitting}>
            {submitting ? 'Creating…' : 'Create tenant'}
          </button>
        </form>

        <div className="panel tenants-list">
          <div className="toolbar tenants-toolbar">
            <label htmlFor="tenant-search">
              Filter
              <input
                id="tenant-search"
                type="search"
                placeholder="Search by name"
                value={nameFilter}
                onChange={(event) => {
                  setNameFilter(event.target.value);
                  setOffset(0);
                }}
              />
            </label>
            <button
              type="button"
              className="ghost-button"
              onClick={() => {
                reload();
              }}
              disabled={loading}
            >
              {loading ? 'Refreshing…' : 'Refresh'}
            </button>
          </div>

          {loading ? <p className="muted">Loading tenants&hellip;</p> : null}
          {error ? <p className="form-error">Failed to load tenants: {error}</p> : null}
          {!loading && !error && rows.length === 0 ? <p className="muted">No tenants match the current filters.</p> : null}

          {!loading && !error && rows.length > 0 ? (
            <>
              <table className="tenants-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Tenant ID</th>
                    <th>Created</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {rows.map((tenant) => (
                    <tr key={tenant.id} className={selectedTenantId === tenant.id ? 'active-row' : undefined}>
                      <td>{tenant.name}</td>
                      <td>{tenant.id}</td>
                      <td>{formatDate(tenant.created_at)}</td>
                      <td>
                        <button type="button" className="ghost-button" onClick={() => openTenantDetails(tenant.id)}>
                          {selectedTenantId === tenant.id ? 'Hide' : 'View'}
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
                  Showing {rows.length} of {pagination.total} tenants
                </span>
                <button
                  type="button"
                  disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
                  onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
                >
                  Next
                </button>
              </div>
            </>
          ) : null}
        </div>

        {selectedTenant ? (
          <aside className="panel tenant-detail">
            <h3>Tenant details</h3>
            <dl className="meta-grid">
              <div>
                <dt>Name</dt>
                <dd>{selectedTenant.name}</dd>
              </div>
              <div>
                <dt>Tenant ID</dt>
                <dd className="mono">{selectedTenant.id}</dd>
              </div>
              <div>
                <dt>Created</dt>
                <dd>{formatDate(selectedTenant.created_at)}</dd>
              </div>
            </dl>
            <div className="tenant-detail-form">
              <label htmlFor="rename-tenant">Rename tenant</label>
              <input
                id="rename-tenant"
                type="text"
                value={renameValue}
                onChange={(event) => setRenameValue(event.target.value)}
              />
              <div className="detail-actions">
                <button type="button" className="ghost-button" onClick={() => setSelectedTenantId(null)}>
                  Close
                </button>
                <button type="button" className="primary-button" onClick={handleRenameTenant} disabled={renaming}>
                  {renaming ? 'Saving…' : 'Save changes'}
                </button>
                <button type="button" className="danger-button" onClick={handleDeleteTenant} disabled={deleting}>
                  {deleting ? 'Deleting…' : 'Delete tenant'}
                </button>
              </div>
            </div>
          </aside>
        ) : null}
      </div>
    </section>
  );
}
