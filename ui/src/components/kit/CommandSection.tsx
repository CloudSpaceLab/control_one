import { type ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { type StateTone, TONE_COLOR } from './types';

export interface CommandSectionProps {
  label: string;
  tone?: StateTone | 'brand' | 'accent';
  children: ReactNode;
  className?: string;
}

export function CommandSection({ label, tone = 'brand', children, className }: CommandSectionProps) {
  const accent = tone === 'brand' || tone === 'accent' ? `var(--${tone}-500)` : TONE_COLOR[tone as StateTone];

  return (
    <section className={cn('flex flex-col gap-3', className)}>
      <div className="flex items-center gap-3">
        <span
          className="h-px shrink-0 w-3"
          style={{ backgroundColor: accent }}
          aria-hidden
        />
        <span
          className="font-mono text-[0.6rem] font-semibold uppercase tracking-widest"
          style={{ color: accent }}
        >
          {label}
        </span>
        <span className="h-px flex-1 bg-border-subtle" aria-hidden />
      </div>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {children}
      </div>
    </section>
  );
}
