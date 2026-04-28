import { type ReactNode } from 'react';
import { cn } from '@/lib/utils';

export interface EmptyStateProps {
  icon?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
  tone?: 'neutral' | 'success';
}

export function EmptyState({ icon, title, description, action, className, tone = 'neutral' }: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border-subtle bg-surface px-6 py-10 text-center',
        tone === 'success' && 'border-state-healthy/30 bg-state-healthy/5',
        className,
      )}
    >
      {icon && (
        <div
          className={cn(
            'flex h-10 w-10 items-center justify-center rounded-full bg-surface-2 text-text-secondary [&_svg]:h-5 [&_svg]:w-5',
            tone === 'success' && 'bg-state-healthy/15 text-state-healthy',
          )}
        >
          {icon}
        </div>
      )}
      <div className="flex flex-col gap-1">
        <h4 className="font-display text-sm font-semibold text-foreground">{title}</h4>
        {description && <p className="max-w-sm text-xs text-text-secondary">{description}</p>}
      </div>
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}
