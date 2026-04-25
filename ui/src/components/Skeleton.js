import { jsx as _jsx } from "react/jsx-runtime";
import './Skeleton.css';
// Skeleton renders a shimmering placeholder box. Use for cards, rows, and
// avatars while data is in-flight — call sites should swap their real content
// in once loading is false.
export function Skeleton({ width = '100%', height = 16, radius = 4 }) {
    return _jsx("span", { className: "skeleton", style: { width, height, borderRadius: radius }, "aria-hidden": "true" });
}
export function SkeletonRow({ columns = 5 }) {
    return (_jsx("tr", { children: Array.from({ length: columns }).map((_, i) => (_jsx("td", { children: _jsx(Skeleton, { height: 12 }) }, i))) }));
}
