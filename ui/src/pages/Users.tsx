import { GlobalRoleSources } from '../features/patch-management/GlobalRoleSources';
import { FormEvent, useEffect, useMemo, useState } from 'react';
import { RefreshCw, Users as UsersIcon, X } from 'lucide-react';
import { useUsers } from '../hooks/useUsers';
import { useRoles } from '../hooks/useRoles';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { User, Role, APIError } from '../lib/api';
import { Button } from '../components/ui/button';
import {
  DataTable,
  EmptyState,
  KpiTile,
  Panel,
  SectionHeader,
  StatusTag,
} from '../components/kit';
import type { ColumnDef } from '@tanstack/react-table';

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

function uniqueRoleNames(roleNames?: string[]): string[] {
  const seen = new Set<string>();
  const unique: string[] = [];
  roleNames?.forEach((roleName) => {
    const trimmed = roleName.trim();
    const key = trimmed.toLowerCase();
    if (!trimmed || seen.has(key)) return;
    seen.add(key);
    unique.push(trimmed);
  });
  return unique;
}

export function Users(): JSX.Element {
  const api = useApiClient();
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [isEditingRoles, setIsEditingRoles] = useState(false);
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [selectedUserIds, setSelectedUserIds] = useState<Set<string>>(new Set());
  const [isBulkRoleModalOpen, setIsBulkRoleModalOpen] = useState(false);
  const [bulkAssigning, setBulkAssigning] = useState(false);
  const [bulkAssignRoles, setBulkAssignRoles] = useState<string[]>([]);
  const [roleConflict, setRoleConflict] = useState(false);
  const [sourceReviewed, setSourceReviewed] = useState(false);
  const [bulkBaselines, setBulkBaselines] = useState<User[]>([]);
  const [bulkFailed, setBulkFailed] = useState(false);
  const [bulkError, setBulkError] = useState<string | null>(null);

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
    roles.forEach((role) => map.set(role.name.trim().toLowerCase(), role));
    return map;
  }, [roles]);

  useEffect(() => {
    const visibleUserIds = new Set(users.map((user) => user.id));
    setSelectedUserIds((prev) => new Set(Array.from(prev).filter((id) => visibleUserIds.has(id))));
  }, [users]);

  const handleEditRoles = (user: User) => {
    if ((user.role_scope !== 'global' || !user.role_revision)) { showError('Global role scope is unavailable. Refresh after the matching backend is deployed.'); return; }
    setRoleConflict(false);
    setSourceReviewed(false);
    setSelectedUser({...user, roles: [...user.roles]});
    setSelectedRoles(uniqueRoleNames(user.roles));
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
    if (!selectedUser || selectedUser.role_scope !== 'global' || !selectedUser.role_revision || roleConflict) return;

    setUpdating(true);
    resetFeedback();

    try {
      await api.updateUserRoles(selectedUser.id, { roles: selectedRoles, expected_revision: selectedUser.role_revision });
      showSuccess('User roles updated successfully');
      setIsEditingRoles(false);
      setSelectedUser(null);
      setSelectedRoles([]);
      reloadUsers();
      reloadRoles();
    } catch (error: unknown) {
      if (error instanceof APIError && (error.status === 409 || error.status === 403)) setRoleConflict(true);
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
    if (users.some(user => selectedUserIds.has(user.id) && (user.role_scope !== 'global' || !user.role_revision))) { setBulkError('Global role scope is unavailable for a selected account. Refresh before editing.'); return; }
    if (bulkFailed) return;
    if (selectedUserIds.size === 0) {
      showToast('Please select at least one user', 'error');
      return;
    }
    if (bulkAssignRoles.length === 0) {
      showToast('Please select at least one role', 'error');
      return;
    }

    setBulkAssigning(true);
    resetFeedback();
    setBulkError(null);

    try {
      const userIds = bulkBaselines.map(user => user.id);
      const results = await Promise.allSettled(bulkBaselines.map(user => api.updateUserRoles(user.id, { roles: bulkAssignRoles, expected_revision: user.role_revision! })));
      const failed = results.filter(result => result.status === 'rejected');
      if (failed.length) {
        const reason = failed[0].status === 'rejected' ? failed[0].reason : null;
        throw new Error(`${results.length - failed.length} saved; ${failed.length} failed. ${reason instanceof Error ? reason.message : 'Role update failed.'} Cancel, refresh the directory and review before retrying.`);
      }
      showSuccess(`Successfully replaced roles for ${userIds.length} user(s)`);
      setSelectedUserIds(new Set());
      setBulkAssignRoles([]);
      setIsBulkRoleModalOpen(false);
      reloadUsers();
      reloadRoles();
    } catch (error: unknown) {
      setBulkFailed(true);
      const message = error instanceof Error ? error.message : 'Failed to assign roles';
      setBulkError(message);
      showError(message);
      showToast(message, 'error');
    } finally {
      setBulkAssigning(false);
    }
  };

  const columns = useMemo<ColumnDef<User>[]>(() => [
    {
      id: 'select',
      header: () => (
        <input
          type="checkbox"
          className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
          aria-label="Select all users"
          checked={selectedUserIds.size === users.length && users.length > 0}
          onChange={(e) => handleSelectAll(e.target.checked)}
          title="Select all"
        />
      ),
      cell: ({ row }) => (
        <input
          type="checkbox"
          className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
          aria-label={`Select ${row.original.display_name || row.original.external_id || row.original.id}`}
          checked={selectedUserIds.has(row.original.id)}
          onChange={(e) => handleSelectUser(row.original.id, e.target.checked)}
        />
      ),
    },
    {
      id: 'user',
      header: 'User',
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="font-medium">{row.original.display_name || row.original.external_id}</span>
          <span className="font-mono text-[0.65rem] text-text-muted">ID: {row.original.id.slice(0, 8)}...</span>
        </div>
      ),
    },
    {
      accessorKey: 'email',
      header: 'Email',
      cell: ({ getValue }) => <span className="text-sm">{(getValue() as string) || '—'}</span>,
    },
    {
      id: 'roles',
      header: 'Global roles',
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          {(row.original.role_scope !== 'global' || !row.original.role_revision) ? <span className="text-xs text-text-muted">Global role scope unavailable</span> : row.original.roles && row.original.roles.length > 0 ? (
            uniqueRoleNames(row.original.roles).map((roleName) => {
              const role = roleMap.get(roleName.toLowerCase());
              return (
                <StatusTag key={roleName} tone="info" {...(role?.description ? { title: role.description } : {})}>
                  {roleName}
                </StatusTag>
              );
            })
          ) : (
            <span className="text-xs text-text-muted">No roles assigned</span>
          )}
        </div>
      ),
    },
    {
      accessorKey: 'created_at',
      header: 'Created',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums">{formatDate(getValue() as string)}</span>
      ),
    },
    {
      id: 'actions',
      header: 'Actions',
      cell: ({ row }) => (
        <Button variant="ghost" size="sm" disabled={(row.original.role_scope !== 'global' || !row.original.role_revision)} onClick={() => handleEditRoles(row.original)}>
          Edit Roles
        </Button>
      ),
    },
  // eslint-disable-next-line react-hooks/exhaustive-deps
  ], [users, selectedUserIds, roleMap]);

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="GOVERNANCE · IDENTITY"
        title="Users & Roles"
        description="Manage existing accounts and global roles across tenants. Tenant role assignments are managed separately in Patch posture → Review policy & ownership."
        actions={
          <>
            {selectedUserIds.size > 0 && (
              <>
                <span className="text-sm text-text-secondary">{selectedUserIds.size} selected</span>
                <Button
                  variant="primary"
                  size="md"
                  onClick={() => {
                    setBulkError(null);
                    setBulkFailed(false);
                    setBulkBaselines(users.filter(user => selectedUserIds.has(user.id)).map(user => ({...user, roles: [...user.roles]})));
                    setIsBulkRoleModalOpen(true);
                  }}
                  disabled={bulkAssigning || users.some(user => selectedUserIds.has(user.id) && (user.role_scope !== 'global' || !user.role_revision))}
                >
                  Bulk Replace Roles
                </Button>
              </>
            )}
            <Button variant="secondary" size="md" onClick={handleRefresh} loading={usersLoading || rolesLoading}>
              <RefreshCw className={`h-4 w-4 ${usersLoading || rolesLoading ? 'animate-spin' : ''}`} /> {usersLoading || rolesLoading ? 'Refreshing…' : 'Refresh'}
            </Button>
          </>
        }
      />

      {usersError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load users">
          <p className="text-sm text-state-critical" role="alert">{usersError}</p>
        </Panel>
      )}
      {rolesError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load roles">
          <p className="text-sm text-state-critical" role="alert">{rolesError}</p>
        </Panel>
      )}
      {!isEditingRoles && formError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Role update failed">
          <p className="text-sm text-state-critical" role="alert">{formError}</p>
        </Panel>
      )}
      {!isEditingRoles && formSuccess && (
        <Panel padding="md" tone="inset" toneAccent="healthy" eyebrow="STATUS" title="Role update complete">
          <p className="text-sm text-state-healthy">{formSuccess}</p>
        </Panel>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="TOTAL USERS" value={pagination.total} tone="brand" />
        <KpiTile label="AVAILABLE ROLES" value={roles.length} tone="info" />
        <KpiTile
          label="USERS WITH ROLES"
          value={users.filter((u) => uniqueRoleNames(u.roles).length > 0).length}
          tone="healthy"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[2fr,1fr]">
        <Panel padding="sm" tone="inset" eyebrow={`USERS · ${users.length} of ${pagination.total}`} title="Directory">
          <DataTable
            columns={columns}
            rows={users}
            rowKey={(r) => r.id}
            loading={usersLoading}
            compact
            empty={
              usersError ? (
                <EmptyState
                  icon={<UsersIcon />}
                  title="Users could not be loaded"
                  description="Resolve the error above and refresh."
                />
              ) : (
                <EmptyState icon={<UsersIcon />} title="No users found" description="No users match the current filters." />
              )
            }
          />
          <div className="flex items-center justify-between gap-2 border-t border-border-subtle p-3">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setOffset(Math.max(0, offset - limit))}
              disabled={offset === 0 || usersLoading}
            >
              Previous
            </Button>
            <span className="font-mono text-xs text-text-muted">
              Page {Math.floor(offset / limit) + 1} of {Math.ceil(pagination.total / limit) || 1}
            </span>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setOffset(offset + limit)}
              disabled={offset + limit >= pagination.total || usersLoading}
            >
              Next
            </Button>
          </div>
        </Panel>

        <Panel padding="md" eyebrow="RBAC" title="Available Roles">
          {rolesLoading ? (
            <p className="text-sm text-text-muted">Loading roles...</p>
          ) : roles.length === 0 ? (
            <EmptyState
              title={rolesError ? 'Roles could not be loaded' : 'No roles found'}
              description={rolesError ? 'Resolve the error above and refresh.' : undefined}
            />
          ) : (
            <div className="flex flex-col gap-2">
              {roles.map((role) => (
                <div
                  key={role.id}
                  className="rounded-md border border-border-subtle bg-surface p-3 flex flex-col gap-1"
                >
                  <div className="flex items-center justify-between gap-2">
                    <h3 className="font-display text-sm font-semibold">{role.name}</h3>
                    <span className="text-[0.65rem] text-text-muted">
                      {users.filter((u) => uniqueRoleNames(u.roles).some((assigned) => assigned.toLowerCase() === role.name.toLowerCase())).length} user(s)
                    </span>
                  </div>
                  {role.description && (
                    <p className="text-xs text-text-secondary">{role.description}</p>
                  )}
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>

      {isEditingRoles && selectedUser && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="edit-user-roles-title"
          onClick={() => {
            if (!updating) handleCancelEdit();
          }}
        >
          <div
            className="w-full max-w-2xl max-h-[90vh] overflow-y-auto rounded-lg border border-border-subtle bg-elevated shadow-[var(--shadow-panel)]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="sticky top-0 z-10 flex items-start justify-between gap-4 border-b border-border-subtle bg-elevated p-5">
              <h2 id="edit-user-roles-title" className="min-w-0 break-words font-display text-base font-semibold">
                Edit Roles for {selectedUser.display_name || selectedUser.external_id}
              </h2>
              <Button
                variant="ghost"
                size="icon"
                onClick={handleCancelEdit}
                disabled={updating}
                aria-label="Close role editor"
              >
                <X className="h-4 w-4 text-foreground" />
              </Button>
            </div>

            <GlobalRoleSources key={selectedUser.id} userID={selectedUser.id} onReviewed={() => { setSourceReviewed(true); setRoleConflict(true); reloadUsers(); }} />
            <form onSubmit={handleSaveRoles}>
              <div className="flex flex-col gap-3 p-4">
                {formError && (
                  <p className="text-xs text-state-critical" role="alert">{formError}</p>
                )}
                {roleConflict && <p role={sourceReviewed ? "status" : "alert"} className="text-sm text-text-secondary">{sourceReviewed ? "Role source changed. Close and reopen this editor before changing global roles." : "This draft cannot be saved. Cancel, refresh the directory and review current roles and administrator access before opening a new draft."}</p>}
                {formSuccess && (
                  <p className="text-xs text-state-healthy">{formSuccess}</p>
                )}

                <p className="text-sm text-text-secondary">Select global roles for this user. These apply across tenants. Temporary tenant assignments are not included or changed here. If another administrator changes these roles, this draft will be rejected. Cancel and refresh the directory before reviewing a new draft.</p>
                <div className="flex flex-col gap-2 max-h-[400px] overflow-y-auto">
                  {roles.map((role) => (
                    <label
                      key={role.id}
                      className="flex items-start gap-2 rounded-md border border-border-subtle bg-surface p-2 cursor-pointer hover:border-border-strong"
                    >
                      <input
                        type="checkbox"
                        className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                        checked={selectedRoles.includes(role.name)}
                        onChange={() => handleRoleToggle(role.name)}
                      />
                      <div className="flex flex-col gap-0.5">
                        <span className="text-sm font-medium">{role.name}</span>
                        {role.description && (
                          <span className="text-xs text-text-secondary">{role.description}</span>
                        )}
                      </div>
                    </label>
                  ))}
                </div>
                {selectedRoles.length === 0 && (
                  <p className="text-xs text-state-warning">
                    At least one role is required for console access.
                  </p>
                )}
              </div>

              <div className="flex items-center justify-end gap-2 border-t border-border-subtle p-4">
                <Button variant="secondary" size="md" type="button" onClick={handleCancelEdit} disabled={updating}>
                  Cancel
                </Button>
                <Button variant="primary" size="md" type="submit" disabled={updating || roleConflict || selectedRoles.length === 0}>
                  {updating ? 'Saving...' : 'Save Changes'}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {isBulkRoleModalOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="bulk-replace-roles-title"
          onClick={() => {
            if (!bulkAssigning) {
              setIsBulkRoleModalOpen(false);
              setBulkError(null);
            }
          }}
        >
          <div
            className="w-full max-w-2xl max-h-[90vh] overflow-y-auto rounded-lg border border-border-subtle bg-elevated shadow-[var(--shadow-panel)]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="sticky top-0 z-10 flex items-start justify-between gap-4 border-b border-border-subtle bg-elevated p-5">
              <h2 id="bulk-replace-roles-title" className="min-w-0 break-words font-display text-base font-semibold">Bulk Replace Roles</h2>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => {
                  setIsBulkRoleModalOpen(false);
                  setBulkError(null);
                }}
                disabled={bulkAssigning}
                aria-label="Close bulk role editor"
              >
                <X className="h-4 w-4 text-foreground" />
              </Button>
            </div>

            <div className="flex flex-col gap-3 p-4">
              {bulkError ? (
                <p className="rounded-md border border-state-critical/40 bg-state-critical/10 px-3 py-2 text-sm text-state-critical" role="alert">
                  Bulk role update failed: {bulkError}
                </p>
              ) : null}
              <p className="text-sm text-text-secondary">
                This replaces global roles across tenants for {selectedUserIds.size} selected user(s). Tenant assignments remain unchanged:
              </p>
              <div className="flex flex-col gap-2 max-h-[400px] overflow-y-auto">
                {roles.map((role) => (
                  <label
                    key={role.id}
                    className="flex items-start gap-2 rounded-md border border-border-subtle bg-surface p-2 cursor-pointer hover:border-border-strong"
                  >
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                      checked={bulkAssignRoles.includes(role.name)}
                      onChange={() => {
                        setBulkAssignRoles((prev) =>
                          prev.includes(role.name)
                            ? prev.filter((r) => r !== role.name)
                            : [...prev, role.name],
                        );
                      }}
                    />
                    <div className="flex flex-col gap-0.5">
                      <span className="text-sm font-medium">{role.name}</span>
                      {role.description && (
                        <span className="text-xs text-text-secondary">{role.description}</span>
                      )}
                    </div>
                  </label>
                ))}
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 border-t border-border-subtle p-4">
              <Button
                variant="secondary"
                size="md"
                type="button"
                onClick={() => {
                  setIsBulkRoleModalOpen(false);
                  setBulkAssignRoles([]);
                  setBulkError(null);
                }}
                disabled={bulkAssigning}
              >
                Cancel
              </Button>
              <Button
                variant="primary"
                size="md"
                type="button"
                onClick={handleBulkAssignRoles}
                disabled={bulkAssigning || bulkFailed || bulkAssignRoles.length === 0}
              >
                {bulkAssigning ? 'Replacing...' : `Replace roles for ${selectedUserIds.size} User(s)`}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
