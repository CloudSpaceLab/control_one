import './glyphs.css';

interface Props {
  from: { x: number; y: number };
  to: { x: number; y: number };
  // Curvature factor 0..1; 0 = straight line.
  curvature?: number;
  color?: string;
}

// ConnArc renders a single curved SVG path between two points. Used in the
// fleet topology view to show agent ↔ controlplane links and node ↔ node
// peer flows. Caller owns the parent <svg>.
export default function ConnArc({ from, to, curvature = 0.4, color }: Props) {
  const mx = (from.x + to.x) / 2;
  const my = (from.y + to.y) / 2;
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  // Perpendicular offset for the control point.
  const cx = mx - dy * curvature;
  const cy = my + dx * curvature;
  const d = `M${from.x},${from.y} Q${cx},${cy} ${to.x},${to.y}`;
  return (
    <g className="glyph-conn-arc">
      <path d={d} style={color ? { stroke: color } : undefined} />
    </g>
  );
}
