import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import './EmptyState.css';
// EmptyState is the standard "nothing to show yet" component. Use it
// everywhere a list, table, or pane has no rows — never raw text. Real empty
// states explain *what* the page does, *why* it's empty, and *what to do next*.
export function EmptyState({ icon, title, description, primaryAction, secondaryAction }) {
    return (_jsxs("div", { className: "co-empty", children: [icon ? _jsx("div", { className: "co-empty__icon", "aria-hidden": "true", children: icon }) : null, _jsx("h3", { className: "co-empty__title", children: title }), description ? _jsx("p", { className: "co-empty__desc", children: description }) : null, primaryAction || secondaryAction ? (_jsxs("div", { className: "co-empty__actions", children: [primaryAction ? (_jsx("button", { type: "button", className: "primary-button", onClick: primaryAction.onClick, children: primaryAction.label })) : null, secondaryAction ? (_jsx("button", { type: "button", className: "secondary-button", onClick: secondaryAction.onClick, children: secondaryAction.label })) : null] })) : null] }));
}
