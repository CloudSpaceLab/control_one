import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
export function DemandForm({ title, icon, summary, children, defaultExpanded = false }) {
    const [isExpanded, setIsExpanded] = useState(defaultExpanded);
    const toggleExpanded = () => {
        setIsExpanded(!isExpanded);
    };
    return (_jsxs("div", { className: `demand-form ${isExpanded ? 'expanded' : 'collapsed'}`, children: [_jsxs("div", { className: "demand-form-header", onClick: toggleExpanded, children: [_jsxs("div", { className: "demand-form-title", children: [_jsx("div", { className: "demand-form-icon", children: icon }), _jsxs("div", { children: [_jsx("div", { children: title }), summary && _jsx("div", { className: "demand-form-summary", children: summary })] })] }), _jsx("div", { className: "demand-form-toggle", children: _jsx("svg", { width: "16", height: "16", viewBox: "0 0 16 16", fill: "none", children: _jsx("path", { d: "M4 6L8 10L12 6", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round" }) }) })] }), _jsx("div", { className: "demand-form-content", children: children })] }));
}
