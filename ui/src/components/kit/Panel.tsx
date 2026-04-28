import { forwardRef, type HTMLAttributes, type ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { Eyebrow } from './Eyebrow';

export interface PanelProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  title?: ReactNode;
  eyebrow?: string;
  actions?: ReactNode;
  loading?: boolean;
  tone?: 'default' | 'inset' | 'glow';
  padding?: 'sm' | 'md' | 'lg';
  toneAccent?: 'brand' | 'accent' | 'healthy' | 'warning' | 'critical';
}

const PADDING: Record<NonNullable<PanelProps['padding']>, string> = {
  sm: 'p-4',
  md: 'p-5',
  lg: 'p-6',
};

const ACCENT_DOT: Record<NonNullable<PanelProps['toneAccent']>, string> = {
  brand: 'bg-brand-500',
  accent: 'bg-accent-500',
  healthy: 'bg-state-healthy',
  warning: 'bg-state-warning',
  critical: 'bg-state-critical',
};

export const Panel = forwardRef<HTMLElement, PanelProps>(
  (
    {
      className,
      title,
      eyebrow,
      actions,
      loading,
      tone = 'default',
      padding = 'md',
      toneAccent,
      children,
      ...props
    },
    ref,
  ) => (
    <section
      ref={ref}
      className={cn(
        'relative flex min-w-0 flex-col gap-4 rounded-lg border text-foreground transition-shadow',
        tone === 'inset'
          ? 'border-border-subtle bg-surface'
          : 'border-border-subtle bg-elevated shadow-[var(--shadow-panel)]',
        tone === 'glow' && 'co-anim-pulse-glow',
        PADDING[padding],
        className,
      )}
      aria-busy={loading || undefined}
      {...props}
    >
      {(title || eyebrow || actions) && (
        <header className="flex items-start justify-between gap-3">
          <div className="flex flex-col gap-1 min-w-0">
            {eyebrow && (
              <div className="flex items-center gap-2">
                {toneAccent && (
                  <span className={cn('h-1.5 w-1.5 rounded-full', ACCENT_DOT[toneAccent])} aria-hidden />
                )}
                <Eyebrow>{eyebrow}</Eyebrow>
              </div>
            )}
            {title && (
              <h3 className="font-display text-base font-semibold leading-tight tracking-tight text-foreground">
                {title}
              </h3>
            )}
          </div>
          {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className={cn('flex min-w-0 flex-1 flex-col gap-3', loading && 'opacity-60')}>{children}</div>
    </section>
  ),
);
Panel.displayName = 'Panel';
