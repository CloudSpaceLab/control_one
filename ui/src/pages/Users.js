import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useUsers } from '../hooks/useUsers';
import { useRoles } from '../hooks/useRoles';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import './Users.css';
function formatDate(value) {
    if (!value) {
        return '—';
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
        return value;
    }
    return parsed.toLocaleString();
}
export function Users() {
    const api = useApiClient();
    const [limit] = useState(50);
    const [offset, setOffset] = useState(0);
    const [selectedUser, setSelectedUser] = useState(null);
    const [isEditingRoles, setIsEditingRoles] = useState(false);
    const [selectedRoles, setSelectedRoles] = useState([]);
    const [selectedUserIds, setSelectedUserIds] = useState(new Set());
    const [isBulkAssigning, setIsBulkAssigning] = useState(false);
    const [bulkAssignRoles, setBulkAssignRoles] = useState([]);
    const { data: users, loading: usersLoading, error: usersError, pagination, reload: reloadUsers, } = useUsers({ limit, offset });
    const { data: roles, loading: rolesLoading, error: rolesError, reload: reloadRoles, } = useRoles();
    const { error: formError, success: formSuccess, showError, showSuccess, reset: resetFeedback, } = useFormFeedback();
    const { showToast } = useToast();
    const [updating, setUpdating] = useState(false);
    const roleMap = useMemo(() => {
        const map = new Map();
        roles.forEach((role) => map.set(role.name, role));
        return map;
    }, [roles]);
    const handleEditRoles = (user) => {
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
    const handleRoleToggle = (roleName) => {
        setSelectedRoles((prev) => {
            if (prev.includes(roleName)) {
                return prev.filter((r) => r !== roleName);
            }
            return [...prev, roleName];
        });
    };
    const handleSaveRoles = async (event) => {
        event.preventDefault();
        if (!selectedUser)
            return;
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
        }
        catch (error) {
            const message = error?.message || 'Failed to update user roles';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setUpdating(false);
        }
    };
    const handleRefresh = () => {
        reloadUsers();
        reloadRoles();
    };
    const handleSelectUser = (userId, checked) => {
        setSelectedUserIds((prev) => {
            const next = new Set(prev);
            if (checked) {
                next.add(userId);
            }
            else {
                next.delete(userId);
            }
            return next;
        });
    };
    const handleSelectAll = (checked) => {
        if (checked) {
            setSelectedUserIds(new Set(users.map((u) => u.id)));
        }
        else {
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
        }
        catch (error) {
            const message = error?.message || 'Failed to assign roles';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setIsBulkAssigning(false);
        }
    };
    return (_jsxs("div", { className: "users-page", children: [_jsxs("div", { className: "page-header", children: [_jsxs("div", { children: [_jsx("h1", { children: "Users & Roles" }), _jsx("p", { className: "subtitle", children: "Manage users and their role assignments" })] }), _jsxs("div", { className: "page-actions", children: [selectedUserIds.size > 0 && (_jsxs("div", { style: { display: 'flex', gap: '0.75rem', alignItems: 'center' }, children: [_jsxs("span", { style: { fontSize: '0.9rem', color: 'var(--text-secondary)' }, children: [selectedUserIds.size, " selected"] }), _jsx("button", { type: "button", onClick: () => setIsBulkAssigning(true), className: "btn-primary", disabled: isBulkAssigning, children: "Bulk Assign Roles" })] })), _jsx("button", { type: "button", onClick: handleRefresh, className: "btn-secondary", children: "Refresh" })] })] }), usersError && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading users: ", usersError] }) })), rolesError && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading roles: ", rolesError] }) })), _jsxs("div", { className: "users-stats", children: [_jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: pagination.total }), _jsx("div", { className: "stat-label", children: "Total Users" })] }), _jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: roles.length }), _jsx("div", { className: "stat-label", children: "Available Roles" })] }), _jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: users.filter((u) => u.roles && u.roles.length > 0).length }), _jsx("div", { className: "stat-label", children: "Users with Roles" })] })] }), _jsxs("div", { className: "content-grid", children: [_jsxs("div", { className: "users-section", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Users" }), _jsxs("div", { className: "results-count", children: ["Showing ", users.length, " of ", pagination.total] })] }), usersLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading users..." })) : users.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No users found." }) })) : (_jsxs(_Fragment, { children: [_jsx("div", { className: "table-container", children: _jsxs("table", { className: "users-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { style: { width: '40px' }, children: _jsx("input", { type: "checkbox", checked: selectedUserIds.size === users.length && users.length > 0, onChange: (e) => handleSelectAll(e.target.checked), title: "Select all" }) }), _jsx("th", { children: "User" }), _jsx("th", { children: "Email" }), _jsx("th", { children: "Roles" }), _jsx("th", { children: "Created" }), _jsx("th", { children: "Actions" })] }) }), _jsx("tbody", { children: users.map((user) => (_jsxs("tr", { children: [_jsx("td", { children: _jsx("input", { type: "checkbox", checked: selectedUserIds.has(user.id), onChange: (e) => handleSelectUser(user.id, e.target.checked) }) }), _jsx("td", { children: _jsxs("div", { className: "user-info", children: [_jsx("div", { className: "user-name", children: user.display_name || user.external_id }), _jsxs("div", { className: "user-id", children: ["ID: ", user.id.slice(0, 8), "..."] })] }) }), _jsx("td", { children: user.email || '—' }), _jsx("td", { children: _jsx("div", { className: "roles-list", children: user.roles && user.roles.length > 0 ? (user.roles.map((roleName) => {
                                                                        const role = roleMap.get(roleName);
                                                                        return (_jsx("span", { className: "role-badge", title: role?.description, children: roleName }, roleName));
                                                                    })) : (_jsx("span", { className: "no-roles", children: "No roles assigned" })) }) }), _jsx("td", { children: formatDate(user.created_at) }), _jsx("td", { children: _jsx("button", { type: "button", onClick: () => handleEditRoles(user), className: "btn-link", children: "Edit Roles" }) })] }, user.id))) })] }) }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", onClick: () => setOffset(Math.max(0, offset - limit)), disabled: offset === 0 || usersLoading, className: "btn-secondary", children: "Previous" }), _jsxs("span", { className: "pagination-info", children: ["Page ", Math.floor(offset / limit) + 1, " of ", Math.ceil(pagination.total / limit) || 1] }), _jsx("button", { type: "button", onClick: () => setOffset(offset + limit), disabled: offset + limit >= pagination.total || usersLoading, className: "btn-secondary", children: "Next" })] })] }))] }), _jsxs("div", { className: "roles-section", children: [_jsx("div", { className: "section-header", children: _jsx("h2", { children: "Available Roles" }) }), rolesLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading roles..." })) : roles.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No roles found." }) })) : (_jsx("div", { className: "roles-list-container", children: roles.map((role) => (_jsxs("div", { className: "role-card", children: [_jsx("div", { className: "role-header", children: _jsx("h3", { className: "role-name", children: role.name }) }), role.description && (_jsx("p", { className: "role-description", children: role.description })), _jsx("div", { className: "role-meta", children: _jsxs("span", { className: "role-users-count", children: [users.filter((u) => u.roles?.includes(role.name)).length, " user(s)"] }) })] }, role.id))) }))] })] }), isEditingRoles && selectedUser && (_jsx("div", { className: "modal-overlay", onClick: handleCancelEdit, children: _jsxs("div", { className: "modal-content", onClick: (e) => e.stopPropagation(), children: [_jsxs("div", { className: "modal-header", children: [_jsxs("h2", { children: ["Edit Roles for ", selectedUser.display_name || selectedUser.external_id] }), _jsx("button", { type: "button", onClick: handleCancelEdit, className: "modal-close", children: "\u00D7" })] }), _jsxs("form", { onSubmit: handleSaveRoles, children: [_jsxs("div", { className: "modal-body", children: [formError && (_jsx("div", { className: "error-banner", children: _jsx("p", { children: formError }) })), formSuccess && (_jsx("div", { className: "success-banner", children: _jsx("p", { children: formSuccess }) })), _jsxs("div", { className: "roles-selection", children: [_jsx("p", { className: "selection-hint", children: "Select roles to assign to this user:" }), _jsx("div", { className: "roles-checkboxes", children: roles.map((role) => (_jsxs("label", { className: "role-checkbox", children: [_jsx("input", { type: "checkbox", checked: selectedRoles.includes(role.name), onChange: () => handleRoleToggle(role.name) }), _jsxs("div", { className: "checkbox-content", children: [_jsx("span", { className: "checkbox-role-name", children: role.name }), role.description && (_jsx("span", { className: "checkbox-role-desc", children: role.description }))] })] }, role.id))) })] })] }), _jsxs("div", { className: "modal-footer", children: [_jsx("button", { type: "button", onClick: handleCancelEdit, className: "btn-secondary", disabled: updating, children: "Cancel" }), _jsx("button", { type: "submit", className: "btn-primary", disabled: updating, children: updating ? 'Saving...' : 'Save Changes' })] })] })] }) })), isBulkAssigning && (_jsx("div", { className: "modal-overlay", onClick: () => setIsBulkAssigning(false), children: _jsxs("div", { className: "modal-content", onClick: (e) => e.stopPropagation(), children: [_jsxs("div", { className: "modal-header", children: [_jsx("h2", { children: "Bulk Assign Roles" }), _jsx("button", { type: "button", onClick: () => setIsBulkAssigning(false), className: "modal-close", children: "\u00D7" })] }), _jsxs("div", { className: "modal-body", children: [_jsxs("p", { className: "selection-hint", children: ["Assign roles to ", selectedUserIds.size, " selected user(s):"] }), _jsx("div", { className: "roles-checkboxes", children: roles.map((role) => (_jsxs("label", { className: "role-checkbox", children: [_jsx("input", { type: "checkbox", checked: bulkAssignRoles.includes(role.name), onChange: () => {
                                                    setBulkAssignRoles((prev) => prev.includes(role.name)
                                                        ? prev.filter((r) => r !== role.name)
                                                        : [...prev, role.name]);
                                                } }), _jsxs("div", { className: "checkbox-content", children: [_jsx("span", { className: "checkbox-role-name", children: role.name }), role.description && (_jsx("span", { className: "checkbox-role-desc", children: role.description }))] })] }, role.id))) })] }), _jsxs("div", { className: "modal-footer", children: [_jsx("button", { type: "button", onClick: () => {
                                        setIsBulkAssigning(false);
                                        setBulkAssignRoles([]);
                                    }, className: "btn-secondary", disabled: isBulkAssigning, children: "Cancel" }), _jsx("button", { type: "button", onClick: handleBulkAssignRoles, className: "btn-primary", disabled: isBulkAssigning || bulkAssignRoles.length === 0, children: isBulkAssigning ? 'Assigning...' : `Assign to ${selectedUserIds.size} User(s)` })] })] }) }))] }));
}
