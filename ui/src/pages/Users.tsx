import { FormEvent, useMemo, useState } from 'react';
import { RefreshCw, Users as UsersIcon } from 'lucide-react';
import { useUsers } from '../hooks/useUsers';
import { useRoles } from '../hooks/useRoles';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { User, Role } from '../lib/api';
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
      header: 'Roles',
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          {row.original.roles && row.original.roles.length > 0 ? (
            row.original.roles.map((roleName) => {
              const role = roleMap.get(roleName);
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
        <Button variant="ghost" size="sm" onClick={() => handleEditRoles(row.original)}>
          Edit Roles
        </Button>
      ),
    },
  ], [users, selectedUserIds, roleMap]);

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="GOVERNANCE · IDENTITY"
        title="Users & Roles"
        description="Manage users and their role assignments."
        actions={
          <>
            {selectedUserIds.size > 0 && (
              <>
                <span className="text-sm text-text-secondary">{selectedUserIds.size} selected</span>
                <Button
                  variant="primary"
                  size="md"
                  onClick={() => setIsBulkAssigning(true)}
                  disabled={isBulkAssigning}
                >
                  Bulk Assign Roles
                </Button>
              </>
            )}
            <Button variant="secondary" size="md" onClick={handleRefresh}>
              <RefreshCw className="h-4 w-4" /> Refresh
            </Button>
          </>
        }
      />

      {usersError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load users">
          <p className="text-sm text-state-critical">{usersError}</p>
        </Panel>
      )}
      {rolesError && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load roles">
          <p className="text-sm text-state-critical">{rolesError}</p>
        </Panel>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="TOTAL USERS" value={pagination.total} tone="brand" />
        <KpiTile label="AVAILABLE ROLES" value={roles.length} tone="info" />
        <KpiTile
          label="USERS WITH ROLES"
          value={users.filter((u) => u.roles && u.roles.length > 0).length}
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
              <EmptyState icon={<UsersIcon />} title="No users found" description="No users match the current filters." />
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
            <EmptyState title="No roles found" />
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
                      {users.filter((u) => u.roles?.includes(role.name)).length} user(s)
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
          onClick={handleCancelEdit}
        >
          <div
            className="w-full max-w-lg rounded-lg border border-border-subtle bg-elevated shadow-[var(--shadow-panel)]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-border-subtle p-4">
              <h2 className="font-display text-base font-semibold">
                Edit Roles for {selectedUser.display_name || selectedUser.external_id}
              </h2>
              <Button variant="ghost" size="sm" onClick={handleCancelEdit}>
                ×
              </Button>
            </div>

            <form onSubmit={handleSaveRoles}>
              <div className="flex flex-col gap-3 p-4">
                {formError && (
                  <p className="text-xs text-state-critical">{formError}</p>
                )}
                {formSuccess && (
                  <p className="text-xs text-state-healthy">{formSuccess}</p>
                )}

                <p className="text-sm text-text-secondary">Select roles to assign to this user:</p>
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
              </div>

              <div className="flex items-center justify-end gap-2 border-t border-border-subtle p-4">
                <Button variant="secondary" size="md" type="button" onClick={handleCancelEdit} disabled={updating}>
                  Cancel
                </Button>
                <Button variant="primary" size="md" type="submit" disabled={updating}>
                  {updating ? 'Saving...' : 'Save Changes'}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {isBulkAssigning && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
          onClick={() => setIsBulkAssigning(false)}
        >
          <div
            className="w-full max-w-lg rounded-lg border border-border-subtle bg-elevated shadow-[var(--shadow-panel)]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-border-subtle p-4">
              <h2 className="font-display text-base font-semibold">Bulk Assign Roles</h2>
              <Button variant="ghost" size="sm" onClick={() => setIsBulkAssigning(false)}>
                ×
              </Button>
            </div>

            <div className="flex flex-col gap-3 p-4">
              <p className="text-sm text-text-secondary">
                Assign roles to {selectedUserIds.size} selected user(s):
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
                  setIsBulkAssigning(false);
                  setBulkAssignRoles([]);
                }}
                disabled={isBulkAssigning}
              >
                Cancel
              </Button>
              <Button
                variant="primary"
                size="md"
                type="button"
                onClick={handleBulkAssignRoles}
                disabled={isBulkAssigning || bulkAssignRoles.length === 0}
              >
                {isBulkAssigning ? 'Assigning...' : `Assign to ${selectedUserIds.size} User(s)`}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
