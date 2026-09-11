import { type ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { Eyebrow } from './Eyebrow';

export interface SectionHeaderProps {
  eyebrow?: string;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
}

export function SectionHeader({ eyebrow, title, description, actions, className }: SectionHeaderProps) {
  return (
    <header
      className={cn(
        'flex flex-col gap-2 pb-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6',
        className,
      )}
    >
      <div className="min-w-0 flex flex-col gap-1.5">
        {eyebrow && <Eyebrow>{eyebrow}</Eyebrow>}
        <h1 className="break-words font-display text-2xl font-semibold leading-tight tracking-tight text-foreground sm:text-3xl">
          {title}
        </h1>
        {description && (
          <p className="max-w-2xl break-words text-sm text-text-secondary">{description}</p>
        )}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2 shrink-0">{actions}</div>}
    </header>
  );
}
