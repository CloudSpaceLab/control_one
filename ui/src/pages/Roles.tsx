import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import type { Permission, RoleWithPermissions } from '../lib/api';
import { EmptyState } from '../components/EmptyState';

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

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">RBAC</p>
          <h2>Roles & permissions</h2>
          <p className="subtitle">
            CISO admins regulate exactly what each role can do. Toggle a checkbox to grant or revoke a permission live.
          </p>
        </div>
        <button type="button" className="primary-button" onClick={() => setCreating(true)}>
          New custom role
        </button>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

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
        <EmptyState title="No roles yet" description="Create one to start configuring access." />
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table className="data-table" style={{ minWidth: 800 }}>
            <thead>
              <tr>
                <th style={{ position: 'sticky', left: 0, background: 'var(--bg-primary)' }}>Permission</th>
                {roles.map((r) => (
                  <th key={r.id} style={{ textAlign: 'center' }}>
                    <div style={{ fontSize: 14 }}>{r.name}</div>
                    <small style={{ color: 'var(--text-secondary)', fontWeight: 400 }}>
                      {BUILTIN_ROLE_IDS.has(r.id) ? 'built-in' : 'custom'}
                    </small>
                    {!BUILTIN_ROLE_IDS.has(r.id) ? (
                      <button
                        type="button"
                        onClick={() => handleDelete(r)}
                        style={{ background: 'transparent', border: 'none', color: 'var(--state-critical)', cursor: 'pointer', fontSize: 12 }}
                      >
                        delete
                      </button>
                    ) : null}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {Object.entries(grouped).map(([cat, perms]) => (
                <>
                  <tr key={`cat-${cat}`}>
                    <td colSpan={roles.length + 1} style={{ background: 'var(--bg-tertiary)', fontWeight: 600, padding: 8 }}>
                      {cat}
                    </td>
                  </tr>
                  {perms.map((p) => (
                    <tr key={p.name}>
                      <td style={{ position: 'sticky', left: 0, background: 'var(--bg-primary)' }}>
                        <div>{p.name}</div>
                        <small style={{ color: 'var(--text-secondary)' }}>{p.description}</small>
                      </td>
                      {roles.map((r) => {
                        const has = r.permissions.includes(p.name);
                        return (
                          <td key={`${r.id}-${p.name}`} style={{ textAlign: 'center' }}>
                            <input
                              type="checkbox"
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
      )}
    </section>
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
    <form onSubmit={submit} style={{ background: 'var(--bg-secondary)', padding: 16, borderRadius: 8, marginBottom: 16, display: 'grid', gap: 8 }}>
      <input placeholder="Role name (e.g. incident-responder)" value={name} onChange={(e) => setName(e.target.value)} required />
      <input placeholder="Description" value={description} onChange={(e) => setDescription(e.target.value)} />
      <details>
        <summary style={{ cursor: 'pointer' }}>Initial permissions ({selected.length} selected)</summary>
        <div style={{ maxHeight: 240, overflowY: 'auto', marginTop: 8 }}>
          {permissions.map((p) => (
            <label key={p.name} style={{ display: 'flex', gap: 8, padding: 4 }}>
              <input
                type="checkbox"
                checked={selected.includes(p.name)}
                onChange={(e) => {
                  setSelected((cur) =>
                    e.target.checked ? [...cur, p.name] : cur.filter((x) => x !== p.name),
                  );
                }}
              />
              <span style={{ flex: 1 }}>
                {p.name} <small style={{ color: 'var(--text-secondary)' }}>{p.description}</small>
              </span>
            </label>
          ))}
        </div>
      </details>
      <div style={{ display: 'flex', gap: 8 }}>
        <button type="submit" className="primary-button">Create role</button>
        <button type="button" className="secondary-button" onClick={onCancel}>Cancel</button>
      </div>
    </form>
  );
}
