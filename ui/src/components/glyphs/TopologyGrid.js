import { jsx as _jsx } from "react/jsx-runtime";
import StatusDot from './StatusDot';
import './glyphs.css';
// TopologyGrid renders the whole fleet as colour dots. Hover for tooltip,
// click to drill in. Auto-fills based on viewport so this scales from 5
// nodes to thousands without code changes.
export default function TopologyGrid({ nodes, onNodeClick }) {
    return (_jsx("div", { className: "glyph-topology-grid", role: "grid", children: nodes.map((node) => (_jsx("button", { type: "button", className: "glyph-topology-node", title: node.hint ?? node.hostname ?? node.id, onClick: () => onNodeClick?.(node), style: { background: 'transparent', border: 'none' }, children: _jsx(StatusDot, { state: node.state, title: node.hostname ?? node.id }) }, node.id))) }));
}
