import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
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
export function Roles() {
    const client = useApiClient();
    const [permissions, setPermissions] = useState([]);
    const [roles, setRoles] = useState([]);
    const [error, setError] = useState(null);
    const [creating, setCreating] = useState(false);
    const refresh = useCallback(async () => {
        try {
            const [perms, rs] = await Promise.all([
                client.listPermissions(),
                client.listRolesWithPermissions(),
            ]);
            setPermissions(perms);
            setRoles(rs);
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'load failed');
        }
    }, [client]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    const grouped = useMemo(() => {
        const out = {};
        permissions.forEach((p) => {
            const key = p.category || 'general';
            if (!out[key])
                out[key] = [];
            out[key].push(p);
        });
        return out;
    }, [permissions]);
    const togglePermission = async (role, perm, checked) => {
        const next = checked
            ? Array.from(new Set([...role.permissions, perm]))
            : role.permissions.filter((p) => p !== perm);
        setRoles((cur) => cur.map((r) => (r.id === role.id ? { ...r, permissions: next } : r)));
        try {
            await client.setRolePermissions(role.id, next);
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'update failed');
            refresh();
        }
    };
    const handleDelete = async (role) => {
        if (BUILTIN_ROLE_IDS.has(role.id))
            return;
        if (!confirm(`Delete role "${role.name}"?`))
            return;
        try {
            await client.deleteRole(role.id);
            refresh();
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'delete failed');
        }
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "RBAC" }), _jsx("h2", { children: "Roles & permissions" }), _jsx("p", { className: "subtitle", children: "CISO admins regulate exactly what each role can do. Toggle a checkbox to grant or revoke a permission live." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => setCreating(true), children: "New custom role" })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, creating ? (_jsx(NewRoleForm, { permissions: permissions, onCancel: () => setCreating(false), onCreated: () => {
                    setCreating(false);
                    refresh();
                } })) : null, roles.length === 0 ? (_jsx(EmptyState, { title: "No roles yet", description: "Create one to start configuring access." })) : (_jsx("div", { style: { overflowX: 'auto' }, children: _jsxs("table", { className: "data-table", style: { minWidth: 800 }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { style: { position: 'sticky', left: 0, background: 'var(--bg-primary)' }, children: "Permission" }), roles.map((r) => (_jsxs("th", { style: { textAlign: 'center' }, children: [_jsx("div", { style: { fontSize: 14 }, children: r.name }), _jsx("small", { style: { color: 'var(--text-secondary)', fontWeight: 400 }, children: BUILTIN_ROLE_IDS.has(r.id) ? 'built-in' : 'custom' }), !BUILTIN_ROLE_IDS.has(r.id) ? (_jsx("button", { type: "button", onClick: () => handleDelete(r), style: { background: 'transparent', border: 'none', color: 'var(--state-critical)', cursor: 'pointer', fontSize: 12 }, children: "delete" })) : null] }, r.id)))] }) }), _jsx("tbody", { children: Object.entries(grouped).map(([cat, perms]) => (_jsxs(_Fragment, { children: [_jsx("tr", { children: _jsx("td", { colSpan: roles.length + 1, style: { background: 'var(--bg-tertiary)', fontWeight: 600, padding: 8 }, children: cat }) }, `cat-${cat}`), perms.map((p) => (_jsxs("tr", { children: [_jsxs("td", { style: { position: 'sticky', left: 0, background: 'var(--bg-primary)' }, children: [_jsx("div", { children: p.name }), _jsx("small", { style: { color: 'var(--text-secondary)' }, children: p.description })] }), roles.map((r) => {
                                                const has = r.permissions.includes(p.name);
                                                return (_jsx("td", { style: { textAlign: 'center' }, children: _jsx("input", { type: "checkbox", checked: has, onChange: (e) => togglePermission(r, p.name, e.target.checked) }) }, `${r.id}-${p.name}`));
                                            })] }, p.name)))] }))) })] }) }))] }));
}
function NewRoleForm({ permissions, onCancel, onCreated, }) {
    const client = useApiClient();
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [selected, setSelected] = useState([]);
    const submit = async (e) => {
        e.preventDefault();
        try {
            await client.createCustomRole({ name: name.trim(), description: description.trim(), permissions: selected });
            onCreated();
        }
        catch (err) {
            alert(err instanceof Error ? err.message : 'create failed');
        }
    };
    return (_jsxs("form", { onSubmit: submit, style: { background: 'var(--bg-secondary)', padding: 16, borderRadius: 8, marginBottom: 16, display: 'grid', gap: 8 }, children: [_jsx("input", { placeholder: "Role name (e.g. incident-responder)", value: name, onChange: (e) => setName(e.target.value), required: true }), _jsx("input", { placeholder: "Description", value: description, onChange: (e) => setDescription(e.target.value) }), _jsxs("details", { children: [_jsxs("summary", { style: { cursor: 'pointer' }, children: ["Initial permissions (", selected.length, " selected)"] }), _jsx("div", { style: { maxHeight: 240, overflowY: 'auto', marginTop: 8 }, children: permissions.map((p) => (_jsxs("label", { style: { display: 'flex', gap: 8, padding: 4 }, children: [_jsx("input", { type: "checkbox", checked: selected.includes(p.name), onChange: (e) => {
                                        setSelected((cur) => e.target.checked ? [...cur, p.name] : cur.filter((x) => x !== p.name));
                                    } }), _jsxs("span", { style: { flex: 1 }, children: [p.name, " ", _jsx("small", { style: { color: 'var(--text-secondary)' }, children: p.description })] })] }, p.name))) })] }), _jsxs("div", { style: { display: 'flex', gap: 8 }, children: [_jsx("button", { type: "submit", className: "primary-button", children: "Create role" }), _jsx("button", { type: "button", className: "secondary-button", onClick: onCancel, children: "Cancel" })] })] }));
}
