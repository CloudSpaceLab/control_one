import './glyphs.css';

interface Props {
  values: number[];
  height?: number;
  width?: number;
  color?: string;
}

// Sparkline — pure SVG path, zero deps. Empty arrays render an empty
// viewBox rather than throwing so dashboard cells can mount before data.
export default function Sparkline({ values, height = 32, width = 120, color }: Props) {
  if (!values || values.length === 0) {
    return <svg className="glyph-sparkline" viewBox={`0 0 ${width} ${height}`} />;
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
  return (
    <svg
      className="glyph-sparkline"
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
    >
      <path d={d} style={color ? { stroke: color } : undefined} />
    </svg>
  );
}
