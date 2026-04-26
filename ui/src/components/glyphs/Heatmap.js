import { jsx as _jsx } from "react/jsx-runtime";
import './glyphs.css';
// Heatmap — CSS grid + per-cell background opacity. Colour ramp comes from
// --state-* tokens so it adapts to theme. No chart library, ever.
export default function Heatmap({ values, cellTitle }) {
    if (!values || values.length === 0) {
        return null;
    }
    const cols = Math.max(...values.map((row) => row.length));
    return (_jsx("div", { className: "glyph-heatmap", style: { gridTemplateColumns: `repeat(${cols}, 1fr)` }, children: values.flatMap((row, r) => row.map((v, c) => {
            const clamped = Math.max(0, Math.min(1, v));
            const color = colorRamp(clamped);
            return (_jsx("div", { className: "glyph-heatmap-cell", title: cellTitle ? cellTitle(r, c, v) : `${(v * 100).toFixed(0)}%`, style: { background: color } }, `${r}-${c}`));
        })) }));
}
function colorRamp(v) {
    if (v < 0.05)
        return 'var(--bg-tertiary)';
    if (v < 0.25)
        return 'var(--state-healthy)';
    if (v < 0.55)
        return 'var(--state-warning)';
    if (v < 0.8)
        return 'var(--state-degraded)';
    return 'var(--state-critical)';
}
