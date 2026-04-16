import { FormEvent, useMemo, useState } from 'react';
import { useUsers } from '../hooks/useUsers';
import { useRoles } from '../hooks/useRoles';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { User, Role } from '../lib/api';
import './Users.css';

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

export function Users(): JSX.Element {
  const api = useApiClient();
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [isEditingRoles, setIsEditingRoles] = useState(false);
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [selectedUserIds, setSelectedUserIds] = useState<Set<string>>(new Set());
  const [isBulkAssigning, setIsBulkAssigning] = useState(false);
  const [bulkAssignRoles, setBulkAssignRoles] = useState<string[]>([]);

  const {
    data: users,
    loading: usersLoading,
    error: usersError,
    pagination,
    reload: reloadUsers,
  } = useUsers({ limit, offset });

  const {
    data: roles,
    loading: rolesLoading,
    error: rolesError,
    reload: reloadRoles,
  } = useRoles();

  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [updating, setUpdating] = useState(false);

  const roleMap = useMemo(() => {
    const map = new Map<string, Role>();
    roles.forEach((role) => map.set(role.name, role));
    return map;
  }, [roles]);

  const handleEditRoles = (user: User) => {
    setSelectedUser(user);
    setSelectedRoles([...user.roles]);
    setIsEditingRoles(true);
    resetFeedback();
  };

  const handleCancelEdit = () => {
    setIsEditingRoles(false);
    setSelectedUser(null);
    setSelectedRoles([]);
    resetFeedback();
  };

  const handleRoleToggle = (roleName: string) => {
    setSelectedRoles((prev) => {
      if (prev.includes(roleName)) {
        return prev.filter((r) => r !== roleName);
      }
      return [...prev, roleName];
    });
  };

  const handleSaveRoles = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!selectedUser) return;

    setUpdating(true);
    resetFeedback();

    try {
      await api.updateUserRoles(selectedUser.id, { roles: selectedRoles });
      showSuccess('User roles updated successfully');
      setIsEditingRoles(false);
      setSelectedUser(null);
      setSelectedRoles([]);
      reloadUsers();
      reloadRoles();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to update user roles';
      showError(message);
      showToast(message, 'error');
    } finally {
      setUpdating(false);
    }
  };

  const handleRefresh = () => {
    reloadUsers();
    reloadRoles();
  };

  const handleSelectUser = (userId: string, checked: boolean) => {
    setSelectedUserIds((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(userId);
      } else {
        next.delete(userId);
      }
      return next;
    });
  };

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedUserIds(new Set(users.map((u) => u.id)));
    } else {
      setSelectedUserIds(new Set());
    }
  };

  const handleBulkAssignRoles = async () => {
    if (selectedUserIds.size === 0) {
      showToast('Please select at least one user', 'error');
      return;
    }
    if (bulkAssignRoles.length === 0) {
      showToast('Please select at least one role', 'error');
      return;
    }

    setIsBulkAssigning(true);
    resetFeedback();

    try {
      const userIds = Array.from(selectedUserIds);
      const promises = userIds.map((userId) => api.updateUserRoles(userId, { roles: bulkAssignRoles }));
      await Promise.all(promises);
      showSuccess(`Successfully assigned roles to ${userIds.length} user(s)`);
      setSelectedUserIds(new Set());
      setBulkAssignRoles([]);
      reloadUsers();
      reloadRoles();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to assign roles';
      showError(message);
      showToast(message, 'error');
    } finally {
      setIsBulkAssigning(false);
    }
  };

  return (
    <div className="users-page">
      <div className="page-header">
        <div>
          <h1>Users & Roles</h1>
          <p className="subtitle">Manage users and their role assignments</p>
        </div>
        <div className="page-actions">
          {selectedUserIds.size > 0 && (
            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
              <span style={{ fontSize: '0.9rem', color: 'var(--text-secondary)' }}>
                {selectedUserIds.size} selected
              </span>
              <button
                type="button"
                onClick={() => setIsBulkAssigning(true)}
                className="btn-primary"
                disabled={isBulkAssigning}
              >
                Bulk Assign Roles
              </button>
            </div>
          )}
          <button type="button" onClick={handleRefresh} className="btn-secondary">
            Refresh
          </button>
        </div>
      </div>

      {usersError && (
        <div className="error-banner">
          <p>Error loading users: {usersError}</p>
        </div>
      )}

      {rolesError && (
        <div className="error-banner">
          <p>Error loading roles: {rolesError}</p>
        </div>
      )}

      <div className="users-stats">
        <div className="stat-card">
          <div className="stat-value">{pagination.total}</div>
          <div className="stat-label">Total Users</div>
        </div>
        <div className="stat-card">
          <div className="stat-value">{roles.length}</div>
          <div className="stat-label">Available Roles</div>
        </div>
        <div className="stat-card">
          <div className="stat-value">
            {users.filter((u) => u.roles && u.roles.length > 0).length}
          </div>
          <div className="stat-label">Users with Roles</div>
        </div>
      </div>

      <div className="content-grid">
        <div className="users-section">
          <div className="section-header">
            <h2>Users</h2>
            <div className="results-count">
              Showing {users.length} of {pagination.total}
            </div>
          </div>

          {usersLoading ? (
            <div className="loading-placeholder">Loading users...</div>
          ) : users.length === 0 ? (
            <div className="empty-state">
              <p>No users found.</p>
            </div>
          ) : (
            <>
              <div className="table-container">
                <table className="users-table">
                  <thead>
                    <tr>
                      <th style={{ width: '40px' }}>
                        <input
                          type="checkbox"
                          checked={selectedUserIds.size === users.length && users.length > 0}
                          onChange={(e) => handleSelectAll(e.target.checked)}
                          title="Select all"
                        />
                      </th>
                      <th>User</th>
                      <th>Email</th>
                      <th>Roles</th>
                      <th>Created</th>
                      <th>Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {users.map((user) => (
                      <tr key={user.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={selectedUserIds.has(user.id)}
                            onChange={(e) => handleSelectUser(user.id, e.target.checked)}
                          />
                        </td>
                        <td>
                          <div className="user-info">
                            <div className="user-name">
                              {user.display_name || user.external_id}
                            </div>
                            <div className="user-id">ID: {user.id.slice(0, 8)}...</div>
                          </div>
                        </td>
                        <td>{user.email || '—'}</td>
                        <td>
                          <div className="roles-list">
                            {user.roles && user.roles.length > 0 ? (
                              user.roles.map((roleName) => {
                                const role = roleMap.get(roleName);
                                return (
                                  <span key={roleName} className="role-badge" title={role?.description}>
                                    {roleName}
                                  </span>
                                );
                              })
                            ) : (
                              <span className="no-roles">No roles assigned</span>
                            )}
                          </div>
                        </td>
                        <td>{formatDate(user.created_at)}</td>
                        <td>
                          <button
                            type="button"
                            onClick={() => handleEditRoles(user)}
                            className="btn-link"
                          >
                            Edit Roles
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              <div className="pagination">
                <button
                  type="button"
                  onClick={() => setOffset(Math.max(0, offset - limit))}
                  disabled={offset === 0 || usersLoading}
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
                  disabled={offset + limit >= pagination.total || usersLoading}
                  className="btn-secondary"
                >
                  Next
                </button>
              </div>
            </>
          )}
        </div>

        <div className="roles-section">
          <div className="section-header">
            <h2>Available Roles</h2>
          </div>

          {rolesLoading ? (
            <div className="loading-placeholder">Loading roles...</div>
          ) : roles.length === 0 ? (
            <div className="empty-state">
              <p>No roles found.</p>
            </div>
          ) : (
            <div className="roles-list-container">
              {roles.map((role) => (
                <div key={role.id} className="role-card">
                  <div className="role-header">
                    <h3 className="role-name">{role.name}</h3>
                  </div>
                  {role.description && (
                    <p className="role-description">{role.description}</p>
                  )}
                  <div className="role-meta">
                    <span className="role-users-count">
                      {users.filter((u) => u.roles?.includes(role.name)).length} user(s)
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {isEditingRoles && selectedUser && (
        <div className="modal-overlay" onClick={handleCancelEdit}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2>Edit Roles for {selectedUser.display_name || selectedUser.external_id}</h2>
              <button type="button" onClick={handleCancelEdit} className="modal-close">
                ×
              </button>
            </div>

            <form onSubmit={handleSaveRoles}>
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

                <div className="roles-selection">
                  <p className="selection-hint">Select roles to assign to this user:</p>
                  <div className="roles-checkboxes">
                    {roles.map((role) => (
                      <label key={role.id} className="role-checkbox">
                        <input
                          type="checkbox"
                          checked={selectedRoles.includes(role.name)}
                          onChange={() => handleRoleToggle(role.name)}
                        />
                        <div className="checkbox-content">
                          <span className="checkbox-role-name">{role.name}</span>
                          {role.description && (
                            <span className="checkbox-role-desc">{role.description}</span>
                          )}
                        </div>
                      </label>
                    ))}
                  </div>
                </div>
              </div>

              <div className="modal-footer">
                <button type="button" onClick={handleCancelEdit} className="btn-secondary" disabled={updating}>
                  Cancel
                </button>
                <button type="submit" className="btn-primary" disabled={updating}>
                  {updating ? 'Saving...' : 'Save Changes'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {isBulkAssigning && (
        <div className="modal-overlay" onClick={() => setIsBulkAssigning(false)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2>Bulk Assign Roles</h2>
              <button type="button" onClick={() => setIsBulkAssigning(false)} className="modal-close">
                ×
              </button>
            </div>

            <div className="modal-body">
              <p className="selection-hint">
                Assign roles to {selectedUserIds.size} selected user(s):
              </p>
              <div className="roles-checkboxes">
                {roles.map((role) => (
                  <label key={role.id} className="role-checkbox">
                    <input
                      type="checkbox"
                      checked={bulkAssignRoles.includes(role.name)}
                      onChange={() => {
                        setBulkAssignRoles((prev) =>
                          prev.includes(role.name)
                            ? prev.filter((r) => r !== role.name)
                            : [...prev, role.name]
                        );
                      }}
                    />
                    <div className="checkbox-content">
                      <span className="checkbox-role-name">{role.name}</span>
                      {role.description && (
                        <span className="checkbox-role-desc">{role.description}</span>
                      )}
                    </div>
                  </label>
                ))}
              </div>
            </div>

            <div className="modal-footer">
              <button
                type="button"
                onClick={() => {
                  setIsBulkAssigning(false);
                  setBulkAssignRoles([]);
                }}
                className="btn-secondary"
                disabled={isBulkAssigning}
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleBulkAssignRoles}
                className="btn-primary"
                disabled={isBulkAssigning || bulkAssignRoles.length === 0}
              >
                {isBulkAssigning ? 'Assigning...' : `Assign to ${selectedUserIds.size} User(s)`}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

