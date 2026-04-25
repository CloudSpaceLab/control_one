import './Badge.css';

export type BadgeVariant =
  | 'success'
  | 'error'
  | 'warning'
  | 'info'
  | 'neutral'
  | 'critical';

interface BadgeProps {
  variant?: BadgeVariant;
  children: React.ReactNode;
  size?: 'sm' | 'md';
}

// Badge is the single source of truth for status pills in the app. Pages
// previously rolled their own status spans with inline styles — converging
// on this component removes a class of subtle visual mismatches that make
// the product feel hand-stitched instead of polished.
export function Badge({ variant = 'neutral', size = 'md', children }: BadgeProps): JSX.Element {
  return <span className={`co-badge co-badge--${variant} co-badge--${size}`}>{children}</span>;
}

// severityToVariant maps a string severity into a Badge variant. Falls back
// to "neutral" so unknown values never crash a render.
export function severityToVariant(severity: string | undefined | null): BadgeVariant {
  switch ((severity ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'error';
    case 'medium':
      return 'warning';
    case 'low':
      return 'info';
    default:
      return 'neutral';
  }
}

// stateToVariant maps a state string ("open", "acked", "resolved") onto Badge.
export function stateToVariant(state: string | undefined | null): BadgeVariant {
  switch ((state ?? '').toLowerCase()) {
    case 'resolved':
    case 'completed':
    case 'success':
    case 'succeeded':
      return 'success';
    case 'failed':
    case 'denied':
      return 'error';
    case 'pending':
    case 'queued':
    case 'running':
      return 'info';
    case 'acked':
    case 'in_review':
      return 'warning';
    default:
      return 'neutral';
  }
}
