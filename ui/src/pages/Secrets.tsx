import { FormEvent, useState } from 'react';
import { useSecretGroups, useSecretSyncs } from '../hooks/useSecrets';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { SecretGroup, CreateSecretGroupPayload } from '../lib/api';
import './Secrets.css';

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

export function Secrets(): JSX.Element {
  const api = useApiClient();
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [isSyncing, setIsSyncing] = useState(false);

  const { data: tenants } = useTenants();
  const {
    data: groups,
    loading: groupsLoading,
    error: groupsError,
    pagination,
    reload: reloadGroups,
  } = useSecretGroups({
    tenant_id: selectedTenant,
    limit,
    offset,
  });

  const {
    data: syncs,
    loading: syncsLoading,
    error: syncsError,
    reload: reloadSyncs,
  } = useSecretSyncs(selectedGroupId, { limit: 20, offset: 0 });

  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [saving, setSaving] = useState(false);

  const [formData, setFormData] = useState<CreateSecretGroupPayload>({
    name: '',
    backend: 'vault',
    endpoint: '',
    sync_interval_seconds: 900,
  });

  const handleCreate = () => {
    setIsCreating(true);
    setFormData({
      name: '',
      backend: 'vault',
      endpoint: '',
      sync_interval_seconds: 900,
    });
    resetFeedback();
  };

  const handleCancel = () => {
    setIsCreating(false);
    setFormData({
      name: '',
      backend: 'vault',
      endpoint: '',
      sync_interval_seconds: 900,
    });
    resetFeedback();
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!formData.name.trim()) {
      showError('Name is required');
      return;
    }

    setSaving(true);
    resetFeedback();

    try {
      const payload: CreateSecretGroupPayload = {
        ...formData,
        tenant_id: selectedTenant || undefined,
      };
      await api.createSecretGroup(payload);
      showSuccess('Secret group created successfully');
      setIsCreating(false);
      reloadGroups();
    } catch (error: any) {
      const message = error?.message || 'Failed to create secret group';
      showError(message);
      showToast(message, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (groupId: string) => {
    if (!confirm('Are you sure you want to delete this secret group?')) {
      return;
    }

    try {
      await api.deleteSecretGroup(groupId);
      showToast('Secret group deleted successfully', 'success');
      reloadGroups();
      if (selectedGroupId === groupId) {
        setSelectedGroupId(null);
      }
    } catch (error: any) {
      const message = error?.message || 'Failed to delete secret group';
      showToast(message, 'error');
    }
  };

  const handleSync = async (groupId: string) => {
    setIsSyncing(true);
    try {
      await api.syncSecretGroup(groupId);
      showToast('Secret sync triggered successfully', 'success');
      reloadGroups();
      if (selectedGroupId === groupId) {
        reloadSyncs();
      }
    } catch (error: any) {
      const message = error?.message || 'Failed to sync secrets';
      showToast(message, 'error');
    } finally {
      setIsSyncing(false);
    }
  };

  const selectedGroup = groups.find((g) => g.id === selectedGroupId) || null;

  return (
    <div className="secrets-page">
      <div className="page-header">
        <div>
          <h1>Secrets Management</h1>
          <p className="subtitle">Manage secret groups and sync status</p>
        </div>
        <div className="page-actions">
          <button type="button" onClick={handleCreate} className="btn-primary">
            Create Secret Group
          </button>
        </div>
      </div>

      {groupsError && (
        <div className="error-banner">
          <p>Error loading secret groups: {groupsError}</p>
        </div>
      )}

      <div className="secrets-stats">
        <div className="stat-card">
          <div className="stat-value">{pagination.total}</div>
          <div className="stat-label">Total Groups</div>
        </div>
        <div className="stat-card">
          <div className="stat-value">
            {groups.filter((g) => g.sync_status === 'success').length}
          </div>
          <div className="stat-label">Synced</div>
        </div>
        <div className="stat-card">
          <div className="stat-value">
            {groups.filter((g) => g.sync_status === 'failed').length}
          </div>
          <div className="stat-label">Failed</div>
        </div>
      </div>

      <div className="content-grid">
        <div className="groups-section">
          <div className="section-header">
            <h2>Secret Groups</h2>
            <div className="results-count">
              Showing {groups.length} of {pagination.total}
            </div>
          </div>

          {groupsLoading ? (
            <div className="loading-placeholder">Loading secret groups...</div>
          ) : groups.length === 0 ? (
            <div className="empty-state">
              <p>No secret groups found.</p>
            </div>
          ) : (
            <div className="table-container">
              <table className="groups-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Backend</th>
                    <th>Sync Status</th>
                    <th>Last Sync</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {groups.map((group) => (
                    <tr
                      key={group.id}
                      className={selectedGroupId === group.id ? 'selected' : ''}
                      onClick={() => setSelectedGroupId(group.id)}
                    >
                      <td>
                        <div className="group-name">{group.name}</div>
                        {group.endpoint && (
                          <div className="group-endpoint">{group.endpoint}</div>
                        )}
                      </td>
                      <td>
                        <span className="backend-badge">{group.backend}</span>
                      </td>
                      <td>
                        <span className={`status-pill status-${group.sync_status}`}>
                          {group.sync_status}
                        </span>
                        {group.sync_error && (
                          <div className="sync-error" title={group.sync_error}>
                            ⚠️
                          </div>
                        )}
                      </td>
                      <td>{formatDate(group.last_sync_at)}</td>
                      <td>
                        <div className="action-buttons">
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation();
                              handleSync(group.id);
                            }}
                            className="btn-link"
                            disabled={isSyncing}
                          >
                            Sync
                          </button>
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation();
                              handleDelete(group.id);
                            }}
                            className="btn-link danger"
                          >
                            Delete
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <div className="pagination">
            <button
              type="button"
              onClick={() => setOffset(Math.max(0, offset - limit))}
              disabled={offset === 0 || groupsLoading}
              className="btn-secondary"
            >
              Previous
            </button>
            <span className="pagination-info">
              Page {Math.floor(offset / limit) + 1} of {Math.ceil(pagination.total / limit) || 1}
            </span>
            <button
              type="button"
              onClick={() => setOffset(offset + limit)}
              disabled={offset + limit >= pagination.total || groupsLoading}
              className="btn-secondary"
            >
              Next
            </button>
          </div>
        </div>

        {selectedGroup && (
          <div className="syncs-section">
            <div className="section-header">
              <h2>Sync History</h2>
              <button
                type="button"
                onClick={() => setSelectedGroupId(null)}
                className="btn-link"
              >
                Close
              </button>
            </div>

            <div className="group-details">
              <h3>{selectedGroup.name}</h3>
              <dl>
                <dt>Backend</dt>
                <dd>{selectedGroup.backend}</dd>
                <dt>Endpoint</dt>
                <dd>{selectedGroup.endpoint || '—'}</dd>
                <dt>Sync Status</dt>
                <dd>
                  <span className={`status-pill status-${selectedGroup.sync_status}`}>
                    {selectedGroup.sync_status}
                  </span>
                </dd>
                <dt>Last Sync</dt>
                <dd>{formatDate(selectedGroup.last_sync_at)}</dd>
              </dl>
            </div>

            {syncsLoading ? (
              <div className="loading-placeholder">Loading sync history...</div>
            ) : syncsError ? (
              <div className="error-banner">
                <p>Error loading sync history: {syncsError}</p>
              </div>
            ) : syncs.length === 0 ? (
              <div className="empty-state">
                <p>No sync history available.</p>
              </div>
            ) : (
              <div className="table-container">
                <table className="syncs-table">
                  <thead>
                    <tr>
                      <th>Secret Path</th>
                      <th>Version</th>
                      <th>Status</th>
                      <th>Synced At</th>
                    </tr>
                  </thead>
                  <tbody>
                    {syncs.map((sync) => (
                      <tr key={sync.id}>
                        <td>{sync.secret_path}</td>
                        <td>{sync.secret_version || '—'}</td>
                        <td>
                          <span className={`status-pill status-${sync.sync_status}`}>
                            {sync.sync_status}
                          </span>
                        </td>
                        <td>{formatDate(sync.synced_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </div>

      {isCreating && (
        <div className="modal-overlay" onClick={handleCancel}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2>Create Secret Group</h2>
              <button type="button" onClick={handleCancel} className="modal-close">
                ×
              </button>
            </div>

            <form onSubmit={handleSubmit}>
              <div className="modal-body">
                {formError && (
                  <div className="error-banner">
                    <p>{formError}</p>
                  </div>
                )}

                {formSuccess && (
                  <div className="success-banner">
                    <p>{formSuccess}</p>
                  </div>
                )}

                <div className="form-group">
                  <label htmlFor="name">Name *</label>
                  <input
                    id="name"
                    type="text"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    required
                  />
                </div>

                <div className="form-group">
                  <label htmlFor="backend">Backend *</label>
                  <select
                    id="backend"
                    value={formData.backend}
                    onChange={(e) => setFormData({ ...formData, backend: e.target.value })}
                    required
                  >
                    <option value="vault">Vault</option>
                  </select>
                </div>

                <div className="form-group">
                  <label htmlFor="endpoint">Endpoint</label>
                  <input
                    id="endpoint"
                    type="text"
                    value={formData.endpoint}
                    onChange={(e) => setFormData({ ...formData, endpoint: e.target.value })}
                    placeholder="secret/data/app"
                  />
                </div>

                <div className="form-group">
                  <label htmlFor="sync_interval">Sync Interval (seconds)</label>
                  <input
                    id="sync_interval"
                    type="number"
                    value={formData.sync_interval_seconds}
                    onChange={(e) =>
                      setFormData({
                        ...formData,
                        sync_interval_seconds: parseInt(e.target.value, 10) || 900,
                      })
                    }
                    min="60"
                  />
                </div>
              </div>

              <div className="modal-footer">
                <button
                  type="button"
                  onClick={handleCancel}
                  className="btn-secondary"
                  disabled={saving}
                >
                  Cancel
                </button>
                <button type="submit" className="btn-primary" disabled={saving}>
                  {saving ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

