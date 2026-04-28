import { cva, type VariantProps } from 'class-variance-authority';
import { type HTMLAttributes } from 'react';
import { cn } from '@/lib/utils';

const badgeVariants = cva(
  'inline-flex items-center gap-1 rounded-sm border px-1.5 py-0.5 text-[0.65rem] font-mono uppercase tracking-wider transition-colors focus:outline-none',
  {
    variants: {
      tone: {
        default: 'border-border-subtle bg-surface-2 text-text-secondary',
        brand: 'border-brand-500/40 bg-brand-500/15 text-brand-400',
        accent: 'border-accent-500/40 bg-accent-500/15 text-accent-400',
        healthy: 'border-state-healthy/40 bg-state-healthy/15 text-state-healthy',
        warning: 'border-state-warning/40 bg-state-warning/15 text-state-warning',
        degraded: 'border-state-degraded/40 bg-state-degraded/15 text-state-degraded',
        critical: 'border-state-critical/50 bg-state-critical/15 text-state-critical',
        info: 'border-state-info/40 bg-state-info/15 text-state-info',
        unknown: 'border-state-unknown/40 bg-state-unknown/15 text-state-unknown',
      },
      variant: {
        soft: '',
        solid: 'border-transparent text-[#0f172a]',
        outline: 'bg-transparent',
      },
    },
    compoundVariants: [
      { variant: 'solid', tone: 'brand', className: 'bg-brand-500' },
      { variant: 'solid', tone: 'accent', className: 'bg-accent-500' },
      { variant: 'solid', tone: 'healthy', className: 'bg-state-healthy' },
      { variant: 'solid', tone: 'warning', className: 'bg-state-warning' },
      { variant: 'solid', tone: 'degraded', className: 'bg-state-degraded' },
      { variant: 'solid', tone: 'critical', className: 'bg-state-critical' },
      { variant: 'solid', tone: 'info', className: 'bg-state-info' },
    ],
    defaultVariants: { tone: 'default', variant: 'soft' },
  },
);

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

export function Badge({ className, tone, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ tone, variant }), className)} {...props} />;
}

export { badgeVariants };
