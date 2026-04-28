import { type HTMLAttributes } from 'react';
import { cn } from '@/lib/utils';

export interface EyebrowProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: 'muted' | 'brand';
}

export function Eyebrow({ className, tone = 'muted', children, ...props }: EyebrowProps) {
  return (
    <span
      className={cn(
        'inline-block font-mono text-[0.65rem] uppercase tracking-[0.16em]',
        tone === 'brand' ? 'text-brand-400' : 'text-text-muted',
        className,
      )}
      {...props}
    >
      {children}
    </span>
  );
}
