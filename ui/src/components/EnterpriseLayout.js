import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Enterprise-grade layout container with optimized component placement
 * for professional dashboards and management interfaces
 */
export function EnterpriseLayout({ children, variant = 'dashboard', className = '' }) {
    return (_jsx("div", { className: `enterprise-layout enterprise-layout--${variant} ${className}`, children: children }));
}
/**
 * Executive overview section for bird's eye dashboard perspective
 * Optimized for maximum information density with professional hierarchy
 */
export function ExecutiveOverview({ children, title, subtitle, className = '' }) {
    return (_jsxs("section", { className: `executive-overview ${className}`, children: [_jsxs("div", { className: "executive-overview__header", children: [_jsxs("div", { className: "executive-overview__title-section", children: [_jsx("h1", { className: "executive-overview__title", children: title }), subtitle && (_jsx("p", { className: "executive-overview__subtitle", children: subtitle }))] }), _jsx("div", { className: "executive-overview__actions" })] }), _jsx("div", { className: "executive-overview__kpi-grid", children: children })] }));
}
/**
 * Professional management panel with consistent positioning
 * Optimized for forms, filters, and detail views
 */
export function ManagementPanel({ children, title, icon, subtitle, className = '', position = 'primary' }) {
    return (_jsxs("div", { className: `management-panel management-panel--${position} ${className}`, children: [_jsx("div", { className: "management-panel__header", children: _jsxs("div", { className: "management-panel__title-section", children: [icon && _jsx("span", { className: "management-panel__icon", children: icon }), _jsxs("div", { children: [_jsx("h2", { className: "management-panel__title", children: title }), subtitle && (_jsx("p", { className: "management-panel__subtitle", children: subtitle }))] })] }) }), _jsx("div", { className: "management-panel__content", children: children })] }));
}
/**
 * Standardized action zone for consistent button and widget placement
 */
export function ActionZone({ children, variant = 'primary', alignment = 'right', className = '' }) {
    return (_jsx("div", { className: `action-zone action-zone--${variant} action-zone--${alignment} ${className}`, children: children }));
}
/**
 * Professional content grid for optimal information organization
 */
export function ContentGrid({ children, columns = 2, gap = 'md', className = '' }) {
    return (_jsx("div", { className: `content-grid content-grid--${columns}-col content-grid--gap-${gap} ${className}`, children: children }));
}
