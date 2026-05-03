import type { NodeHealthRiskLevel } from '@/lib/api';

export interface HealthGaugeProps {
  score: number;
  risk: NodeHealthRiskLevel;
  size?: 'sm' | 'md' | 'lg';
  ariaLabel?: string;
}

const SIZE: Record<NonNullable<HealthGaugeProps['size']>, { w: number; h: number; r: number; sw: number; fs: number }> = {
  sm: { w: 84, h: 50, r: 32, sw: 8, fs: 0.95 },
  md: { w: 120, h: 70, r: 48, sw: 10, fs: 1.25 },
  lg: { w: 180, h: 100, r: 70, sw: 12, fs: 1.6 },
};

function strokeColor(risk: NodeHealthRiskLevel): string {
  switch (risk) {
    case 'critical':
      return 'var(--state-critical)';
    case 'high':
      return 'var(--state-degraded)';
    case 'medium':
      return 'var(--state-warning)';
    case 'low':
      return 'var(--state-healthy)';
    case 'calibrating':
    default:
      return 'var(--text-muted)';
  }
}

export function HealthGauge({ score, risk, size = 'md', ariaLabel }: HealthGaugeProps) {
  const { w, h, r, sw, fs } = SIZE[size];
  const circumference = Math.PI * r;
  const clamped = Math.max(0, Math.min(100, score));
  const offset = circumference * (1 - clamped / 100);
  const cx = w / 2;
  const cy = h - 10;

  return (
    <svg
      width={w}
      height={h}
      viewBox={`0 0 ${w} ${h}`}
      role="img"
      aria-label={ariaLabel ?? `Health score ${clamped}`}
    >
      <path
        d={`M ${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r} ${cy}`}
        fill="none"
        stroke="currentColor"
        strokeOpacity={0.15}
        strokeWidth={sw}
        strokeLinecap="round"
      />
      <path
        d={`M ${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r} ${cy}`}
        fill="none"
        stroke={strokeColor(risk)}
        strokeWidth={sw}
        strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={offset}
      />
      <text
        x={cx}
        y={cy - 4}
        textAnchor="middle"
        className="font-display fill-current text-foreground"
        style={{ fontSize: `${fs}rem`, fontWeight: 600 }}
      >
        {risk === 'calibrating' ? '—' : clamped}
      </text>
    </svg>
  );
}
