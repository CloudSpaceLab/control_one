import { type ReactNode } from 'react';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { type StateTone } from './types';

export interface StatusTagProps extends Omit<BadgeProps, 'tone' | 'children'> {
  tone: StateTone;
  variant?: BadgeProps['variant'];
  icon?: ReactNode;
  children: ReactNode;
}

export function StatusTag({ tone, variant = 'soft', icon, children, ...props }: StatusTagProps) {
  return (
    <Badge tone={tone} variant={variant} {...props}>
      {icon && <span className="inline-flex shrink-0">{icon}</span>}
      <span>{children}</span>
    </Badge>
  );
}
