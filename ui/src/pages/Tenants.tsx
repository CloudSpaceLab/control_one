import { FormEvent, useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { 
  EnterpriseLayout, 
  ExecutiveOverview, 
  ManagementPanel, 
  ActionZone,
  ContentGrid 
} from '../components/EnterpriseLayout';
import '../components/EnterpriseLayout.css';

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function Tenants(): JSX.Element {
  const api = useApiClient();
  const [limit] = useState(20);
  const [nameFilter, setNameFilter] = useState('');
  const { data, pagination, loading, reload } = useTenants({
    limit,
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
    const trimmedName = tenantName.trim();
    if (!trimmedName) {
      showError('Tenant name is required');
      return;
    }

    setSubmitting(true);
    reset();

    try {
      await api.createTenant({ name: trimmedName });
      showSuccess(`Tenant "${trimmedName}" created successfully.`);
      showToast(`Tenant "${trimmedName}" created successfully.`, 'success');
      setTenantName('');
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create tenant.';
      showError(message);
      showToast(message, 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const handleRenameTenant = async () => {
    if (!selectedTenant || !renameValue.trim()) {
      showError('Tenant name is required');
      return;
    }

    setRenaming(true);
    try {
      await api.updateTenant(selectedTenant.id, { name: renameValue.trim() });
      showSuccess(`Tenant renamed to "${renameValue.trim()}".`);
      showToast('Tenant renamed successfully.', 'success');
      setRenameValue('');
      setSelectedTenantId(null);
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to rename tenant.';
      showError(message);
      showToast(message, 'error');
    } finally {
      setRenaming(false);
    }
  };

  const handleDeleteTenant = async () => {
    if (!selectedTenant) return;
    const confirmed = window.confirm(`Delete tenant "${selectedTenant.name}"? This action cannot be undone.`);
    if (!confirmed) return;

    setDeleting(true);
    try {
      await api.deleteTenant(selectedTenant.id);
      showSuccess(`Tenant "${selectedTenant.name}" deleted successfully.`);
      showToast('Tenant deleted successfully.', 'success');
      setSelectedTenantId(null);
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to delete tenant.';
      showError(message);
      showToast(message, 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <EnterpriseLayout variant="management">
      {/* Executive Overview */}
      <ExecutiveOverview 
        title="🏢 Tenant Management"
        subtitle="Manage workspaces and environments for your organization"
      >
        <article className="stat-card">
          <span className="muted">Total Tenants</span>
          <strong>{summary.total}</strong>
          <small className="muted">All environments</small>
        </article>
        <article className="stat-card">
          <span className="muted">Newest Tenant</span>
          <strong>{summary.newestName}</strong>
          <small className="muted">{summary.newestDate}</small>
        </article>
        <article className="stat-card">
          <span className="muted">Active Now</span>
          <strong>{rows.length}</strong>
          <small className="muted">Currently loaded</small>
        </article>
        <article className="stat-card">
          <span className="muted">Status</span>
          <strong>Healthy</strong>
          <small className="muted">All systems operational</small>
        </article>
      </ExecutiveOverview>

      <div className="management-dashboard">
        {/* Main Content Area */}
        <div className="management-main">
          {/* Create Tenant */}
          <ManagementPanel 
            title="Create New Tenant"
            icon="➕"
            subtitle="Add a new workspace or environment"
            position="primary"
          >
            <form onSubmit={handleCreateTenant}>
              <ContentGrid columns={1} gap="md">
                <div className="form-field">
                  <label htmlFor="tenant-name">Tenant Name</label>
                  <input
                    id="tenant-name"
                    type="text"
                    value={tenantName}
                    onChange={(e) => setTenantName(e.target.value)}
                    placeholder="e.g. Production Environment"
                    disabled={submitting}
                    required
                  />
                </div>
              </ContentGrid>
              {formError && <div className="form-error">{formError}</div>}
              {formSuccess && <div className="form-success">{formSuccess}</div>}
              <ActionZone alignment="right" variant="primary">
                <button type="submit" className="primary-button" disabled={submitting}>
                  {submitting ? 'Creating…' : 'Create Tenant'}
                </button>
              </ActionZone>
            </form>
          </ManagementPanel>

          {/* Tenant List */}
          <ManagementPanel 
            title="📋 Tenant Registry"
            subtitle="Manage and monitor all tenant environments"
            position="primary"
          >
            {loading ? (
              <p className="muted">Loading tenants…</p>
            ) : rows.length === 0 ? (
              <div className="empty-state">
                <p>No tenants found. Create your first tenant to get started.</p>
              </div>
            ) : (
              <div className="tenant-list">
                {rows.map((tenant) => (
                  <div key={tenant.id} className="tenant-card">
                    <header>
                      <h3>{tenant.name}</h3>
                      <ActionZone alignment="right" variant="secondary">
                        <button
                          type="button"
                          className="ghost-button"
                          onClick={() => {
                            setSelectedTenantId(tenant.id);
                            setRenameValue(tenant.name);
                          }}
                          disabled={renaming || deleting}
                        >
                          Manage
                        </button>
                      </ActionZone>
                    </header>
                    <dl>
                      <dt>ID</dt>
                      <dd>{tenant.id}</dd>
                      <dt>Created</dt>
                      <dd>{formatDate(tenant.created_at)}</dd>
                    </dl>
                  </div>
                ))}
              </div>
            )}
          </ManagementPanel>
        </div>

        {/* Sidebar */}
        <div className="management-sidebar">
          {/* Filters */}
          <ManagementPanel 
            title="Filters"
            icon="🔍"
            subtitle={`${rows.length} tenants shown`}
            position="secondary"
          >
            <ContentGrid columns={1} gap="md">
              <div className="form-field">
                <label htmlFor="name-filter">Filter by name</label>
                <input
                  id="name-filter"
                  type="text"
                  value={nameFilter}
                  onChange={(e) => setNameFilter(e.target.value)}
                  placeholder="e.g. Production"
                />
              </div>
            </ContentGrid>
          </ManagementPanel>

          {/* Tenant Management */}
          {selectedTenant && (
            <ManagementPanel 
              title={`Manage: ${selectedTenant.name}`}
              icon="⚙️"
              subtitle="Update tenant configuration"
              position="secondary"
            >
              <div className="tenant-management">
                <ContentGrid columns={1} gap="md">
                  <div className="form-field">
                    <label htmlFor="rename-tenant">Rename Tenant</label>
                    <input
                      id="rename-tenant"
                      type="text"
                      value={renameValue}
                      onChange={(e) => setRenameValue(e.target.value)}
                      placeholder="New tenant name"
                      disabled={renaming}
                    />
                  </div>
                </ContentGrid>
                <ActionZone alignment="right" variant="primary">
                  <button
                    type="button"
                    className="primary-button"
                    onClick={handleRenameTenant}
                    disabled={renaming || !renameValue.trim()}
                  >
                    {renaming ? 'Renaming…' : 'Rename Tenant'}
                  </button>
                  <button
                    type="button"
                    className="danger-button"
                    onClick={handleDeleteTenant}
                    disabled={deleting || renaming}
                  >
                    {deleting ? 'Deleting…' : 'Delete Tenant'}
                  </button>
                  <button
                    type="button"
                    className="ghost-button"
                    onClick={() => setSelectedTenantId(null)}
                  >
                    Close
                  </button>
                </ActionZone>
              </div>
            </ManagementPanel>
          )}
        </div>
      </div>
    </EnterpriseLayout>
  );
}
