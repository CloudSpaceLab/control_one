import { jsx as _jsx } from "react/jsx-runtime";
import './glyphs.css';
// Sparkline — pure SVG path, zero deps. Empty arrays render an empty
// viewBox rather than throwing so dashboard cells can mount before data.
export default function Sparkline({ values, height = 32, width = 120, color }) {
    if (!values || values.length === 0) {
        return _jsx("svg", { className: "glyph-sparkline", viewBox: `0 0 ${width} ${height}` });
    }
    const min = Math.min(...values);
    const max = Math.max(...values);
    const span = max - min || 1;
    const step = width / Math.max(values.length - 1, 1);
    const d = values
        .map((v, i) => {
        const x = i * step;
        const y = height - ((v - min) / span) * height;
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
        .join(' ');
    return (_jsx("svg", { className: "glyph-sparkline", viewBox: `0 0 ${width} ${height}`, preserveAspectRatio: "none", children: _jsx("path", { d: d, style: color ? { stroke: color } : undefined }) }));
}
