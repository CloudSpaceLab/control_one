import { Fragment, FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { ShieldCheck, Trash2 } from 'lucide-react';
import { useApiClient } from '../hooks/useApiClient';
import type { Permission, RoleWithPermissions } from '../lib/api';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import {
  EmptyState,
  KpiTile,
  Panel,
  SectionHeader,
  StatusTag,
} from '../components/kit';
import { ConfirmModal } from '../components/ConfirmModal';

// CISO admin's RBAC console.
//
//  - Lists every role, every permission, and a checkbox grid showing
//    which role has which permission.
//  - Toggling a custom-role checkbox writes through PUT /api/v1/roles/{id}/permissions
//    immediately so the change is live for next-request.
//  - Custom roles can be created (admin-only) at the top.
//  - Built-in roles are read-only baselines and cannot be deleted.

const BUILTIN_ROLE_NAMES = new Set(['admin', 'ciso', 'investigator', 'operator', 'viewer']);

function isBuiltInRole(role: Pick<RoleWithPermissions, 'name' | 'built_in'>): boolean {
  if (typeof role.built_in === 'boolean') return role.built_in;
  return BUILTIN_ROLE_NAMES.has(role.name.trim().toLowerCase());
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function Roles(): JSX.Element {
  const client = useApiClient();
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [roles, setRoles] = useState<RoleWithPermissions[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [operationError, setOperationError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [updatingPermissionKey, setUpdatingPermissionKey] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<RoleWithPermissions | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [perms, rs] = await Promise.all([
        client.listPermissions(),
        client.listRolesWithPermissions(),
      ]);
      setPermissions(perms);
      setRoles(rs);
      setLoadError(null);
    } catch (e) {
      setPermissions([]);
      setRoles([]);
      setLoadError(errorMessage(e, 'Failed to load roles.'));
    } finally {
      setLoading(false);
    }
  }, [client]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const grouped = useMemo(() => {
    const out: Record<string, Permission[]> = {};
    permissions.forEach((p) => {
      const key = p.category || 'general';
      if (!out[key]) out[key] = [];
      out[key].push(p);
    });
    return out;
  }, [permissions]);

  const togglePermission = async (role: RoleWithPermissions, perm: string, checked: boolean) => {
    if (updatingPermissionKey) return;
    const permissionKey = `${role.id}:${perm}`;
    const next = checked
      ? Array.from(new Set([...role.permissions, perm]))
      : role.permissions.filter((p) => p !== perm);
    setUpdatingPermissionKey(permissionKey);
    setOperationError(null);
    setRoles((cur) => cur.map((r) => (r.id === role.id ? { ...r, permissions: next } : r)));
    try {
      await client.setRolePermissions(role.id, next);
    } catch (e) {
      setOperationError(`Permission update failed for ${role.name}: ${errorMessage(e, 'Update failed.')}`);
      setRoles((cur) => cur.map((r) => (r.id === role.id ? { ...r, permissions: role.permissions } : r)));
    } finally {
      setUpdatingPermissionKey(null);
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget || isBuiltInRole(deleteTarget)) return;
    setDeleting(true);
    setDeleteError(null);
    setOperationError(null);
    try {
      await client.deleteRole(deleteTarget.id);
      setDeleteTarget(null);
      await refresh();
    } catch (e) {
      setDeleteError(errorMessage(e, 'Delete failed.'));
    } finally {
      setDeleting(false);
    }
  };

  const builtinCount = roles.filter((r) => isBuiltInRole(r)).length;
  const customCount = roles.length - builtinCount;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="GOVERNANCE · RBAC"
        title="Roles & permissions"
        description="CISO admins regulate exactly what custom roles can do. Built-in roles stay visible as read-only permission baselines."
        actions={
          <Button
            variant="primary"
            size="md"
            onClick={() => {
              setCreating(true);
              setOperationError(null);
            }}
          >
            New custom role
          </Button>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="TOTAL ROLES" value={roles.length} tone="brand" />
        <KpiTile label="BUILT-IN" value={builtinCount} tone="info" />
        <KpiTile label="CUSTOM" value={customCount} tone="healthy" />
      </div>

      {loadError ? (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load RBAC data">
          <p className="text-sm text-state-critical" role="alert">{loadError}</p>
        </Panel>
      ) : null}

      {operationError ? (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Role operation failed">
          <p className="text-sm text-state-critical" role="alert">{operationError}</p>
        </Panel>
      ) : null}

      {creating ? (
        <NewRoleForm
          permissions={permissions}
          onCancel={() => setCreating(false)}
          onCreated={async () => {
            setCreating(false);
            await refresh();
          }}
        />
      ) : null}

      {loading && roles.length === 0 ? (
        <Panel padding="md" tone="inset" eyebrow="LOADING" title="Loading role matrix">
          <p className="text-sm text-text-muted">Loading roles and permissions...</p>
        </Panel>
      ) : roles.length === 0 ? (
        <EmptyState
          icon={<ShieldCheck />}
          title={loadError ? 'Roles could not be loaded' : 'No roles yet'}
          description={loadError ? 'Resolve the error above and refresh.' : 'Create one to start configuring access.'}
        />
      ) : (
        <Panel padding="sm" tone="inset" eyebrow="MATRIX" title="Role / permission grid">
          <div className="overflow-x-auto">
            <table className="min-w-[800px] w-full text-sm">
              <thead>
                <tr className="border-b border-border-subtle">
                  <th className="sticky left-0 bg-surface text-left px-3 py-2 font-medium text-text-secondary">
                    Permission
                  </th>
                  {roles.map((r) => (
                    <th key={r.id} className="text-center px-3 py-2 font-medium">
                      <div className="flex flex-col items-center gap-1">
                        <span className="text-sm text-foreground">{r.name}</span>
                        <StatusTag tone={isBuiltInRole(r) ? 'info' : 'healthy'}>
                          {isBuiltInRole(r) ? 'built-in' : 'custom'}
                        </StatusTag>
                        {!isBuiltInRole(r) ? (
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              setDeleteError(null);
                              setDeleteTarget(r);
                            }}
                            aria-label={`Delete custom role ${r.name}`}
                            disabled={deleting || updatingPermissionKey !== null}
                            className="text-state-critical hover:text-state-critical"
                          >
                            <Trash2 className="h-3 w-3" /> delete
                          </Button>
                        ) : null}
                      </div>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {Object.entries(grouped).map(([cat, perms]) => (
                  <Fragment key={cat}>
                    <tr key={`cat-${cat}`}>
                      <td
                        colSpan={roles.length + 1}
                        className="bg-surface-2 px-3 py-2 font-semibold text-xs uppercase tracking-wider text-text-secondary"
                      >
                        {cat}
                      </td>
                    </tr>
                    {perms.map((p) => (
                      <tr key={p.name} className="border-b border-border-subtle/50">
                        <td className="sticky left-0 bg-surface px-3 py-2">
                          <div className="flex flex-col">
                            <span className="font-mono text-xs">{p.name}</span>
                            <small className="text-text-secondary text-[0.7rem]">{p.description}</small>
                          </div>
                        </td>
                        {roles.map((r) => {
                          const has = r.permissions.includes(p.name);
                          const builtIn = isBuiltInRole(r);
                          const permissionKey = `${r.id}:${p.name}`;
                          const savingThisPermission = updatingPermissionKey === permissionKey;
                          const checkboxLabel = builtIn
                            ? `Built-in role ${r.name} ${has ? 'includes' : 'does not include'} ${p.name}`
                            : `${has ? 'Revoke' : 'Grant'} ${p.name} for ${r.name}`;
                          return (
                            <td key={`${r.id}-${p.name}`} className="text-center px-3 py-2">
                              <input
                                type="checkbox"
                                className={`h-4 w-4 rounded border-border-subtle accent-brand-500 ${
                                  builtIn ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'
                                }`}
                                aria-label={checkboxLabel}
                                checked={has}
                                disabled={builtIn || updatingPermissionKey !== null}
                                onChange={(e) => togglePermission(r, p.name, e.target.checked)}
                                title={
                                  builtIn
                                    ? 'Built-in role baseline'
                                    : savingThisPermission
                                      ? 'Saving permission change'
                                      : undefined
                                }
                              />
                            </td>
                          );
                        })}
                      </tr>
                    ))}
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}
      <ConfirmModal
        open={deleteTarget !== null}
        title="Delete custom role?"
        body={`This removes ${deleteTarget?.name ?? 'this role'} from the RBAC catalog. Users assigned to this role should be reviewed before deletion.`}
        variant="danger"
        confirmLabel={deleting ? 'Deleting...' : 'Delete role'}
        confirmDisabled={deleting}
        cancelDisabled={deleting}
        onConfirm={confirmDelete}
        onCancel={() => {
          setDeleteTarget(null);
          setDeleteError(null);
        }}
      >
        {deleteError ? (
          <p className="rounded-md border border-state-critical/40 bg-state-critical/10 px-3 py-2 text-sm text-state-critical" role="alert">
            Role delete failed: {deleteError}
          </p>
        ) : null}
      </ConfirmModal>
    </div>
  );
}

function NewRoleForm({
  permissions,
  onCancel,
  onCreated,
}: {
  permissions: Permission[];
  onCancel: () => void;
  onCreated: () => Promise<void> | void;
}) {
  const client = useApiClient();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const roleName = name.trim();
    if (!roleName) {
      setError('Role name is required.');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await client.createCustomRole({ name: roleName, description: description.trim(), permissions: selected });
      await onCreated();
    } catch (err) {
      setError(errorMessage(err, 'Create failed.'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Panel padding="md" eyebrow="CREATE" title="New custom role">
      <form onSubmit={submit} className="flex flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="role-name">Role name</Label>
          <Input
            id="role-name"
            placeholder="e.g. incident-responder"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              if (error) setError(null);
            }}
            required
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="role-desc">Description</Label>
          <Input
            id="role-desc"
            placeholder="Description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <details>
          <summary className="cursor-pointer text-sm text-text-secondary hover:text-foreground">
            Initial permissions ({selected.length} selected)
          </summary>
          <div className="max-h-60 overflow-y-auto mt-2 rounded-md border border-border-subtle bg-surface p-2">
            {permissions.map((p) => (
              <label key={p.name} className="flex gap-2 p-1 cursor-pointer hover:bg-hover rounded">
                <input
                  type="checkbox"
                  className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                  checked={selected.includes(p.name)}
                  onChange={(e) => {
                    setSelected((cur) =>
                      e.target.checked ? [...cur, p.name] : cur.filter((x) => x !== p.name),
                    );
                  }}
                />
                <span className="flex-1 text-sm">
                  <span className="font-mono text-xs">{p.name}</span>{' '}
                  <small className="text-text-secondary">{p.description}</small>
                </span>
              </label>
            ))}
          </div>
        </details>
        {error ? (
          <p className="rounded-md border border-state-critical/40 bg-state-critical/10 px-3 py-2 text-sm text-state-critical" role="alert">
            {error}
          </p>
        ) : null}
        <div className="flex gap-2">
          <Button type="submit" variant="primary" size="md" loading={submitting} disabled={!name.trim()}>
            Create role
          </Button>
          <Button type="button" variant="secondary" size="md" onClick={onCancel} disabled={submitting}>
            Cancel
          </Button>
        </div>
      </form>
    </Panel>
  );
}
