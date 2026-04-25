import './Skeleton.css';

// Skeleton renders a shimmering placeholder box. Use for cards, rows, and
// avatars while data is in-flight — call sites should swap their real content
// in once loading is false.
export function Skeleton({ width = '100%', height = 16, radius = 4 }: { width?: number | string; height?: number | string; radius?: number }): JSX.Element {
  return <span className="skeleton" style={{ width, height, borderRadius: radius }} aria-hidden="true" />;
}

export function SkeletonRow({ columns = 5 }: { columns?: number }): JSX.Element {
  return (
    <tr>
      {Array.from({ length: columns }).map((_, i) => (
        <td key={i}><Skeleton height={12} /></td>
      ))}
    </tr>
  );
}
