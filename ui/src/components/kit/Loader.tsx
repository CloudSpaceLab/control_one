import { Loader2 } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface LoaderProps {
  size?: 'xs' | 'sm' | 'md' | 'lg';
  label?: string;
  className?: string;
}

const SIZE: Record<NonNullable<LoaderProps['size']>, string> = {
  xs: 'h-3 w-3',
  sm: 'h-4 w-4',
  md: 'h-5 w-5',
  lg: 'h-7 w-7',
};

export function Loader({ size = 'sm', label, className }: LoaderProps) {
  return (
    <span
      role={label ? 'status' : undefined}
      aria-label={label}
      className={cn('inline-flex items-center gap-2 text-text-muted', className)}
    >
      <Loader2 className={cn('animate-spin', SIZE[size])} aria-hidden />
      {label && <span className="text-sm">{label}</span>}
    </span>
  );
}
