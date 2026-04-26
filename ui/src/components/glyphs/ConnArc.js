import { jsx as _jsx } from "react/jsx-runtime";
import './glyphs.css';
// ConnArc renders a single curved SVG path between two points. Used in the
// fleet topology view to show agent ↔ controlplane links and node ↔ node
// peer flows. Caller owns the parent <svg>.
export default function ConnArc({ from, to, curvature = 0.4, color }) {
    const mx = (from.x + to.x) / 2;
    const my = (from.y + to.y) / 2;
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    // Perpendicular offset for the control point.
    const cx = mx - dy * curvature;
    const cy = my + dx * curvature;
    const d = `M${from.x},${from.y} Q${cx},${cy} ${to.x},${to.y}`;
    return (_jsx("g", { className: "glyph-conn-arc", children: _jsx("path", { d: d, style: color ? { stroke: color } : undefined }) }));
}
