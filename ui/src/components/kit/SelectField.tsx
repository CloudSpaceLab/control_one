import { forwardRef, type ReactNode, type SelectHTMLAttributes } from 'react';
import { cn } from '@/lib/utils';
import { Label } from '@/components/ui/label';

export const SELECT_CLASS =
  'flex h-9 w-full rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:cursor-not-allowed disabled:opacity-50';

export interface SelectFieldProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  children: ReactNode;
  wrapperClassName?: string;
}

export const SelectField = forwardRef<HTMLSelectElement, SelectFieldProps>(
  ({ id, label, children, className, wrapperClassName, ...props }, ref) => {
    if (label) {
      return (
        <div className={cn('flex flex-col gap-1.5', wrapperClassName)}>
          <Label htmlFor={id}>{label}</Label>
          <select ref={ref} id={id} className={cn(SELECT_CLASS, className)} {...props}>
            {children}
          </select>
        </div>
      );
    }
    return (
      <select ref={ref} id={id} className={cn(SELECT_CLASS, className)} {...props}>
        {children}
      </select>
    );
  },
);

SelectField.displayName = 'SelectField';
