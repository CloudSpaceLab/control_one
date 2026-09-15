import { type ReactNode } from 'react';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { type StateTone } from './types';

export interface StatusTagProps extends Omit<BadgeProps, 'tone' | 'children'> {
  tone: StateTone;
  variant?: BadgeProps['variant'];
  icon?: ReactNode;
  children: ReactNode;
}

export function StatusTag({ tone, variant = 'soft', icon, children, className, ...props }: StatusTagProps) {
  return (
    <Badge tone={tone} variant={variant} className={cn('max-w-full min-w-0', className)} {...props}>
      {icon && <span className="inline-flex shrink-0">{icon}</span>}
      <span className="min-w-0 break-words [overflow-wrap:anywhere]">{children}</span>
    </Badge>
  );
}
