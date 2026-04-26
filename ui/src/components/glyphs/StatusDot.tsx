import './glyphs.css';

export type State = 'healthy' | 'warning' | 'degraded' | 'critical' | 'unknown';

interface Props {
  state: State;
  title?: string;
  size?: number;
}

// StatusDot — the pixel-cheapest health indicator. Critical pulses; the
// rest are static so a busy fleet view doesn't twitch. Reduced-motion media
// query in glyphs.css disables the pulse globally.
export default function StatusDot({ state, title, size }: Props) {
  const style = size ? { width: size, height: size } : undefined;
  return (
    <span
      className="glyph-status-dot"
      data-state={state}
      title={title}
      style={style}
      role="img"
      aria-label={title ?? state}
    />
  );
}
