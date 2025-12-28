import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
function formatDate(value) {
    return new Date(value).toLocaleString();
}
export function Tenants() {
    const [offset, setOffset] = useState(0);
    const [limit] = useState(20);
    const { data, pagination, loading, error } = useTenants();
    const rows = useMemo(() => data, [data]);
    return (_jsxs("section", { children: [_jsx("h2", { children: "Tenants" }), _jsx("p", { children: "Tenants represent isolation boundaries for infrastructure, policy, and compliance scope." }), loading ? _jsx("p", { className: "muted", children: "Loading tenants\u2026" }) : null, error ? _jsxs("p", { className: "form-error", children: ["Failed to load tenants: ", error] }) : null, !loading && !error && rows.length === 0 ? _jsx("p", { className: "muted", children: "No tenants registered yet." }) : null, !loading && !error && rows.length > 0 ? (_jsxs(_Fragment, { children: [_jsxs("table", { children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "ID" }), _jsx("th", { children: "Name" }), _jsx("th", { children: "Created" })] }) }), _jsx("tbody", { children: rows.map((tenant) => (_jsxs("tr", { children: [_jsx("td", { children: tenant.id }), _jsx("td", { children: tenant.name }), _jsx("td", { children: formatDate(tenant.created_at) })] }, tenant.id))) })] }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", disabled: !pagination.prevOffset, onClick: () => setOffset(pagination.prevOffset ?? 0), children: "Previous" }), _jsxs("span", { children: ["Showing ", rows.length, " of ", pagination.total, " tenants"] }), _jsx("button", { type: "button", disabled: !pagination.nextOffset, onClick: () => setOffset(pagination.nextOffset ?? offset + limit), children: "Next" })] })] })) : null] }));
}
