import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
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

// CISO admin's RBAC console.
//
//  - Lists every role, every permission, and a checkbox grid showing
//    which role has which permission.
//  - Toggling a checkbox writes through PUT /api/v1/roles/{id}/permissions
//    immediately so the change is live for next-request.
//  - Custom roles can be created (admin-only) at the top.
//  - Built-in roles (admin/ciso/operator/viewer) cannot be deleted.

const BUILTIN_ROLE_IDS = new Set([
  '00000000-0000-0000-0000-000000000001',
  '00000000-0000-0000-0000-000000000002',
  '00000000-0000-0000-0000-000000000003',
  '00000000-0000-0000-0000-000000000004',
]);

export function Roles(): JSX.Element {
  const client = useApiClient();
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [roles, setRoles] = useState<RoleWithPermissions[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const [perms, rs] = await Promise.all([
        client.listPermissions(),
        client.listRolesWithPermissions(),
      ]);
      setPermissions(perms);
      setRoles(rs);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed');
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
    const next = checked
      ? Array.from(new Set([...role.permissions, perm]))
      : role.permissions.filter((p) => p !== perm);
    setRoles((cur) => cur.map((r) => (r.id === role.id ? { ...r, permissions: next } : r)));
    try {
      await client.setRolePermissions(role.id, next);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update failed');
      refresh();
    }
  };

  const handleDelete = async (role: RoleWithPermissions) => {
    if (BUILTIN_ROLE_IDS.has(role.id)) return;
    if (!confirm(`Delete role "${role.name}"?`)) return;
    try {
      await client.deleteRole(role.id);
      refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed');
    }
  };

  const builtinCount = roles.filter((r) => BUILTIN_ROLE_IDS.has(r.id)).length;
  const customCount = roles.length - builtinCount;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="GOVERNANCE · RBAC"
        title="Roles & permissions"
        description="CISO admins regulate exactly what each role can do. Toggle a checkbox to grant or revoke a permission live."
        actions={
          <Button variant="primary" size="md" onClick={() => setCreating(true)}>
            New custom role
          </Button>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="TOTAL ROLES" value={roles.length} tone="brand" />
        <KpiTile label="BUILT-IN" value={builtinCount} tone="info" />
        <KpiTile label="CUSTOM" value={customCount} tone="healthy" />
      </div>

      {error ? (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Operation failed">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      ) : null}

      {creating ? (
        <NewRoleForm
          permissions={permissions}
          onCancel={() => setCreating(false)}
          onCreated={() => {
            setCreating(false);
            refresh();
          }}
        />
      ) : null}

      {roles.length === 0 ? (
        <EmptyState
          icon={<ShieldCheck />}
          title="No roles yet"
          description="Create one to start configuring access."
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
                        <StatusTag tone={BUILTIN_ROLE_IDS.has(r.id) ? 'info' : 'healthy'}>
                          {BUILTIN_ROLE_IDS.has(r.id) ? 'built-in' : 'custom'}
                        </StatusTag>
                        {!BUILTIN_ROLE_IDS.has(r.id) ? (
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDelete(r)}
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
                  <>
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
                          return (
                            <td key={`${r.id}-${p.name}`} className="text-center px-3 py-2">
                              <input
                                type="checkbox"
                                className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                                checked={has}
                                onChange={(e) => togglePermission(r, p.name, e.target.checked)}
                              />
                            </td>
                          );
                        })}
                      </tr>
                    ))}
                  </>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}
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
  onCreated: () => void;
}) {
  const client = useApiClient();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selected, setSelected] = useState<string[]>([]);

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    try {
      await client.createCustomRole({ name: name.trim(), description: description.trim(), permissions: selected });
      onCreated();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'create failed');
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
            onChange={(e) => setName(e.target.value)}
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
        <div className="flex gap-2">
          <Button type="submit" variant="primary" size="md">Create role</Button>
          <Button type="button" variant="secondary" size="md" onClick={onCancel}>Cancel</Button>
        </div>
      </form>
    </Panel>
  );
}
