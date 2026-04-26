import { jsx as _jsx } from "react/jsx-runtime";
import './glyphs.css';
// StatusDot — the pixel-cheapest health indicator. Critical pulses; the
// rest are static so a busy fleet view doesn't twitch. Reduced-motion media
// query in glyphs.css disables the pulse globally.
export default function StatusDot({ state, title, size }) {
    const style = size ? { width: size, height: size } : undefined;
    return (_jsx("span", { className: "glyph-status-dot", "data-state": state, title: title, style: style, role: "img", "aria-label": title ?? state }));
}
