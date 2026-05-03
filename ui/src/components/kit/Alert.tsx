import { AlertCircle, AlertTriangle, CheckCircle2, Info, type LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

export type AlertVariant = 'info' | 'success' | 'warning' | 'critical';

export interface AlertProps {
  variant?: AlertVariant;
  title?: ReactNode;
  /** Action slot rendered on the right (e.g. a Retry button). */
  actions?: ReactNode;
  icon?: LucideIcon | null;
  className?: string;
  children?: ReactNode;
  role?: 'alert' | 'status';
}

const VARIANT_CLASS: Record<AlertVariant, string> = {
  info: 'border-state-info/30 bg-state-info/10 text-state-info',
  success: 'border-state-healthy/30 bg-state-healthy/10 text-state-healthy',
  warning: 'border-state-warning/30 bg-state-warning/10 text-state-warning',
  critical: 'border-state-critical/30 bg-state-critical/10 text-state-critical',
};

const VARIANT_ICON: Record<AlertVariant, LucideIcon> = {
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  critical: AlertCircle,
};

export function Alert({
  variant = 'info',
  title,
  actions,
  icon,
  className,
  children,
  role,
}: AlertProps) {
  const Icon = icon === null ? null : icon ?? VARIANT_ICON[variant];
  return (
    <div
      role={role ?? (variant === 'critical' || variant === 'warning' ? 'alert' : 'status')}
      className={cn(
        'flex items-start gap-3 rounded-lg border px-4 py-3 text-sm',
        VARIANT_CLASS[variant],
        className,
      )}
    >
      {Icon && <Icon className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        {title && <div className="font-display text-sm font-semibold">{title}</div>}
        {children && <div className="text-text-secondary">{children}</div>}
      </div>
      {actions && <div className="ml-auto flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
